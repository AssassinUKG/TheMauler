package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/tools"
)

type verifyWriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

type verifyEditParams struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	Old       string `json:"old"`
	New       string `json:"new"`
}

func verifyMutationResult(tc llm.ToolCallDef) string {
	switch tc.Function.Name {
	case "write_file", "write":
		return verifyWriteFileMutation(tc)
	case "edit_file", "edit":
		return verifyEditFileMutation(tc)
	default:
		return ""
	}
}

func verifyWriteFileMutation(tc llm.ToolCallDef) string {
	toolName := "write"
	if tc.Function.Name == "write_file" {
		toolName = "write_file"
	}
	var p verifyWriteParams
	if err := json.Unmarshal(tc.Function.Arguments, &p); err != nil {
		return fmt.Sprintf("Verification failed: could not parse %s arguments: %v", toolName, err)
	}
	vf, verifyErr := readVerifiedFile(p.Path)
	if verifyErr != "" {
		return verifyErr
	}

	var status string
	if p.Append {
		if p.Content != "" && !strings.HasSuffix(string(vf.data), p.Content) {
			status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but it does not end with the appended content.", vf.displayPath, vf.size)
		} else {
			status = fmt.Sprintf("Verification: append confirmed for %s (%d bytes).", vf.displayPath, vf.size)
		}
	} else if string(vf.data) != p.Content {
		status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but file content differs from %s input (%d bytes).", vf.displayPath, vf.size, toolName, len(p.Content))
	} else {
		status = fmt.Sprintf("Verification: write confirmed for %s (%d bytes).", vf.displayPath, vf.size)
	}
	return appendLint(status, vf)
}

func verifyEditFileMutation(tc llm.ToolCallDef) string {
	toolName := "edit"
	if tc.Function.Name == "edit_file" {
		toolName = "edit_file"
	}
	var p verifyEditParams
	if err := json.Unmarshal(tc.Function.Arguments, &p); err != nil {
		return fmt.Sprintf("Verification failed: could not parse %s arguments: %v", toolName, err)
	}
	if p.OldString == "" {
		p.OldString = p.Old
	}
	if p.NewString == "" {
		p.NewString = p.New
	}
	vf, verifyErr := readVerifiedFile(p.Path)
	if verifyErr != "" {
		return verifyErr
	}
	content := string(vf.data)

	var status string
	if p.NewString != "" && !strings.Contains(content, p.NewString) {
		status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but new_string was not found after edit.", vf.displayPath, vf.size)
	} else if p.OldString != "" && p.OldString != p.NewString && !strings.Contains(p.NewString, p.OldString) && strings.Contains(content, p.OldString) {
		status = fmt.Sprintf("Verification warning: %s exists (%d bytes), but old_string is still present after edit.", vf.displayPath, vf.size)
	} else {
		status = fmt.Sprintf("Verification: edit confirmed for %s (%d bytes).", vf.displayPath, vf.size)
	}
	return appendLint(status, vf)
}

// verifiedFile is the result of reading a just-mutated file back for
// verification. hostPath is set only when the file lives on the Windows host (so
// it can be linted); WSL-routed files leave it empty.
type verifiedFile struct {
	data        []byte
	size        int64
	displayPath string
	hostPath    string
}

func readVerifiedFile(rawPath string) (verifiedFile, string) {
	raw := strings.TrimSpace(rawPath)
	if raw == "" {
		return verifiedFile{}, "Verification failed: tool arguments did not include a path."
	}
	// Mirror write_file's routing: a Linux-absolute path on a WSL-backed host was
	// written inside WSL, so it must be read back inside WSL too — os.Stat on the
	// Windows host would look for \tmp\... and fail.
	if tools.ShouldUseWSLForPath(raw) {
		display := filepath.ToSlash(strings.ReplaceAll(raw, "\\", "/"))
		data, err := tools.ReadFileViaWSL(raw)
		if err != nil {
			return verifiedFile{}, fmt.Sprintf("Verification failed: read %s (WSL): %v", display, err)
		}
		return verifiedFile{data: data, size: int64(len(data)), displayPath: display}, ""
	}
	path := tools.NormalizeHostPath(raw)
	info, err := os.Stat(path)
	if err != nil {
		return verifiedFile{}, fmt.Sprintf("Verification failed: stat %s: %v", filepath.ToSlash(path), err)
	}
	if info.IsDir() {
		return verifiedFile{}, fmt.Sprintf("Verification failed: %s is a directory, not a file.", filepath.ToSlash(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return verifiedFile{}, fmt.Sprintf("Verification failed: read %s: %v", filepath.ToSlash(path), err)
	}
	return verifiedFile{data: data, size: info.Size(), displayPath: filepath.ToSlash(path), hostPath: path}, ""
}

func appendLint(status string, vf verifiedFile) string {
	if vf.hostPath == "" {
		return status // WSL-routed file: host linters can't reach it
	}
	if lintOut := lintFile(vf.hostPath); lintOut != "" {
		return status + "\n" + lintOut
	}
	return status
}
