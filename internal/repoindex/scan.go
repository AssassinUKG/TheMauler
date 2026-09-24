// Package repoindex builds deterministic, content-addressed manifests for
// workspace files without placing whole repositories in memory or model input.
package repoindex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

const (
	ExtractorVersion = "stream-text-v1"
	TrustLabel       = "untrusted_repository_content"

	StatusIndexed        = "indexed"
	StatusExcluded       = "excluded"
	StatusDuplicate      = "duplicate"
	StatusUnsupported    = "unsupported"
	StatusBinary         = "unsupported_binary"
	StatusSymlinkSkipped = "symlink_skipped"
	StatusSizeSkipped    = "size_skipped"
	StatusBudgetSkipped  = "total_budget_skipped"
	StatusDecodeError    = "decode_error"
	StatusReadError      = "read_error"
	StatusExtractorError = "extractor_error"
	StatusExpansionLimit = "expansion_limit"
	StatusExtractorLimit = "extractor_timeout"
	StatusEncrypted      = "encrypted_unsupported"
)

var defaultExcludedDirs = map[string]struct{}{
	".git": {}, ".cache": {}, ".next": {}, "__pycache__": {},
	"build": {}, "dist": {}, "node_modules": {}, "target": {}, "vendor": {},
}

// IndexPolicy is code-owned scan policy. A zero byte limit is unlimited.
type IndexPolicy struct {
	Roots           []string        `json:"roots"`
	Include         []string        `json:"include,omitempty"`
	Exclude         []string        `json:"exclude,omitempty"`
	MaxFileBytes    int64           `json:"max_file_bytes"`
	MaxTotalBytes   int64           `json:"max_total_bytes"`
	FollowSymlinks  bool            `json:"follow_symlinks"`
	ExtractorPolicy ExtractorPolicy `json:"extractor_policy"`
}

type ExtractorPolicy struct {
	TryUnknownText    bool  `json:"try_unknown_text"`
	ChunkLines        int   `json:"chunk_lines"`
	ChunkBytes        int   `json:"chunk_bytes"`
	MaxExpandedBytes  int64 `json:"max_expanded_bytes"`
	MaxArchiveEntries int   `json:"max_archive_entries"`
	TimeoutMillis     int   `json:"timeout_millis"`
}

type ManifestEntry struct {
	Root       string    `json:"root"`
	Path       string    `json:"path"`
	Language   string    `json:"language,omitempty"`
	Encoding   string    `json:"encoding,omitempty"`
	Extractor  string    `json:"extractor,omitempty"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	SHA256     string    `json:"sha256,omitempty"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	ChunkCount int       `json:"chunk_count"`
}

