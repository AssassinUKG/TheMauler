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
}

func verifyMutationResult(tc llm.ToolCallDef) string {
	switch tc.Function.Name {
	case "write_file":
		return verifyWriteFileMutation(tc)
	case "edit_file":
		return verifyEditFileMutation(tc)
	default:
		return ""
	}
}

func verifyWriteFileMutation(tc llm.ToolCallDef) string {
	var p verifyWriteParams
	if err := json.Unmarshal(tc.Function.Arguments, &p); err != nil {
		return fmt.Sprintf("Verification failed: could not parse write_file arguments: %v", err)
	}
	path := tools.NormalizeHostPath(p.Path)
	data, info, verifyErr := readVerifiedFile(path)
	if verifyErr != "" {
		return verifyErr
	}

	var status string
	if p.Append {
		if p.Content != "" && !strings.HasSuffix(string(data), p.Content) {
			status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but it does not end with the appended content.", filepath.ToSlash(path), info.Size())
		} else {
			status = fmt.Sprintf("Verification: append confirmed for %s (%d bytes).", filepath.ToSlash(path), info.Size())
		}
	} else if string(data) != p.Content {
		status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but file content differs from write_file input (%d bytes).", filepath.ToSlash(path), info.Size(), len(p.Content))
	} else {
		status = fmt.Sprintf("Verification: write confirmed for %s (%d bytes).", filepath.ToSlash(path), info.Size())
	}
	return appendLint(status, path)
}

func verifyEditFileMutation(tc llm.ToolCallDef) string {
	var p verifyEditParams
	if err := json.Unmarshal(tc.Function.Arguments, &p); err != nil {
		return fmt.Sprintf("Verification failed: could not parse edit_file arguments: %v", err)
	}
	path := tools.NormalizeHostPath(p.Path)
	data, info, verifyErr := readVerifiedFile(path)
	if verifyErr != "" {
		return verifyErr
	}
	content := string(data)

	var status string
	if p.NewString != "" && !strings.Contains(content, p.NewString) {
		status = fmt.Sprintf("Verification failed: %s exists (%d bytes), but new_string was not found after edit.", filepath.ToSlash(path), info.Size())
	} else if p.OldString != "" && p.OldString != p.NewString && !strings.Contains(p.NewString, p.OldString) && strings.Contains(content, p.OldString) {
		status = fmt.Sprintf("Verification warning: %s exists (%d bytes), but old_string is still present after edit.", filepath.ToSlash(path), info.Size())
	} else {
		status = fmt.Sprintf("Verification: edit confirmed for %s (%d bytes).", filepath.ToSlash(path), info.Size())
	}
	return appendLint(status, path)
}

func readVerifiedFile(path string) ([]byte, os.FileInfo, string) {
	if strings.TrimSpace(path) == "" {
		return nil, nil, "Verification failed: tool arguments did not include a path."
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Sprintf("Verification failed: stat %s: %v", filepath.ToSlash(path), err)
	}
	if info.IsDir() {
		return nil, nil, fmt.Sprintf("Verification failed: %s is a directory, not a file.", filepath.ToSlash(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Sprintf("Verification failed: read %s: %v", filepath.ToSlash(path), err)
	}
	return data, info, ""
}

func appendLint(status, path string) string {
	if lintOut := lintFile(path); lintOut != "" {
		return status + "\n" + lintOut
	}
	return status
}
