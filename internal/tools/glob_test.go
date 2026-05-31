package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGlob(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&Glob{}).Run(context.Background(), raw)
}

func makeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{
		"main.go",
		"utils.go",
		"README.md",
		"sub/helper.go",
		"sub/data.json",
		"sub/nested/deep.go",
		"node_modules/pkg/index.js", // should be skipped
	}
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGlobDoublestarGoFiles(t *testing.T) {
	root := makeTree(t)
	out, err := runGlob(t, map[string]any{"pattern": "**/*.go", "dir": root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "sub/helper.go") || !strings.Contains(out, "sub/nested/deep.go") {
		t.Fatalf("missing expected files: %q", out)
	}
	if strings.Contains(out, "README.md") {
		t.Fatalf("README.md should not match **/*.go")
	}
}

func TestGlobSkipsNodeModules(t *testing.T) {
	root := makeTree(t)
	out, err := runGlob(t, map[string]any{"pattern": "**/*.js", "dir": root})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("node_modules should be skipped: %q", out)
	}
}

func TestGlobSimpleExtension(t *testing.T) {
	root := makeTree(t)
	out, err := runGlob(t, map[string]any{"pattern": "*.md", "dir": root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "README.md") {
		t.Fatalf("expected README.md: %q", out)
	}
}

func TestGlobNoMatch(t *testing.T) {
	root := makeTree(t)
	out, err := runGlob(t, map[string]any{"pattern": "**/*.py", "dir": root})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no files matched" {
		t.Fatalf("expected no match message, got %q", out)
	}
}

func TestGlobMissingPatternError(t *testing.T) {
	_, err := runGlob(t, map[string]any{"pattern": ""})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestGlobSubdirPattern(t *testing.T) {
	root := makeTree(t)
	out, err := runGlob(t, map[string]any{"pattern": "sub/**/*.go", "dir": root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "helper.go") || !strings.Contains(out, "deep.go") {
		t.Fatalf("expected sub go files: %q", out)
	}
	if strings.Contains(out, "main.go") {
		t.Fatalf("main.go should not match sub/**/*.go: %q", out)
	}
}

func TestGlobDoublestarMatchesFilename(t *testing.T) {
	if !globDoublestar("**/*.go", "sub/nested/file.go") {
		t.Error("should match")
	}
	if globDoublestar("**/*.go", "sub/nested/file.ts") {
		t.Error("should not match")
	}
	if !globDoublestar("**/*.go", "file.go") {
		t.Error("root file should match")
	}
}

func TestGlobDoublestarWithPrefix(t *testing.T) {
	if !globDoublestar("src/**/*.ts", "src/components/App.ts") {
		t.Error("should match src prefix")
	}
	if globDoublestar("src/**/*.ts", "lib/foo.ts") {
		t.Error("should not match different prefix")
	}
}

func TestShouldSkipDir(t *testing.T) {
	for _, skip := range []string{".git", "node_modules", "vendor", "dist", "build", "target"} {
		if !shouldSkipDir(skip) {
			t.Errorf("%s should be skipped", skip)
		}
	}
	if shouldSkipDir("src") || shouldSkipDir("internal") {
		t.Error("normal dirs should not be skipped")
	}
}
