package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileOutlineReturnsSymbols(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.go")
	content := "package sample\n\nimport \"fmt\"\n\ntype Thing struct{}\n\nfunc DoThing() {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := (&FileOutline{}).Run(context.Background(), mustJSON(t, map[string]any{"path": path}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lines: 7", "type Thing struct{}", "func DoThing()"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in outline:\n%s", want, out)
		}
	}
}

func TestReadChunksReadsBoundedChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := (&ReadChunks{}).Run(context.Background(), mustJSON(t, map[string]any{
		"path":             path,
		"chunk_index":      2,
		"chunk_size_lines": 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "chunk 2/3") || !strings.Contains(out, "   3\tthree") || strings.Contains(out, "one") {
		t.Fatalf("unexpected chunk output:\n%s", out)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
