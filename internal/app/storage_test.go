package app

import (
	"os"
	"path/filepath"
	"testing"

	"mauler/internal/ledger"
)

func TestStorageItemsIncludeClearableLogsAndBenchmarks(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(t.Context()) })
	if err := saveBenchmarkRun(ProfileBenchmarkResult{ID: "bench-test", Summary: "ok"}); err != nil {
		t.Fatalf("save benchmark: %v", err)
	}
	if err := saveAgentEvalReport(AgentEvalReport{ID: "eval-test", Profile: "mock"}); err != nil {
		t.Fatalf("save eval: %v", err)
	}
	app.recordLedger(ledger.Event{Kind: "test", Message: "hello"})

	items, err := app.ListStorageItems()
	if err != nil {
		t.Fatalf("ListStorageItems: %v", err)
	}
	byID := map[string]StorageItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, id := range []string{"config", "state_db", "benchmark_runs", "agent_eval_runs", "ledger", "session_recall", "checkpoints"} {
		if byID[id].ID == "" {
			t.Fatalf("missing storage item %q in %#v", id, items)
		}
	}
	if !byID["benchmark_runs"].Clearable || !byID["agent_eval_runs"].Clearable || !byID["ledger"].Clearable {
		t.Fatalf("expected benchmark/eval/ledger to be clearable: %#v", byID)
	}
	if byID["config"].Clearable || byID["memory"].Clearable {
		t.Fatalf("config/memory should not be one-click clearable: %#v", byID)
	}
}

func TestClearStorageItemClearsAgentEvalHistory(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(t.Context()) })
	if err := saveAgentEvalReport(AgentEvalReport{ID: "eval-test", Profile: "mock"}); err != nil {
		t.Fatalf("save eval: %v", err)
	}
	path, err := agentEvalReportsPath()
	if err != nil {
		t.Fatalf("eval path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("eval file before clear: %v", err)
	}
	if err := app.ClearStorageItem("agent_eval_runs"); err != nil {
		t.Fatalf("ClearStorageItem: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("eval file after clear err=%v path=%s", err, filepath.Clean(path))
	}
}

func TestClearStorageItemClearsBenchmarkHistory(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(t.Context()) })
	if err := saveBenchmarkRun(ProfileBenchmarkResult{ID: "bench-test", Summary: "ok"}); err != nil {
		t.Fatalf("save benchmark: %v", err)
	}
	path, err := benchmarkRunsPath()
	if err != nil {
		t.Fatalf("benchmark path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("benchmark file before clear: %v", err)
	}
	if err := app.ClearStorageItem("benchmark_runs"); err != nil {
		t.Fatalf("ClearStorageItem: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("benchmark file after clear err=%v path=%s", err, filepath.Clean(path))
	}
}
