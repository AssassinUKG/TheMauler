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

func TestBuildProjectInstructionsPromptDoesNotAutoLoadMasterSkillsAlongsideProjectDoc(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "master_skills.md"), "master skill instructions")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "root mauler") {
		t.Fatalf("expected project doc to be loaded, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "master skill instructions") {
		t.Fatalf("master skills should not be auto-loaded as project docs:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptDoesNotAutoLoadSingularMasterSkillFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "master_skill.md"), "singular master skill instructions")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if strings.Contains(prompt, "singular master skill instructions") {
		t.Fatalf("master_skill.md should not be auto-loaded, got:\n%s", prompt)
	}
}

func TestBuildProjectInstructionsPromptDoesNotAutoLoadMasterSkillsDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "root mauler")
	mustWrite(t, filepath.Join(root, "master_skills", "01-build.md"), "build skill")
	mustWrite(t, filepath.Join(root, "master_skills", "nested", "02-review.md"), "review skill")
	mustWrite(t, filepath.Join(root, "master_skills", "ignored.txt"), "not markdown")

	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "root mauler") {
		t.Fatalf("expected project doc to be loaded, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "build skill") || strings.Contains(prompt, "review skill") || strings.Contains(prompt, "not markdown") {
		t.Fatalf("master_skills directory should not be auto-loaded, got:\n%s", prompt)
	}
}

func TestProjectInstructionFilenamesFiltersLegacyMasterSkillFallbacks(t *testing.T) {
	names := projectInstructionFilenames([]string{"MAULER.md", "master_skill.md", "master_skills", "AGENTS.md"})
	joined := strings.Join(names, " ")
	if strings.Contains(joined, "master_skill") || strings.Contains(joined, "master_skills") {
		t.Fatalf("legacy master skill names should be filtered: %#v", names)
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

func TestExplicitInstructionDirectoryPrioritizesMasterSkill(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "WIRING_DIAGRAM.md"), "wiring first alphabetically")
	mustWrite(t, filepath.Join(root, "SKILL.md"), "local skill")
	mustWrite(t, filepath.Join(root, "master_skill.md"), "navigator master brain")
	mustWrite(t, filepath.Join(root, "maps", "htb_methodology.md"), "htb map")

	prompt := buildProjectInstructionsPrompt(settings.ContextConfig{
		MAULERMDPath:       root,
		ProjectDocMaxBytes: 4096,
	})
	if !strings.Contains(prompt, "navigator master brain") {
		t.Fatalf("expected master_skill.md to load from explicit directory, got:\n%s", prompt)
	}
	if strings.Index(prompt, "navigator master brain") > strings.Index(prompt, "local skill") {
		t.Fatalf("master_skill.md should be loaded before adjacent SKILL.md:\n%s", prompt)
	}
	if strings.Index(prompt, "navigator master brain") > strings.Index(prompt, "wiring first alphabetically") {
		t.Fatalf("master_skill.md should be loaded before alphabetically earlier docs:\n%s", prompt)
	}
	if !strings.Contains(prompt, "read at most 1-3 targeted follow-up files") {
		t.Fatalf("expected anti-crawl guidance in explicit framework prompt:\n%s", prompt)
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
