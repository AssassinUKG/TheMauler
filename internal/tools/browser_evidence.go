package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	browserEvidenceVersion    = 1
	maxBrowserEvidenceRecords = 500
	maxBrowserManifestBytes   = 64 * 1024
)

// BrowserEvidenceRecord is the UI-safe, immutable capture descriptor. Fresh
// and CurrentSHA256 are derived on review; the original SHA256 is never
// replaced when a file changes.
type BrowserEvidenceRecord struct {
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	CapturedAt    string `json:"captured_at"`
	WorkspaceFile string `json:"workspace_file"`
	ManifestFile  string `json:"manifest_file"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	FullPage      bool   `json:"full_page"`
	URL           string `json:"url,omitempty"`
	Title         string `json:"title,omitempty"`
	Fresh         bool   `json:"fresh"`
	CurrentSHA256 string `json:"current_sha256,omitempty"`
	ReviewDetail  string `json:"review_detail,omitempty"`
}

func saveBrowserScreenshotEvidence(ctx context.Context, pngBytes []byte, width, height int, fullPage bool, status BrowserRuntimeStatus) (BrowserEvidenceRecord, error) {
	root, err := filepath.Abs(browserWorkspaceRoot(ctx))
	if err != nil {
		return BrowserEvidenceRecord{}, fmt.Errorf("browser_screenshot: resolve workspace: %w", err)
	}
	ownerDir := safeBrowserOwnerDir(browserSessionOwner(ctx))
	dir := filepath.Join(root, ".mauler", "browser-evidence", ownerDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BrowserEvidenceRecord{}, fmt.Errorf("browser_screenshot: create evidence directory: %w", err)
	}
	sum := sha256.Sum256(pngBytes)
	digest := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	id := fmt.Sprintf("browser-%d-%s", now.UnixNano(), digest[:12])
	imagePath := filepath.Join(dir, id+".png")
	manifestPath := filepath.Join(dir, id+".json")
	if err := writeExclusiveFile(imagePath, pngBytes, 0o600); err != nil {
		return BrowserEvidenceRecord{}, fmt.Errorf("browser_screenshot: persist PNG: %w", err)
	}
	imageRel, _ := filepath.Rel(root, imagePath)
	manifestRel, _ := filepath.Rel(root, manifestPath)
	record := BrowserEvidenceRecord{
		Version: browserEvidenceVersion, ID: id, Kind: "screenshot", CapturedAt: now.Format(time.RFC3339Nano),
		WorkspaceFile: filepath.ToSlash(imageRel), ManifestFile: filepath.ToSlash(manifestRel),
		SHA256: digest, Size: int64(len(pngBytes)), Width: width, Height: height, FullPage: fullPage,
		URL: sanitizeBrowserEvidenceURL(status.URL), Title: compactBrowserEvidenceTitle(status.Title),
		Fresh: true, CurrentSHA256: digest, ReviewDetail: "SHA-256 matches the captured screenshot bytes",
	}
	manifestRecord := record
	manifestRecord.Fresh = false
	manifestRecord.CurrentSHA256 = ""
	manifestRecord.ReviewDetail = ""
	raw, err := json.MarshalIndent(manifestRecord, "", "  ")
	if err != nil {
		_ = os.Remove(imagePath)
		return BrowserEvidenceRecord{}, fmt.Errorf("browser_screenshot: encode evidence manifest: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeExclusiveFile(manifestPath, raw, 0o600); err != nil {
		_ = os.Remove(imagePath)
		return BrowserEvidenceRecord{}, fmt.Errorf("browser_screenshot: persist evidence manifest: %w", err)
	}
	return record, nil
}

func writeExclusiveFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written := 0
	for written < len(data) {
		n, writeErr := file.Write(data[written:])
		written += n
		if writeErr != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return writeErr
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// ListBrowserEvidence re-hashes every bounded manifest entry under the current
// workspace. Changed/missing files remain visible as stale; their captured
// fingerprint is never overwritten by the current bytes.
func ListBrowserEvidence(root string) ([]BrowserEvidenceRecord, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("browser evidence workspace is unavailable")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve browser evidence workspace: %w", err)
	}
	base := filepath.Join(root, ".mauler", "browser-evidence")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return []BrowserEvidenceRecord{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("read browser evidence directory: %w", err)
	}
	var manifestPaths []string
	err = filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			manifestPaths = append(manifestPaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate browser evidence: %w", err)
	}
	if len(manifestPaths) > maxBrowserEvidenceRecords {
		sort.Slice(manifestPaths, func(i, j int) bool { return manifestPaths[i] > manifestPaths[j] })
		manifestPaths = manifestPaths[:maxBrowserEvidenceRecords]
	}
	records := make([]BrowserEvidenceRecord, 0, len(manifestPaths))
	for _, manifestPath := range manifestPaths {
		record, err := readBrowserEvidenceRecord(root, manifestPath)
		if err != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CapturedAt == records[j].CapturedAt {
			return records[i].ID > records[j].ID
		}
		return records[i].CapturedAt > records[j].CapturedAt
	})
	return records, nil
}

func readBrowserEvidenceRecord(root, manifestPath string) (BrowserEvidenceRecord, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return BrowserEvidenceRecord{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBrowserManifestBytes+1))
	closeErr := file.Close()
	if err != nil {
		return BrowserEvidenceRecord{}, err
	}
	if closeErr != nil {
		return BrowserEvidenceRecord{}, closeErr
	}
	if len(raw) > maxBrowserManifestBytes {
		return BrowserEvidenceRecord{}, fmt.Errorf("browser evidence manifest exceeds %d bytes", maxBrowserManifestBytes)
	}
	var record BrowserEvidenceRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return BrowserEvidenceRecord{}, err
	}
	if record.Version != browserEvidenceVersion || record.Kind != "screenshot" || strings.TrimSpace(record.ID) == "" || len(record.SHA256) != 64 {
		return BrowserEvidenceRecord{}, fmt.Errorf("invalid browser evidence manifest")
	}
	manifestRel, _ := filepath.Rel(root, manifestPath)
	record.ManifestFile = filepath.ToSlash(manifestRel)
	imagePath, err := containedBrowserEvidencePath(root, record.WorkspaceFile)
	if err != nil {
		record.Fresh = false
		record.ReviewDetail = err.Error()
		return record, nil
	}
	info, err := os.Stat(imagePath)
	if err != nil || !info.Mode().IsRegular() {
		record.Fresh = false
		record.ReviewDetail = "captured screenshot is missing or not a regular file"
		return record, nil
	}
	current, err := browserFileSHA256(imagePath)
	if err != nil {
		record.Fresh = false
		record.ReviewDetail = "could not re-hash captured screenshot: " + err.Error()
		return record, nil
	}
	record.CurrentSHA256 = current
	record.Fresh = info.Size() == record.Size && strings.EqualFold(current, record.SHA256)
	if record.Fresh {
		record.ReviewDetail = "SHA-256 and size match the captured screenshot bytes"
	} else {
		record.ReviewDetail = "current file bytes differ from the immutable captured fingerprint"
	}
	return record, nil
}

func containedBrowserEvidencePath(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("browser evidence path is not workspace-relative")
	}
	candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("browser evidence path escapes the workspace")
	}
	realRoot, rootErr := filepath.EvalSymlinks(root)
	realCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if rootErr == nil && candidateErr == nil {
		realRel, realErr := filepath.Rel(realRoot, realCandidate)
		if realErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("browser evidence path resolves outside the workspace")
		}
	}
	return candidate, nil
}

func sanitizeBrowserEvidenceURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func compactBrowserEvidenceTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160]) + "…"
	}
	return value
}
