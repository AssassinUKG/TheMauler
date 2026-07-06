package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/ledger"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func newDistillTestApp(t *testing.T) *App {
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
		ledger:   ledger.New(filepath.Join(t.TempDir(), "ledger.jsonl")),
	}
}

func TestAutoDistillLearningsSavesReflection(t *testing.T) {
	app := newDistillTestApp(t)
	run := startTaskRun("attack the box", "Ops", "default", "model")
	run.attachLedger(app.ledger)

	// A tool error on this run becomes a high-importance reflection candidate.
	app.recordLedger(ledger.Event{
		RunID:  run.ID,
		Kind:   "tool_error",
		Source: "tool",
		Tool:   "shell",
		Status: "error",
		Error:  "nmap: command not found in PATH",
		Detail: "shell backend lacked nmap; install or use full path",
	})

	app.autoDistillLearnings(&run, app.cfg.Memory)

	entries, err := loadMemory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one distilled memory entry")
	}
	found := false
	for _, e := range entries {
		hasAuto := false
		for _, tag := range e.Tags {
			if tag == "auto" {
				hasAuto = true
			}
		}
		if hasAuto && strings.Contains(strings.ToLower(e.Content), "nmap") {
			found = true
		}
	}
	if !found {
		t.Fatalf("distilled memory missing the nmap reflection: %+v", entries)
	}
}

func TestAutoDistillLearningsDedupsAndCaps(t *testing.T) {
	app := newDistillTestApp(t)
	run := startTaskRun("x", "Ops", "default", "model")
	run.attachLedger(app.ledger)
	for i := 0; i < 5; i++ {
		app.recordLedger(ledger.Event{
			RunID:  run.ID,
			Kind:   "tool_error",
			Source: "tool",
			Tool:   "shell",
			Status: "error",
			Error:  "distinct failure number " + string(rune('a'+i)),
		})
	}
	app.autoDistillLearnings(&run, app.cfg.Memory)
	first, _ := loadMemory()
	if len(first) > maxAutoDistilledMemories {
		t.Fatalf("distilled %d entries, cap is %d", len(first), maxAutoDistilledMemories)
	}

	// Running distill again on the same run must not duplicate entries.
	app.autoDistillLearnings(&run, app.cfg.Memory)
	second, _ := loadMemory()
	if len(second) != len(first) {
		t.Fatalf("re-distill duplicated entries: before=%d after=%d", len(first), len(second))
	}
}

func TestAutoDistillLearningsCapturesCommandStorm(t *testing.T) {
	app := newDistillTestApp(t)
	run := startTaskRun("dump the hash", "Ops", "default", "model")
	run.attachLedger(app.ledger)
	// A storm of near-identical extraction requests against one endpoint.
	for i := 0; i < shellStormThreshold+2; i++ {
		args, _ := json.Marshal(map[string]string{"command": "curl -s http://connected.htb/admin/ajax.php?p=" + strings.Repeat("a", i)})
		run.Tools = append(run.Tools, TaskToolEvent{Name: "shell", Input: string(args), Status: "done"})
	}
	app.autoDistillLearnings(&run, app.cfg.Memory)
	entries, _ := loadMemory()
	found := false
	for _, e := range entries {
		for _, tag := range e.Tags {
			if tag == "inefficiency" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected an inefficiency lesson distilled from the command storm: %+v", entries)
	}
}

func TestAutoDistillLearningsDisabledNoop(t *testing.T) {
	app := newDistillTestApp(t)
	app.cfg.Memory.Enabled = false
	run := startTaskRun("x", "Ops", "default", "model")
	run.attachLedger(app.ledger)
	app.recordLedger(ledger.Event{RunID: run.ID, Kind: "tool_error", Status: "error", Error: "boom"})
	app.autoDistillLearnings(&run, app.cfg.Memory)
	entries, _ := loadMemory()
	if len(entries) != 0 {
		t.Fatalf("distill ran while memory disabled: %d entries", len(entries))
	}
}
