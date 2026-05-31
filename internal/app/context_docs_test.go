package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestBuildProjectInstructionsPromptLayersDocs(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "frontend", "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "frontend", "AGENTS.override.md"), "frontend override")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes:          4096,
		ProjectDocFallbackFilenames: []string{"MAULER.md", "AGENTS.md"},
	})
	if !strings.Contains(prompt, "root mauler") || !strings.Contains(prompt, "frontend override") {
		t.Fatalf("expected layered docs, got:\n%s", prompt)
	}
	if strings.Index(prompt, "root mauler") > strings.Index(prompt, "frontend override") {
		t.Fatalf("root instructions should appear before nested overrides:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptHonorsExplicitPath(t *testing.T) {
	root := t.TempDir()
	explicit := filepath.Join(root, "custom.md")
	mustWrite(t, explicit, strings.Repeat("x", 100))
	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		MAULERMDPath:       explicit,
		ProjectDocMaxBytes: 20,
	})
	if !strings.Contains(prompt, "(truncated)") {
		t.Fatalf("expected explicit doc to be truncated, got:\n%s", prompt)
	}
	if strings.Count(prompt, "x") > 25 {
		t.Fatalf("expected cap to apply, got:\n%s", prompt)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
