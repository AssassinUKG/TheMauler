package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestVerifyWriteFileMutationConfirmsExactContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := verifyMutationResult(toolCallForTest("write_file", map[string]any{
		"path":    path,
		"content": "hello",
	}))

	if !strings.Contains(out, "Verification: write confirmed") || !strings.Contains(out, "out.txt") {
		t.Fatalf("unexpected verification output: %q", out)
	}
}

func TestVerifyWriteFileMutationDetectsContentMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("actual"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := verifyMutationResult(toolCallForTest("write_file", map[string]any{
		"path":    path,
		"content": "expected",
	}))

	if !strings.Contains(out, "Verification failed") || !strings.Contains(out, "differs from write_file input") {
		t.Fatalf("expected mismatch warning, got %q", out)
	}
}

func TestVerifyWriteFileMutationConfirmsAppendSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("before\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := verifyMutationResult(toolCallForTest("write_file", map[string]any{
		"path":    path,
		"content": "after\n",
		"append":  true,
	}))

	if !strings.Contains(out, "Verification: append confirmed") {
		t.Fatalf("expected append confirmation, got %q", out)
	}
}

func TestVerifyEditFileMutationConfirmsNewString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("alpha BETA gamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := verifyMutationResult(toolCallForTest("edit_file", map[string]any{
		"path":       path,
		"old_string": "beta",
		"new_string": "BETA",
	}))

	if !strings.Contains(out, "Verification: edit confirmed") {
		t.Fatalf("expected edit confirmation, got %q", out)
	}
}

func TestVerifyEditFileMutationDetectsMissingNewString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := verifyMutationResult(toolCallForTest("edit_file", map[string]any{
		"path":       path,
		"old_string": "beta",
		"new_string": "BETA",
	}))

	if !strings.Contains(out, "Verification failed") || !strings.Contains(out, "new_string was not found") {
		t.Fatalf("expected failed edit verification, got %q", out)
	}
}

func toolCallForTest(name string, args map[string]any) llm.ToolCallDef {
	raw, _ := json.Marshal(args)
	return llm.ToolCallDef{
		ID:   "test",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      name,
			Arguments: raw,
		},
	}
}
