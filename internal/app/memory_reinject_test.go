package app

import (
	"os"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func newReinjectTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	return &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(8192),
		rollback: &agent.Rollback{},
		registry: tools.New(),
	}
}

func countSystemMessages(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == llm.RoleSystem {
			n++
		}
	}
	return n
}

func TestMaybeReinjectMemorySurfacesAndDedups(t *testing.T) {
	app := newReinjectTestApp(t)
	if _, err := app.AddMemory("nmap service scan", "Use nmap -sV for service/version detection on the target.", []string{"nmap", "recon"}); err != nil {
		t.Fatal(err)
	}

	app.history.Append(llm.NewTextMessage(llm.RoleUser, "Now run nmap against the target to enumerate services."))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, "Scanning ports."))

	run := startTaskRun("enumerate", "Ops", "default", "model")
	injected := map[string]bool{}
	count := 0

	app.maybeReinjectMemory(&run, *app.cfg, injected, &count)
	if count != 1 {
		t.Fatalf("expected one re-injection, got count=%d", count)
	}
	msgs := app.history.Messages()
	if countSystemMessages(msgs) != 1 {
		t.Fatalf("expected one system message appended, got %d", countSystemMessages(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleSystem || !strings.Contains(messageText(last), "nmap -sV") {
		t.Fatalf("re-injected note missing expected memory content: %q", messageText(last))
	}

	// Second call with the same context must not re-inject the same entry.
	app.maybeReinjectMemory(&run, *app.cfg, injected, &count)
	if count != 1 {
		t.Fatalf("dedup failed: count=%d after second call", count)
	}
	if countSystemMessages(app.history.Messages()) != 1 {
		t.Fatalf("dedup failed: %d system messages", countSystemMessages(app.history.Messages()))
	}
}

func TestMaybeReinjectMemoryRespectsCap(t *testing.T) {
	app := newReinjectTestApp(t)
	if _, err := app.AddMemory("nmap tip", "Use nmap -sV against the target.", []string{"nmap"}); err != nil {
		t.Fatal(err)
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "nmap the target"))
	run := startTaskRun("x", "Ops", "default", "model")
	injected := map[string]bool{}
	count := maxMemoryReinjections // already at cap
	app.maybeReinjectMemory(&run, *app.cfg, injected, &count)
	if countSystemMessages(app.history.Messages()) != 0 {
		t.Fatal("re-injection should be skipped once the per-run cap is reached")
	}
}

func TestMaybeReinjectMemoryNoMatchNoInject(t *testing.T) {
	app := newReinjectTestApp(t)
	if _, err := app.AddMemory("smb shares", "Enumerate SMB shares with smbclient.", []string{"smb"}); err != nil {
		t.Fatal(err)
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "compile the rust binary and run cargo test"))
	run := startTaskRun("x", "Ops", "default", "model")
	injected := map[string]bool{}
	count := 0
	app.maybeReinjectMemory(&run, *app.cfg, injected, &count)
	if count != 0 || countSystemMessages(app.history.Messages()) != 0 {
		t.Fatal("unrelated context should not surface SMB memory")
	}
}

func TestMaybeReinjectMemoryWithholdsConflictingTarget(t *testing.T) {
	app := newReinjectTestApp(t)
	app.cfg.Context.Lab.Target = "10.129.15.218"
	if _, err := app.SaveMemoryEntry(MemoryEntry{
		Title:      "Old connected target",
		Content:    "Target: 10.129.12.172. Use the FreePBX exploit notes from that run.",
		Tags:       []string{"target-10-129-12-172", "htb", "run"},
		Kind:       "fact",
		Confidence: "confirmed",
		Source:     "previous_run",
		Importance: 5,
	}); err != nil {
		t.Fatal(err)
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "Use FreePBX notes for 10.129.15.218"))
	run := startTaskRun("x", "Ops", "default", "model")
	injected := map[string]bool{}
	count := 0

	app.maybeReinjectMemory(&run, *app.cfg, injected, &count)
	if count != 0 || countSystemMessages(app.history.Messages()) != 0 {
		t.Fatalf("conflicting target memory should not be re-injected, count=%d system=%d", count, countSystemMessages(app.history.Messages()))
	}
	if len(run.Events) == 0 || run.Events[len(run.Events)-1].Kind != "memory_conflict" {
		t.Fatalf("expected memory_conflict event, got %#v", run.Events)
	}
}

func TestMemoryConflictSummaryCapsExamples(t *testing.T) {
	conflicts := []string{
		"target old",
		"target old",
		"host stale",
		"ip mismatch",
		"workspace mismatch",
		"tool conflict",
		"run conflict",
	}
	got := memoryConflictSummary(conflicts)
	for _, want := range []string{"withheld_conflicts=6", "shown_examples=5", "... 1 more withheld conflict omitted"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n- ") != 5 {
		t.Fatalf("summary should show five examples, got:\n%s", got)
	}
}
