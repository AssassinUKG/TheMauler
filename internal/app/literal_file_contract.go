package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mauler/internal/controlplane"
)

const exactFileByteLimit = 2 * 1024 * 1024

var exactCreateRE = regexp.MustCompile("(?i)^create\\s+(\\S+)\\s+containing exactly the text represented by this json string:\\s*")
var exactAppendRE = regexp.MustCompile("(?i)^append exactly the text represented by this json string to\\s+(\\S+),\\s+preserving every existing byte:\\s*")

func compileExactFileExpectationForAction(prompt, workspace string, concrete bool) (*controlplane.ExactFileExpectation, error) {
	if !concrete {
		return nil, nil
	}
	return compileExactFileExpectation(prompt, workspace)
}

// A deliberately bounded grammar, not an LLM-created verdict. Unknown mixed
// instructions retain the ordinary semantic rails in addition to this check.
func compileExactFileExpectation(prompt, workspace string) (*controlplane.ExactFileExpectation, error) {
	prompt = strings.TrimSpace(prompt)
	match := exactCreateRE.FindStringSubmatchIndex(prompt)
	appendMode := false
	if match == nil {
		match = exactAppendRE.FindStringSubmatchIndex(prompt)
		appendMode = true
	}
	if match == nil {
		return nil, nil
	}
	name := strings.Trim(prompt[match[2]:match[3]], "\"'\x60")
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("literal file target is outside the authoritative workspace")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("literal file target must not be a symbolic link")
	}
	decoder := json.NewDecoder(strings.NewReader(prompt[match[1]:]))
	var text string
	if err := decoder.Decode(&text); err != nil {
		return nil, fmt.Errorf("literal file payload must be one valid JSON string: %w", err)
	}
	if len(text) > 64*1024 {
		return nil, fmt.Errorf("literal file payload exceeds the 64 KiB contract limit")
	}
	expected := []byte(text)
	if appendMode {
		prefix, err := readExactFileBytes(path)
		if err != nil {
			return nil, fmt.Errorf("capture original append bytes: %w", err)
		}
		expected = append(prefix, expected...)
	}
	if len(expected) > exactFileByteLimit {
		return nil, fmt.Errorf("exact file expectation exceeds bounded byte limit")
	}
	tail := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(prompt[match[1]+int(decoder.InputOffset()):]))), " ")
	exclusive := false
	switch tail {
	case "", ".", ". then read back and verify the complete file.",
		". then read back and verify the complete file, and finish the handoff in chat. do not use shell or create any other deliverable.",
		". use append=true, then read back and verify the complete file. finish the handoff in chat. do not use shell or create any other deliverable.":
		exclusive = true
	}
	digest := sha256.Sum256(expected)
	return &controlplane.ExactFileExpectation{Path: path, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(expected)), Exclusive: exclusive}, nil
}

func readExactFileBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > exactFileByteLimit {
		return nil, fmt.Errorf("exact file observation is unavailable or over budget")
	}
	bytes, err := io.ReadAll(io.LimitReader(file, exactFileByteLimit+1))
	if err != nil {
		return nil, err
	}
	if len(bytes) > exactFileByteLimit || int64(len(bytes)) != info.Size() {
		return nil, fmt.Errorf("exact file changed during bounded observation")
	}
	return bytes, nil
}

func exactFileEvidence(run TaskRun, expected *controlplane.ExactFileExpectation) (string, bool) {
	if expected == nil || run.Contract == nil || run.Contract.Validate() != nil {
		return "", false
	}
	bytes, err := readExactFileBytes(expected.Path)
	if err != nil || int64(len(bytes)) != expected.Size {
		return "", false
	}
	digest := sha256.Sum256(bytes)
	if hex.EncodeToString(digest[:]) != expected.SHA256 {
		return "", false
	}
	id := "file_sha256:" + filepath.ToSlash(expected.Path) + ":" + expected.SHA256
	// A pre-existing matching file or a model's declaration is not this run's
	// accepted mutation. Require its original controller-emitted byte witness.
	for _, event := range run.Events {
		if event.Kind == "control_evidence" && event.Message == id {
			return id, true
		}
	}
	return "", false
}

func exactFileCompletionVerdicts(run TaskRun) ([]VerifyVerdict, bool) {
	if run.Contract == nil {
		return nil, false
	}
	var verdicts []VerifyVerdict
	exclusive := false
	for _, check := range run.Contract.AcceptanceChecks {
		if check.ExactFile == nil || check.Verifier != "file_exact_bytes" {
			continue
		}
		verdict := VerifyVerdict{Gate: "literal_file", Status: "fail", Blocking: true, Summary: "Exact requested artifact bytes are not verified.", Improvements: []string{"Restore the exact operator-requested bytes, preserving the captured append prefix, then verify this file."}}
		if id, ok := exactFileEvidence(run, check.ExactFile); ok {
			verdict.Status = "pass"
			verdict.Blocking = false
			verdict.Summary = "Current bytes match the original exact-file contract."
			verdict.Evidence = id
		}
		verdicts = append(verdicts, verdict)
		exclusive = check.ExactFile.Exclusive && len(run.Contract.Deliverables) == 1
	}
	return verdicts, exclusive && len(verdicts) == 1
}
