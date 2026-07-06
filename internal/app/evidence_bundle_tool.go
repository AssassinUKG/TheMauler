package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/tools"
)

type evidenceBundleTool struct{ app *App }

func (t *evidenceBundleTool) Name() string { return "evidence_bundle" }

func (t *evidenceBundleTool) Destructive() bool { return false }

func (t *evidenceBundleTool) Description() string {
	return "Create a compact Markdown evidence bundle from selected files/artifacts and short previews. Use when reporting findings or preserving target evidence instead of pasting large raw outputs into chat."
}

func (t *evidenceBundleTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "title": {"type": "string", "description": "Short bundle title, e.g. boxname.htb service evidence"},
    "target": {"type": "string", "description": "Optional target host/IP/name"},
    "paths": {"type": "array", "items": {"type": "string"}, "description": "Files or directories to include. Directories are scanned shallowly for common evidence files."},
    "notes": {"type": "string", "description": "Optional analyst notes or finding summary"},
    "max_files": {"type": "integer", "minimum": 1, "maximum": 40, "description": "Maximum files to include. Default 18."},
    "preview_bytes": {"type": "integer", "minimum": 200, "maximum": 8000, "description": "Max preview bytes per file. Default 1200."}
  }
}`)
}

type evidenceBundleArgs struct {
	Title        string   `json:"title"`
	Target       string   `json:"target"`
	Paths        []string `json:"paths"`
	Notes        string   `json:"notes"`
	MaxFiles     int      `json:"max_files"`
	PreviewBytes int      `json:"preview_bytes"`
}

type evidenceFile struct {
	Path    string
	Size    int64
	ModTime time.Time
	Preview string
}

func (t *evidenceBundleTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args evidenceBundleArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("evidence_bundle: bad params: %w", err)
		}
	}
	maxFiles := args.MaxFiles
	if maxFiles <= 0 || maxFiles > 40 {
		maxFiles = 18
	}
	previewBytes := args.PreviewBytes
	if previewBytes <= 0 || previewBytes > 8000 {
		previewBytes = 1200
	}
	paths := args.Paths
	if len(paths) == 0 {
		paths = defaultEvidencePaths()
	}
	files := collectEvidenceFiles(paths, maxFiles, previewBytes)
	if len(files) == 0 && strings.TrimSpace(args.Notes) == "" {
		return "No evidence files found. Pass explicit paths, or run scans/probes first so there are artifacts to bundle.", nil
	}
	bundlePath, err := evidenceBundlePath(args.Title, args.Target)
	if err != nil {
		return "", err
	}
	content := renderEvidenceBundle(args, files, bundlePath)
	if err := os.WriteFile(bundlePath, []byte(content), 0o640); err != nil {
		return "", err
	}
	t.app.recordLedger(ledger.Event{
		Kind:      "pipeline",
		Source:    "tool",
		Tool:      t.Name(),
		Status:    "done",
		Message:   firstNonEmpty(strings.TrimSpace(args.Title), "Evidence bundle"),
		Detail:    fmt.Sprintf("%d files", len(files)),
		Output:    evidenceBundleSummary(bundlePath, files),
		Artifacts: []string{filepath.ToSlash(bundlePath)},
		Metadata: map[string]string{
			"target":    strings.TrimSpace(args.Target),
			"max_files": fmt.Sprintf("%d", maxFiles),
		},
	})
	return evidenceBundleSummary(bundlePath, files), nil
}

func defaultEvidencePaths() []string {
	return []string{".mauler_artifacts", "scans", "notes", "loot", "screenshots", "report.md", "writeup.md"}
}

func collectEvidenceFiles(paths []string, maxFiles, previewBytes int) []evidenceFile {
	var candidates []string
	seen := map[string]bool{}
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p := tools.NormalizeHostPath(raw)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if !seen[p] && looksEvidenceFile(p) {
				seen[p] = true
				candidates = append(candidates, p)
			}
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil || len(candidates) >= maxFiles*3 {
				return nil
			}
			if d.IsDir() {
				if path != p && strings.HasPrefix(d.Name(), ".") && d.Name() != ".mauler_artifacts" {
					return filepath.SkipDir
				}
				if rel, relErr := filepath.Rel(p, path); relErr == nil && strings.Count(rel, string(os.PathSeparator)) > 2 {
					return filepath.SkipDir
				}
				return nil
			}
			if !looksEvidenceFile(path) || seen[path] {
				return nil
			}
			seen[path] = true
			candidates = append(candidates, path)
			return nil
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ii, _ := os.Stat(candidates[i])
		jj, _ := os.Stat(candidates[j])
		if ii == nil || jj == nil {
			return candidates[i] < candidates[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	if len(candidates) > maxFiles {
		candidates = candidates[:maxFiles]
	}
	out := make([]evidenceFile, 0, len(candidates))
	for _, p := range candidates {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, evidenceFile{
			Path:    filepath.ToSlash(p),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Preview: readEvidencePreview(p, previewBytes),
		})
	}
	return out
}

func looksEvidenceFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch ext {
	case ".txt", ".md", ".log", ".nmap", ".gnmap", ".xml", ".json", ".csv", ".html", ".req", ".resp":
		return true
	default:
		return strings.Contains(name, "scan") || strings.Contains(name, "proof") || strings.Contains(name, "evidence")
	}
}

func readEvidencePreview(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	text = strings.ReplaceAll(text, "\x00", "")
	return strings.TrimSpace(text)
}

func evidenceBundlePath(title, target string) (string, error) {
	slug := evidenceSlug(firstNonEmpty(target, title, "evidence"))
	dir := filepath.Join(".mauler_artifacts", "evidence_bundle")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%s.md", slug, time.Now().Format("20060102_150405"))), nil
}

func evidenceSlug(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(text, "-")
	text = strings.Trim(text, "-._")
	if text == "" {
		return "evidence"
	}
	if len(text) > 60 {
		text = text[:60]
	}
	return text
}

func renderEvidenceBundle(args evidenceBundleArgs, files []evidenceFile, bundlePath string) string {
	var sb strings.Builder
	title := firstNonEmpty(strings.TrimSpace(args.Title), "Evidence Bundle")
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("- Created: " + time.Now().Format(time.RFC3339) + "\n")
	if target := strings.TrimSpace(args.Target); target != "" {
		sb.WriteString("- Target: " + target + "\n")
	}
	sb.WriteString("- Bundle: " + filepath.ToSlash(bundlePath) + "\n")
	sb.WriteString("- Files: " + fmt.Sprintf("%d", len(files)) + "\n")
	if notes := strings.TrimSpace(args.Notes); notes != "" {
		sb.WriteString("\n## Notes\n\n" + notes + "\n")
	}
	sb.WriteString("\n## Evidence Files\n")
	if len(files) == 0 {
		sb.WriteString("\nNo files were included.\n")
	}
	for _, file := range files {
		sb.WriteString("\n### " + file.Path + "\n\n")
		sb.WriteString(fmt.Sprintf("- Size: %d bytes\n", file.Size))
		sb.WriteString("- Modified: " + file.ModTime.Format(time.RFC3339) + "\n")
		if file.Preview != "" {
			sb.WriteString("\n```text\n")
			sb.WriteString(file.Preview)
			if int64(len(file.Preview)) < file.Size {
				sb.WriteString("\n...[preview truncated]")
			}
			sb.WriteString("\n```\n")
		}
	}
	return sb.String()
}

func evidenceBundleSummary(bundlePath string, files []evidenceFile) string {
	var sb strings.Builder
	sb.WriteString("Evidence bundle created: " + filepath.ToSlash(bundlePath) + "\n")
	sb.WriteString(fmt.Sprintf("Files included: %d", len(files)))
	for _, file := range files {
		sb.WriteString("\n- " + file.Path)
	}
	return sb.String()
}
