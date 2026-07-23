package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/tools"
)

type fileChangeSnapshot struct {
	Exists      bool
	DisplayPath string
	Size        int64
	SHA256      string
	Error       string
}

func snapshotToolTarget(tc llm.ToolCallDef) fileChangeSnapshot {
	path := fileChangePath(tc)
	return snapshotFileChange(path)
}

func fileChangePath(tc llm.ToolCallDef) string {
	switch tc.Function.Name {
	case "write_file", "write":
		var p verifyWriteParams
		if err := json.Unmarshal(tc.Function.Arguments, &p); err == nil {
			return strings.TrimSpace(p.Path)
		}
	case "edit_file", "edit":
		var p verifyEditParams
		if err := json.Unmarshal(tc.Function.Arguments, &p); err == nil {
			return strings.TrimSpace(p.Path)
		}
	}
	return strings.TrimSpace(extractPath(tc))
}

func snapshotFileChange(path string) fileChangeSnapshot {
	path = strings.TrimSpace(path)
	snap := fileChangeSnapshot{DisplayPath: displayFileChangePath(path)}
	if path == "" {
		snap.Error = "path missing"
		return snap
	}
	vf, verifyErr := readVerifiedFile(path)
	if verifyErr != "" {
		snap.Error = verifyErr
		return snap
	}
	sum := sha256.Sum256(vf.data)
	snap.Exists = true
	snap.DisplayPath = vf.displayPath
	snap.Size = vf.size
	snap.SHA256 = hex.EncodeToString(sum[:])
	return snap
}

func displayFileChangePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if tools.ShouldUseWSLForPath(path) {
		return filepath.ToSlash(strings.ReplaceAll(path, "\\", "/"))
	}
	return filepath.ToSlash(tools.NormalizeHostPath(path))
}

func (a *App) recordFileChange(runID string, tc llm.ToolCallDef, before fileChangeSnapshot, verification string, durationMs int64) {
	if a == nil || !isWriteTool(tc.Function.Name) {
		return
	}
	after := snapshotToolTarget(tc)
	if !after.Exists {
		a.recordLedger(ledger.Event{
			RunID:      runID,
			Kind:       "file_change",
			Source:     "tool",
			Tool:       tc.Function.Name,
			Status:     "unverified",
			Message:    "file mutation could not be verified",
			Detail:     firstNonEmpty(after.Error, verification),
			Input:      string(tc.Function.Arguments),
			DurationMs: durationMs,
			Files:      []string{firstNonEmpty(after.DisplayPath, before.DisplayPath, fileChangePath(tc))},
			Metadata: map[string]string{
				"action":        "unknown",
				"before_exists": fmt.Sprintf("%t", before.Exists),
				"after_exists":  fmt.Sprintf("%t", after.Exists),
			},
		})
		return
	}

	action := "modified"
	if !before.Exists {
		action = "created"
	}
	status := action
	if strings.Contains(strings.ToLower(verification), "verification failed") {
		status = action + "_warning"
	}
	a.recordLedger(ledger.Event{
		RunID:      runID,
		Kind:       "file_change",
		Source:     "tool",
		Tool:       tc.Function.Name,
		Status:     status,
		Message:    fmt.Sprintf("%s %s", action, after.DisplayPath),
		Detail:     verification,
		Input:      string(tc.Function.Arguments),
		DurationMs: durationMs,
		Files:      []string{after.DisplayPath},
		Metadata: map[string]string{
			"action":         action,
			"before_exists":  fmt.Sprintf("%t", before.Exists),
			"before_size":    fmt.Sprintf("%d", before.Size),
			"before_sha256":  before.SHA256,
			"after_exists":   fmt.Sprintf("%t", after.Exists),
			"after_size":     fmt.Sprintf("%d", after.Size),
			"after_sha256":   after.SHA256,
			"cleanup_hint":   cleanupHint(action),
			"verification":   verificationStatus(verification),
			"requested_path": fileChangePath(tc),
		},
	})
}

func cleanupHint(action string) string {
	if action == "created" {
		return "safe candidate for cleanup if it was temporary and is not a requested deliverable"
	}
	return "modified existing file; do not delete during cleanup unless the user explicitly asks"
}

func verificationStatus(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case lower == "":
		return "not_recorded"
	case strings.Contains(lower, "verification failed"):
		return "failed"
	case strings.Contains(lower, "verification warning"):
		return "warning"
	case strings.Contains(lower, "verification:"):
		return "confirmed"
	default:
		return "recorded"
	}
}
