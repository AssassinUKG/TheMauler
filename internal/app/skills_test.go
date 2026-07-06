package app

import (
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestRelevantSkillsDoesNotAutoInjectMasterUnlessExplicitlyRequested(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "master_skill.md")
	mustWrite(t, source, "unique navigator workflow instructions")
	if _, _, err := saveMasterSkillSource(source); err != nil {
		t.Fatal(err)
	}
	cfg := settings.SkillsConfig{Enabled: true, AutoInject: true, MaxInject: 3}

	if got := relevantSkills(cfg, "please follow the navigator workflow"); len(got) != 0 {
		t.Fatalf("master skill should not auto-inject unless explicitly requested: %#v", got)
	}
	got := relevantSkills(cfg, "use the master skill for this task")
	if len(got) != 1 || got[0].Name != "master" || strings.Contains(got[0].Body, "unique navigator workflow instructions") {
		t.Fatalf("expected explicit master skill request to inject only lazy metadata, got: %#v", got)
	}
	if strings.Contains(got[0].Body, filepath.ToSlash(filepath.Dir(source))) {
		t.Fatalf("master skill lazy body should not duplicate absolute source path: %#v", got[0])
	}
	if got[0].SourcePath == "" {
		t.Fatal("master skill should keep source path internally for lazy loading")
	}
}

func TestBuildSystemPromptPointsMasterRequestsAtSkillView(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "master_skill.md")
	mustWrite(t, source, "# Master\n\nUse this workflow.")
	if _, _, err := saveMasterSkillSource(source); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"

	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Ops"}, nil, nil)

	for _, want := range []string{"registered as skill `master`", "call skill with mode=view and name `master`", "instead of searching the workspace for master_skill.md"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSaveMasterSkillSourceUsesTheMaulerAdapterContract(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "master_skill.md")
	mustWrite(t, source, "# Master\n\nUse this workflow.")
	skill, _, err := saveMasterSkillSource(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"TheMauler's system prompt",
		"focused query",
		"terminal_send",
		"evidence policy",
		"Do not load or summarize the entire external source",
	} {
		if !strings.Contains(skill.Body, want) {
			t.Fatalf("master adapter body missing %q:\n%s", want, skill.Body)
		}
	}
}

func TestMasterSkillRequestedForPentestTerms(t *testing.T) {
	for _, prompt := range []string{
		"research exploits for the web target",
		"continue HTB foothold",
		"verify CVE payload",
	} {
		if !masterSkillRequested(keywordSet(prompt)) {
			t.Fatalf("master skill should be requested for pentest prompt %q", prompt)
		}
	}
}

func TestSkillRequirementFrontmatterRoundTrip(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	saved, err := saveSkill(Skill{
		Name:          "http-probe",
		Description:   "Use for probing HTTP services",
		Version:       "1.0.0",
		Tags:          []string{"http", "probe"},
		RequiredTools: []string{"shell", "fetch_url"},
		ShellBackend:  "wsl",
		NeedsNetwork:  true,
		NeedsWrite:    true,
		Body:          "Run a compact probe.",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"required_tools: [fetch_url, shell]",
		"shell_backend: wsl",
		"needs_network: true",
		"needs_write: true",
	} {
		if !strings.Contains(saved.Raw, want) {
			t.Fatalf("saved skill missing %q:\n%s", want, saved.Raw)
		}
	}
	loaded, err := loadSkill("http-probe")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.NeedsNetwork || !loaded.NeedsWrite || loaded.ShellBackend != "wsl" {
		t.Fatalf("requirements did not round-trip: %#v", loaded)
	}
	if strings.Join(loaded.RequiredTools, ",") != "fetch_url,shell" {
		t.Fatalf("required tools did not round-trip sorted/deduped: %#v", loaded.RequiredTools)
	}
}

func TestRelevantSkillsAnnotatesUnavailableRequirements(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	_, err := saveSkill(Skill{
		Name:          "shell-probe",
		Description:   "Use for shell probe work",
		Version:       "1.0.0",
		Tags:          []string{"probe"},
		RequiredTools: []string{"shell", "write"},
		ShellBackend:  "wsl",
		NeedsWrite:    true,
		Body:          "Run the probe.",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Skills = settings.SkillsConfig{Enabled: true, AutoInject: true, MaxInject: 3}
	cfg.Tools.ActiveToolset = "safe"
	cfg.Tools.ShellBackend = "powershell"

	got := relevantSkillsForSettings(cfg.Skills, cfg, "please do probe work")
	if len(got) != 1 {
		t.Fatalf("expected one relevant skill, got %#v", got)
	}
	body := got[0].Body
	for _, want := range []string{"Tool availability note:", "required tool \"shell\"", "write/edit tools", "expects shell backend \"wsl\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("skill body missing availability warning %q:\n%s", want, body)
		}
	}
}