type ManifestNotice struct {
	Root   string `json:"root"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Chunk struct {
	ID               string `json:"id"`
	Root             string `json:"root"`
	Path             string `json:"path"`
	Ordinal          int    `json:"ordinal"`
	StartLine        int    `json:"start_line"`
	EndLine          int    `json:"end_line"`
	Text             string `json:"text"`
	TextSHA256       string `json:"text_sha256"`
	TrustLabel       string `json:"trust_label"`
	ExtractorVersion string `json:"extractor_version"`
}

type Manifest struct {
	Version      int              `json:"version"`
	PolicyDigest string           `json:"policy_digest"`
	Digest       string           `json:"digest"`
	Complete     bool             `json:"complete"`
	Cancelled    bool             `json:"cancelled"`
	Entries      []ManifestEntry  `json:"entries"`
	Notices      []ManifestNotice `json:"notices,omitempty"`
	FilesSeen    int              `json:"files_seen"`
	FilesIndexed int              `json:"files_indexed"`
	BytesRead    int64            `json:"bytes_read"`
	Chunks       int              `json:"chunks"`
	RefreshMode  string           `json:"refresh_mode,omitempty"`
	FilesReused  int              `json:"files_reused,omitempty"`
	FilesChanged int              `json:"files_changed,omitempty"`
	FilesDeleted int              `json:"files_deleted,omitempty"`
}

// ReusedFile identifies an indexed file whose source SHA-256 still matches a
// previous generation. Its chunks may be copied transactionally without
// rerunning the extractor.
type ReusedFile struct {
	Root string
	Path string
}

type IncrementalStats struct {
	Reused  []ReusedFile
	Changed int
	Deleted int
}

type FileStamp struct {
	Root    string
	Path    string
	Size    int64
	ModTime time.Time
}

type ChunkSink func(context.Context, Chunk) error

// Progress is metadata-only scan progress. CurrentPath is workspace-relative;
// source content is never copied into progress events.
type Progress struct {
	FilesSeen    int    `json:"files_seen"`
	FilesIndexed int    `json:"files_indexed"`
	BytesRead    int64  `json:"bytes_read"`
	Chunks       int    `json:"chunks"`
	CurrentPath  string `json:"current_path,omitempty"`
	Status       string `json:"status,omitempty"`
}

type ProgressSink func(Progress)

func DefaultPolicy(roots ...string) IndexPolicy {
	return IndexPolicy{
		Roots: roots,
		ExtractorPolicy: ExtractorPolicy{
			ChunkLines:        200,
			ChunkBytes:        32 * 1024,
			MaxExpandedBytes:  16 * 1024 * 1024,
			MaxArchiveEntries: 4096,
			TimeoutMillis:     15_000,
		},
	}
}

// Scan creates a complete deterministic manifest. It returns a partial,
// Complete=false manifest alongside cancellation or sink errors so omissions
// remain visible and a caller cannot mistake partial coverage for success.
func Scan(ctx context.Context, policy IndexPolicy, sink ChunkSink) (Manifest, error) {
	return ScanWithProgress(ctx, policy, sink, nil)
}

// ScanWithProgress creates a deterministic manifest and emits bounded,
// metadata-only progress after each visited file. The callback must return
// quickly; it runs on the scanner goroutine.
func ScanWithProgress(ctx context.Context, policy IndexPolicy, sink ChunkSink, progressSink ProgressSink) (Manifest, error) {
	manifest, _, err := scanWithPrevious(ctx, policy, sink, progressSink, nil)
	return manifest, err
}

// ScanIncrementalWithProgress verifies prior file hashes and reuses only exact
// matches. Changed/new files still pass through the ordinary bounded extractor.
func ScanIncrementalWithProgress(ctx context.Context, policy IndexPolicy, previous Manifest, sink ChunkSink, progressSink ProgressSink) (Manifest, IncrementalStats, error) {
	return scanWithPrevious(ctx, policy, sink, progressSink, &previous)
}

func scanWithPrevious(ctx context.Context, policy IndexPolicy, sink ChunkSink, progressSink ProgressSink, previous *Manifest) (Manifest, IncrementalStats, error) {
	policy, roots, err := normalizePolicy(policy)
	manifest := Manifest{Version: 1, PolicyDigest: policyDigest(policy), RefreshMode: "full"}
	stats := IncrementalStats{}
	if err != nil {
		return manifest, stats, err
	}
	previousEntries := map[string]ManifestEntry{}
	currentEntries := map[string]struct{}{}
	if previous != nil && previous.Complete && previous.PolicyDigest == manifest.PolicyDigest {
		manifest.RefreshMode = "incremental"
		for _, entry := range previous.Entries {
			previousEntries[manifestEntryKey(entry.Root, entry.Path)] = entry
		}
	}
	seen := map[string]struct{}{}
	var totalAccepted int64
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			rel := relativeSlash(root, path)
			if walkErr != nil {
				manifest.Notices = append(manifest.Notices, ManifestNotice{Root: filepath.ToSlash(root), Path: rel, Status: StatusReadError, Detail: walkErr.Error()})
				return nil
			}
			if path == root && !d.IsDir() {
				rel = filepath.Base(root)
			}
			if d.IsDir() {
				if path != root && shouldExcludeDirectory(rel, d.Name(), policy.Exclude) {
					manifest.Notices = append(manifest.Notices, ManifestNotice{Root: filepath.ToSlash(root), Path: rel, Status: StatusExcluded, Detail: "directory excluded by scan policy"})
					return filepath.SkipDir
				}
				return nil
			}

			manifest.FilesSeen++
			entry := ManifestEntry{Root: filepath.ToSlash(root), Path: rel}
			entryKey := manifestEntryKey(entry.Root, entry.Path)
			currentEntries[entryKey] = struct{}{}
			if progressSink != nil {
				progressSink(Progress{FilesSeen: manifest.FilesSeen, FilesIndexed: manifest.FilesIndexed, BytesRead: manifest.BytesRead, Chunks: manifest.Chunks, CurrentPath: rel, Status: "scanning"})
				defer func() {
					progressSink(Progress{FilesSeen: manifest.FilesSeen, FilesIndexed: manifest.FilesIndexed, BytesRead: manifest.BytesRead, Chunks: manifest.Chunks, CurrentPath: rel, Status: entry.Status})
				}()
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				entry.Status, entry.Detail = StatusReadError, infoErr.Error()
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			entry.Size, entry.ModTime = info.Size(), info.ModTime().UTC()
			if d.Type()&os.ModeSymlink != 0 {
				entry.Status = StatusSymlinkSkipped
				entry.Detail = "symlinks are not followed"
				if policy.FollowSymlinks {
					entry.Detail = "file symlink following is not enabled until canonical target ownership is persisted"
				}
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			if !info.Mode().IsRegular() {
				entry.Status, entry.Detail = StatusUnsupported, "not a regular file"
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			canonical, canonicalErr := filepath.EvalSymlinks(path)
			if canonicalErr != nil {
				entry.Status, entry.Detail = StatusReadError, canonicalErr.Error()
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			canonical, _ = filepath.Abs(canonical)
			if !containedBy(root, canonical) {
				entry.Status, entry.Detail = StatusSymlinkSkipped, "canonical path escapes the selected root"
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			key := strings.ToLower(filepath.Clean(canonical))
			if _, exists := seen[key]; exists {
				entry.Status, entry.Detail = StatusDuplicate, "same canonical file already appears in this scan"
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			seen[key] = struct{}{}
			if excludedFile(rel, policy.Include, policy.Exclude) {
				entry.Status, entry.Detail = StatusExcluded, "file excluded by scan policy"
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			if policy.MaxFileBytes > 0 && entry.Size > policy.MaxFileBytes {
				entry.Status = StatusSizeSkipped
				entry.Detail = fmt.Sprintf("size %d exceeds max_file_bytes %d", entry.Size, policy.MaxFileBytes)
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			if policy.MaxTotalBytes > 0 && totalAccepted+entry.Size > policy.MaxTotalBytes {
				entry.Status = StatusBudgetSkipped
				entry.Detail = fmt.Sprintf("file would exceed max_total_bytes %d", policy.MaxTotalBytes)
				manifest.Entries = append(manifest.Entries, entry)
				return nil
			}
			totalAccepted += entry.Size
			if prior, ok := previousEntries[entryKey]; ok && prior.Status == StatusIndexed && prior.SHA256 != "" && prior.Size == entry.Size {
				digest, verifiedBytes, hashErr := hashRegularFile(ctx, canonical)
				manifest.BytesRead += verifiedBytes
				if hashErr != nil {
					entry.Status, entry.Detail = StatusReadError, hashErr.Error()
					manifest.Entries = append(manifest.Entries, entry)
					stats.Changed++
					return nil
				}
				if digest == prior.SHA256 {
					prior.Root, prior.Path, prior.Size, prior.ModTime = entry.Root, entry.Path, entry.Size, entry.ModTime
					entry = prior
					manifest.FilesIndexed++
					manifest.Chunks += entry.ChunkCount
					stats.Reused = append(stats.Reused, ReusedFile{Root: entry.Root, Path: entry.Path})
					manifest.Entries = append(manifest.Entries, entry)
					return nil
				}
			}
			stats.Changed++
			if err := indexFile(ctx, canonical, &entry, policy.ExtractorPolicy, sink, &manifest, progressSink); err != nil {
				manifest.Entries = append(manifest.Entries, entry)
				return err
			}
			manifest.Entries = append(manifest.Entries, entry)
			return nil
		})
		if walkErr != nil {
			manifest.Cancelled = errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)
			finalizeManifest(&manifest)
			return manifest, stats, walkErr
		}
	}
	if previous != nil && manifest.RefreshMode == "incremental" {
		for key := range previousEntries {
			if _, exists := currentEntries[key]; !exists {
				stats.Deleted++
			}
		}
	}
	manifest.FilesReused = len(stats.Reused)
	manifest.FilesChanged = stats.Changed
	manifest.FilesDeleted = stats.Deleted
	manifest.Complete = true
	finalizeManifest(&manifest)
	return manifest, stats, nil
}

func manifestEntryKey(root, path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(root)) + "\x00" + filepath.ToSlash(filepath.Clean(path)))
}

func hashRegularFile(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return "", read, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = hasher.Write(buffer[:n])
			read += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", read, readErr
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), read, nil
}

// Snapshot records only path/size/modtime metadata for cheap watch polling.
// A detected change is later verified by the SHA-based incremental scanner.
func Snapshot(ctx context.Context, policy IndexPolicy) ([]FileStamp, error) {
	policy, roots, err := normalizePolicy(policy)
	if err != nil {
		return nil, err
	}
	var stamps []FileStamp
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			rel := relativeSlash(root, path)
			if path == root && !d.IsDir() {
				rel = filepath.Base(root)
			}
			if d.IsDir() {
				if path != root && shouldExcludeDirectory(rel, d.Name(), policy.Exclude) {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			stamps = append(stamps, FileStamp{Root: filepath.ToSlash(root), Path: rel, Size: info.Size(), ModTime: info.ModTime().UTC()})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(stamps, func(i, j int) bool {
		return manifestEntryKey(stamps[i].Root, stamps[i].Path) < manifestEntryKey(stamps[j].Root, stamps[j].Path)
	})
	return stamps, nil
}

func normalizePolicy(policy IndexPolicy) (IndexPolicy, []string, error) {
	if len(policy.Roots) == 0 {
		return policy, nil, fmt.Errorf("repoindex: at least one root is required")
	}
	if policy.MaxFileBytes < 0 || policy.MaxTotalBytes < 0 {
		return policy, nil, fmt.Errorf("repoindex: byte limits cannot be negative")
	}
	if policy.ExtractorPolicy.ChunkLines <= 0 {
		policy.ExtractorPolicy.ChunkLines = 200
	}
	if policy.ExtractorPolicy.ChunkLines > 2000 {
		policy.ExtractorPolicy.ChunkLines = 2000
	}
	if policy.ExtractorPolicy.ChunkBytes <= 0 {
		policy.ExtractorPolicy.ChunkBytes = 32 * 1024
	}
	if policy.ExtractorPolicy.ChunkBytes < 1024 {
		policy.ExtractorPolicy.ChunkBytes = 1024
	}
	if policy.ExtractorPolicy.ChunkBytes > 256*1024 {
		policy.ExtractorPolicy.ChunkBytes = 256 * 1024
	}
	if policy.ExtractorPolicy.MaxExpandedBytes <= 0 {
		policy.ExtractorPolicy.MaxExpandedBytes = 16 * 1024 * 1024
	}
	if policy.ExtractorPolicy.MaxExpandedBytes < 64*1024 {
		policy.ExtractorPolicy.MaxExpandedBytes = 64 * 1024
	}
	if policy.ExtractorPolicy.MaxExpandedBytes > 256*1024*1024 {
		policy.ExtractorPolicy.MaxExpandedBytes = 256 * 1024 * 1024
	}
	if policy.ExtractorPolicy.MaxArchiveEntries <= 0 {
		policy.ExtractorPolicy.MaxArchiveEntries = 4096
	}
	if policy.ExtractorPolicy.MaxArchiveEntries > 100_000 {
		policy.ExtractorPolicy.MaxArchiveEntries = 100_000
	}
	if policy.ExtractorPolicy.TimeoutMillis <= 0 {
		policy.ExtractorPolicy.TimeoutMillis = 15_000
	}
	if policy.ExtractorPolicy.TimeoutMillis < 1_000 {
		policy.ExtractorPolicy.TimeoutMillis = 1_000
	}
	if policy.ExtractorPolicy.TimeoutMillis > 120_000 {
		policy.ExtractorPolicy.TimeoutMillis = 120_000
	}
	policy.Include = cleanPatterns(policy.Include)
	policy.Exclude = cleanPatterns(policy.Exclude)
	rootSet := map[string]struct{}{}
	var roots []string
	for _, supplied := range policy.Roots {
		root, err := filepath.Abs(strings.TrimSpace(supplied))
		if err != nil {
			return policy, nil, fmt.Errorf("repoindex: resolve root %q: %w", supplied, err)
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return policy, nil, fmt.Errorf("repoindex: resolve root %q: %w", supplied, err)
		}
		info, err := os.Stat(root)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			return policy, nil, fmt.Errorf("repoindex: root is not a readable file or directory: %s", root)
		}
		key := strings.ToLower(filepath.Clean(root))
		if _, exists := rootSet[key]; exists {
			continue
		}
		rootSet[key] = struct{}{}
		roots = append(roots, filepath.Clean(root))
	}
	sort.Slice(roots, func(i, j int) bool { return filepath.ToSlash(roots[i]) < filepath.ToSlash(roots[j]) })
	policy.Roots = make([]string, len(roots))
	for i, root := range roots {
		policy.Roots[i] = filepath.ToSlash(root)
	}
	return policy, roots, nil
}

func indexFile(ctx context.Context, path string, entry *ManifestEntry, policy ExtractorPolicy, sink ChunkSink, manifest *Manifest, progressSink ProgressSink) error {
	if richExtractorKind(path) != "" {
		return indexRichFile(ctx, path, entry, policy, sink, manifest, progressSink)
	}
	return indexTextFile(ctx, path, entry, policy, sink, manifest, progressSink)
}

func indexTextFile(ctx context.Context, path string, entry *ManifestEntry, policy ExtractorPolicy, sink ChunkSink, manifest *Manifest, progressSink ProgressSink) error {
	file, err := os.Open(path)
	if err != nil {
		entry.Status, entry.Detail = StatusReadError, err.Error()
		return nil
	}
	defer file.Close()

	buffered := bufio.NewReaderSize(file, 32*1024)
	prefix, _ := buffered.Peek(8192)
	encoding, _, bom, binary := detectEncoding(prefix)
	if binary {
		entry.Status, entry.Detail = StatusBinary, "binary sniff detected NUL/control-byte content"
		return nil
	}
	language, supported := classifyText(path)
	if !supported && !policy.TryUnknownText {
		entry.Status, entry.Detail = StatusUnsupported, "no text extractor for this extension or filename"
		return nil
	}
	hasher := sha256.New()
	counter := &countWriter{}
	raw := io.TeeReader(buffered, io.MultiWriter(hasher, counter))
	var decoded io.Reader = raw
	switch encoding {
	case "utf-16le":
		bomPolicy := textunicode.IgnoreBOM
		if bom {
			bomPolicy = textunicode.ExpectBOM
		}
		decoded = transform.NewReader(raw, textunicode.UTF16(textunicode.LittleEndian, bomPolicy).NewDecoder())
	case "utf-16be":
		bomPolicy := textunicode.IgnoreBOM
		if bom {
			bomPolicy = textunicode.ExpectBOM
		}
		decoded = transform.NewReader(raw, textunicode.UTF16(textunicode.BigEndian, bomPolicy).NewDecoder())
	}

	entry.Language = language
	entry.Encoding = encoding
	entry.Extractor = ExtractorVersion
	reader := bufio.NewReaderSize(decoded, 32*1024)
	line, ordinal, invalid := 1, 0, 0
	chunkStart := 1
	var chunk strings.Builder
	flush := func(endLine int) error {
		if chunk.Len() == 0 {
			return nil
		}
		ordinal++
		text := chunk.String()
		textHash := sha256.Sum256([]byte(text))
		textDigest := hex.EncodeToString(textHash[:])
		idHash := sha256.Sum256([]byte(ExtractorVersion + "\x00" + entry.Root + "\x00" + entry.Path + "\x00" + strconv.Itoa(chunkStart) + "\x00" + strconv.Itoa(endLine) + "\x00" + textDigest))
		item := Chunk{
			ID: hex.EncodeToString(idHash[:]), Root: entry.Root, Path: entry.Path, Ordinal: ordinal,
			StartLine: chunkStart, EndLine: endLine, Text: text, TextSHA256: textDigest,
			TrustLabel: TrustLabel, ExtractorVersion: ExtractorVersion,
		}
		if sink != nil {
			if err := sink(ctx, item); err != nil {
				entry.Status, entry.Detail = StatusReadError, "chunk sink: "+err.Error()
				return err
			}
		}
		entry.ChunkCount++
		manifest.Chunks++
		if progressSink != nil {
			progressSink(Progress{FilesSeen: manifest.FilesSeen, FilesIndexed: manifest.FilesIndexed, BytesRead: manifest.BytesRead, Chunks: manifest.Chunks, CurrentPath: entry.Path, Status: "scanning"})
		}
		chunk.Reset()
		chunkStart = line
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			entry.Status, entry.Detail = StatusReadError, err.Error()
			return err
		}
		r, size, readErr := reader.ReadRune()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			entry.Status, entry.Detail = StatusReadError, readErr.Error()
			return nil
		}
		if r == utf8.RuneError && size == 1 {
			invalid++
		}
		encodedLen := utf8.RuneLen(r)
		if encodedLen < 0 {
			encodedLen = 1
		}
		if chunk.Len() > 0 && chunk.Len()+encodedLen > policy.ChunkBytes {
			if err := flush(line); err != nil {
				return err
			}
		}
		chunk.WriteRune(r)
		if r == '\n' {
			if line-chunkStart+1 >= policy.ChunkLines {
				if err := flush(line); err != nil {
					return err
				}
			}
			line++
			if chunk.Len() == 0 {
				chunkStart = line
			}
		}
	}
	if err := flush(line); err != nil {
		return err
	}
	entry.SHA256 = hex.EncodeToString(hasher.Sum(nil))
	manifest.BytesRead += counter.n
	if counter.n != entry.Size {
		entry.Detail = fmt.Sprintf("file size changed during scan: stat=%d read=%d", entry.Size, counter.n)
		entry.Status = StatusReadError
		return nil
	}
	if invalid > 0 {
		entry.Status = StatusDecodeError
		entry.Detail = fmt.Sprintf("invalid UTF-8 byte sequences: %d", invalid)
		return nil
	}
	entry.Status = StatusIndexed
	entry.Size = counter.n
	manifest.FilesIndexed++
	return nil
}

type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func detectEncoding(prefix []byte) (name string, order textunicode.Endianness, bom, binary bool) {
	if len(prefix) >= 2 && prefix[0] == 0xff && prefix[1] == 0xfe {
		return "utf-16le", textunicode.LittleEndian, true, false
	}
	if len(prefix) >= 2 && prefix[0] == 0xfe && prefix[1] == 0xff {
		return "utf-16be", textunicode.BigEndian, true, false
	}
	if len(prefix) == 0 {
		return "utf-8", textunicode.LittleEndian, false, false
	}
	var evenNUL, oddNUL, controls int
	for i, b := range prefix {
		if b == 0 {
			if i%2 == 0 {
				evenNUL++
			} else {
				oddNUL++
			}
		}
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			controls++
		}
	}
	pairs := len(prefix) / 2
	if pairs > 4 && oddNUL*3 > pairs*2 && evenNUL*10 < pairs {
		return "utf-16le", textunicode.LittleEndian, false, false
	}
	if pairs > 4 && evenNUL*3 > pairs*2 && oddNUL*10 < pairs {
		return "utf-16be", textunicode.BigEndian, false, false
	}
	if evenNUL+oddNUL > 0 || controls*20 > len(prefix) {
		return "", textunicode.LittleEndian, false, true
	}
	return "utf-8", textunicode.LittleEndian, false, false
}

func classifyText(path string) (string, bool) {
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" || base == "makefile" || base == "license" || base == "readme" || base == "go.mod" || base == "go.sum" || strings.HasPrefix(base, ".env.example") {
		return base, true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown", ".rst", ".log":
		return "text", true
	case ".go":
		return "go", true
	case ".py":
		return "python", true
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript", true
	case ".ts", ".tsx":
		return "typescript", true
	case ".java":
		return "java", true
	case ".rs":
		return "rust", true
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		return "c-cpp", true
	case ".cs":
		return "csharp", true
	case ".sh", ".bash", ".zsh", ".ps1", ".bat", ".cmd":
		return "shell", true
	case ".json", ".jsonl", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".xml":
		return "config", true
	case ".html", ".htm", ".css", ".scss", ".less":
		return "web", true
	case ".sql":
		return "sql", true
	case ".csv", ".tsv":
		return "tabular", true
	case ".ipynb":
		return "notebook-json", true
	default:
		return "text", false
	}
}

func finalizeManifest(manifest *Manifest) {
	sort.Slice(manifest.Entries, func(i, j int) bool {
		left, right := manifest.Entries[i], manifest.Entries[j]
		return left.Root+"/"+left.Path < right.Root+"/"+right.Path
	})
	sort.Slice(manifest.Notices, func(i, j int) bool {
		left, right := manifest.Notices[i], manifest.Notices[j]
		return left.Root+"/"+left.Path+left.Status < right.Root+"/"+right.Path+right.Status
	})
	type digestEntry struct {
		Root, Path, SHA256, Status, Detail, Extractor, Encoding string
		Size, Chunks                                            int64
	}
	stable := struct {
		Version      int
		PolicyDigest string
		Complete     bool
		Cancelled    bool
		Entries      []digestEntry
		Notices      []ManifestNotice
	}{Version: manifest.Version, PolicyDigest: manifest.PolicyDigest, Complete: manifest.Complete, Cancelled: manifest.Cancelled, Notices: manifest.Notices}
	for _, entry := range manifest.Entries {
		stable.Entries = append(stable.Entries, digestEntry{
			Root: entry.Root, Path: entry.Path, SHA256: entry.SHA256, Status: entry.Status,
			Detail: entry.Detail, Extractor: entry.Extractor, Encoding: entry.Encoding,
			Size: entry.Size, Chunks: int64(entry.ChunkCount),
		})
	}
	data, _ := json.Marshal(stable)
	sum := sha256.Sum256(data)
	manifest.Digest = hex.EncodeToString(sum[:])
}

func policyDigest(policy IndexPolicy) string {
	data, _ := json.Marshal(policy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func relativeSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func containedBy(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cleanPatterns(patterns []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "./")
		if pattern == "" {
			continue
		}
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		out = append(out, pattern)
	}
	sort.Strings(out)
	return out
}

func shouldExcludeDirectory(rel, base string, patterns []string) bool {
	if _, exists := defaultExcludedDirs[strings.ToLower(base)]; exists {
		return true
	}
	for _, pattern := range patterns {
		if matchesPattern(pattern, rel) || matchesPattern(pattern, rel+"/") {
			return true
		}
	}
	return false
}

func excludedFile(rel string, include, exclude []string) bool {
	for _, pattern := range exclude {
		if matchesPattern(pattern, rel) {
			return true
		}
	}
	if len(include) == 0 {
		return false
	}
	for _, pattern := range include {
		if matchesPattern(pattern, rel) {
			return false
		}
	}
	return true
}

func matchesPattern(pattern, rel string) bool {
	pattern, rel = filepath.ToSlash(pattern), filepath.ToSlash(rel)
	if matched, _ := filepath.Match(pattern, rel); matched {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		if matched, _ := filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(rel)); matched {
			return true
		}
	}
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(rel, strings.TrimSuffix(pattern, "**"))
	}
	parts := strings.SplitN(pattern, "**", 2)
	return len(parts) == 2 && strings.HasPrefix(rel, parts[0]) && strings.HasSuffix(rel, parts[1])
}
