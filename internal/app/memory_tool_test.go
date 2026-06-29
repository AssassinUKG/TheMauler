package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func newMemoryToolTestApp(t *testing.T) *memoryTool {
	t.Helper()
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
		registry: tools.New(),
	}
	return &memoryTool{app: app}
}

func runMemoryTool(t *testing.T, tool *memoryTool, args memoryToolArgs) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool.Run(context.Background(), raw)
}

func TestMemoryToolRememberThenRecall(t *testing.T) {
	tool := newMemoryToolTestApp(t)

	out, err := runMemoryTool(t, tool, memoryToolArgs{
		Action:     "remember",
		Title:      "connected.htb host entry",
		Content:    "Target connected.htb resolves to 10.129.12.172; add it to /etc/hosts before web enumeration.",
		Kind:       "fact",
		Importance: 4,
		Tags:       []string{"htb", "dns"},
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if !strings.Contains(out, "Saved memory") {
		t.Fatalf("remember output = %q, want a save confirmation", out)
	}

	out, err = runMemoryTool(t, tool, memoryToolArgs{Action: "recall", Query: "connected.htb hosts"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(out, "10.129.12.172") {
		t.Fatalf("recall output = %q, want the stored host detail", out)
	}

	// The agent tag is applied automatically so curated vs learned entries differ.
	entries, err := loadMemory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("stored %d entries, want 1", len(entries))
	}
	hasAgentTag := false
	for _, tag := range entries[0].Tags {
		if tag == "agent" {
			hasAgentTag = true
		}
	}
	if !hasAgentTag {
		t.Fatalf("entry tags = %v, want an \"agent\" tag", entries[0].Tags)
	}
}

func TestMemoryToolRecallEmptyStore(t *testing.T) {
	tool := newMemoryToolTestApp(t)
	out, err := runMemoryTool(t, tool, memoryToolArgs{Action: "recall", Query: "anything"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "no memory") {
		t.Fatalf("empty recall = %q, want a no-results message", out)
	}
}

func TestMemoryToolRememberRequiresContent(t *testing.T) {
	tool := newMemoryToolTestApp(t)
	if _, err := runMemoryTool(t, tool, memoryToolArgs{Action: "remember", Title: "x"}); err == nil {
		t.Fatal("remember without content should error")
	}
}

func TestMemoryToolRejectsUnknownAction(t *testing.T) {
	tool := newMemoryToolTestApp(t)
	if _, err := runMemoryTool(t, tool, memoryToolArgs{Action: "forget"}); err == nil {
		t.Fatal("unknown action should error")
	}
}

func TestMemoryToolDisabledShortCircuits(t *testing.T) {
	tool := newMemoryToolTestApp(t)
	tool.app.cfg.Memory.Enabled = false
	out, err := runMemoryTool(t, tool, memoryToolArgs{Action: "recall", Query: "x"})
	if err != nil {
		t.Fatalf("recall while disabled: %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("disabled recall = %q, want a disabled notice", out)
	}
}

func TestMemoryToolRegisteredAndEnabled(t *testing.T) {
	if _, ok := settings.DefaultSettings().Tools.EnabledTools["memory"]; !ok {
		t.Fatal("memory tool missing from default EnabledTools")
	}
	enabled := settings.EffectiveEnabledTools(settings.DefaultSettings().Tools)
	if !enabled["memory"] {
		t.Fatalf("memory tool not enabled by EffectiveEnabledTools in default toolset")
	}
}
