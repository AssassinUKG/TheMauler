package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/settings"
	"mauler/internal/store"
)

type StorageItem struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Bytes       int64  `json:"bytes"`
	Size        string `json:"size"`
	Clearable   bool   `json:"clearable"`
	Description string `json:"description"`
}

func (a *App) ListStorageItems() ([]StorageItem, error) {
	configDir, err := settings.ConfigDir()
	if err != nil {
		return nil, err
	}
	stateDB, _ := store.DefaultPath()
	ledgerPath, _ := ledger.DefaultPath()
	benchPath, _ := benchmarkRunsPath()
	taskPath, _ := taskRunsPath()
	memPath, _ := memoryPath()
	sessDir, _ := sessionsDir()
	skillDir, _ := skillsDir()

	items := []StorageItem{
		storageItem("config", "Config directory", configDir, "folder", false, "Settings, profiles, memory, logs, caches, and app state."),
		storageItem("state_db", "SQLite state database", stateDB, "database", false, "Session recall, task logs, ledger mirror, todos, memory, learning decisions, and checkpoints."),
		storageItem("benchmark_runs", "Benchmark history", benchPath, "file", true, "Saved model benchmark and context-sweep results."),
		storageItem("task_runs", "Legacy task-run JSON", taskPath, "file", true, "Older task-run log file kept for migration/backward compatibility."),
		storageItem("ledger", "RunLedger JSONL", ledgerPath, "file", true, "Canonical append-only event stream used by Brain and diagnostics."),
		storageItem("sessions", "Saved chat sessions", sessDir, "folder", false, "Saved chat transcripts; session recall index can be cleared separately."),
		storageItem("session_recall", "Session recall index", stateDB, "database", true, "Search index for saved/autosaved sessions inside the SQLite database."),
		storageItem("checkpoints", "Resumable run checkpoints", stateDB, "database", true, "Crash/resume snapshots for unfinished agent runs."),
		storageItem("memory", "Memory file", memPath, "file", false, "Durable project/user memory. Manage this from Memory/Brain."),
		storageItem("skills", "Local skills", skillDir, "folder", false, "User-approved procedural skills."),
	}
	return items, nil
}

func (a *App) ClearStorageItem(id string) error {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "benchmark_runs":
		return a.ClearBenchmarkRuns()
	case "task_runs":
		return a.ClearTaskRuns()
	case "ledger":
		return a.ClearLedgerEvents()
	case "session_recall":
		return a.ClearSessionRecall()
	case "checkpoints":
		return a.clearRunCheckpoints()
	default:
		return fmt.Errorf("storage item %q cannot be cleared here", id)
	}
}

func storageItem(id, label, path, kind string, clearable bool, description string) StorageItem {
	bytes := pathSize(path)
	if kind == "database" {
		bytes = databaseSize(path)
	}
	return StorageItem{
		ID:          id,
		Label:       label,
		Path:        filepath.Clean(path),
		Kind:        kind,
		Bytes:       bytes,
		Size:        formatBytes(bytes),
		Clearable:   clearable,
		Description: description,
	}
}

func pathSize(path string) int64 {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func databaseSize(path string) int64 {
	return pathSize(path) + pathSize(path+"-wal") + pathSize(path+"-shm")
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit && exp < 4; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (a *App) clearRunCheckpoints() error {
	if a != nil && a.db != nil {
		return clearRunCheckpointsDB(a.db)
	}
	db, err := store.OpenDefault()
	if err != nil {
		return err
	}
	defer db.Close()
	return clearRunCheckpointsDB(db)
}

func clearRunCheckpointsDB(db *sql.DB) error {
	_, err := db.Exec(`delete from run_checkpoints`)
	if err != nil {
		return fmt.Errorf("clear run checkpoints: %w", err)
	}
	return nil
}
