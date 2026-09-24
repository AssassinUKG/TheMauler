package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/ledger"
	"mauler/internal/settings"
	maulerstore "mauler/internal/store"
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

func newRepositoryMemoryToolTestApp(t *testing.T) (*memoryTool, string) {
	t.Helper()
	tool := newMemoryToolTestApp(t)
	root := tool.app.GetWorkingDir()
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tool.app.db = db
	tool.app.ledger = ledger.New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	tool.app.ledger.AttachDB(db)
	return tool, root
}

func TestMemoryToolIndexesAndSearchesOnlyCurrentWorkspace(t *testing.T) {
	tool, root := newRepositoryMemoryToolTestApp(t)
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte("package service\n// SentinelWorkspace proves indexed retrieval.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("OutsideWorkspaceSentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	indexed, err := runMemoryTool(t, tool, memoryToolArgs{Action: "index_workspace"})
	if err != nil {
		t.Fatalf("index_workspace: %v", err)
	}
	if !strings.Contains(indexed, "Repository index activated") || !strings.Contains(indexed, filepath.ToSlash(root)) || !strings.Contains(indexed, "manifest: sha256:") {
		t.Fatalf("index output = %q", indexed)
	}

	status, err := runMemoryTool(t, tool, memoryToolArgs{Action: "index_status"})
	if err != nil || !strings.Contains(status, "Repository index active") || !strings.Contains(status, "1 indexed") {
		t.Fatalf("index_status = %q err=%v", status, err)
	}

	found, err := runMemoryTool(t, tool, memoryToolArgs{Action: "search_workspace", Query: "SentinelWorkspace", Limit: 2})
	if err != nil {
		t.Fatalf("search_workspace: %v", err)
	}
	for _, want := range []string{"UNTRUSTED REPOSITORY CONTENT", "service.go:1-", "repo-index://", "file-sha256:", "chunk-sha256:", "SentinelWorkspace"} {
		if !strings.Contains(found, want) {
			t.Fatalf("search output missing %q: %q", want, found)
		}
	}
	missing, err := runMemoryTool(t, tool, memoryToolArgs{Action: "search_workspace", Query: "OutsideWorkspaceSentinel"})
	if err != nil || !strings.Contains(missing, "No indexed workspace chunks matched") {
		t.Fatalf("out-of-workspace search = %q err=%v", missing, err)
	}

	events, err := tool.app.ListLedgerEvents(20)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
		if event.Kind == "repo_index_complete" && event.Output != "" {
			t.Fatalf("ledger copied source/output body: %#v", event)
		}
	}
	for _, want := range []string{"repo_index_start", "repo_index_complete", "repo_index_search"} {
		if !kinds[want] {
			t.Fatalf("ledger missing %s: %#v", want, events)
		}
	}
}

func TestMemoryToolRepositoryIndexRequiresQueryAndCompleteGeneration(t *testing.T) {
	tool, _ := newRepositoryMemoryToolTestApp(t)
	status, err := runMemoryTool(t, tool, memoryToolArgs{Action: "index_status"})
	if err != nil || !strings.Contains(status, "No complete repository index") {
		t.Fatalf("empty index status = %q err=%v", status, err)
	}
	if _, err := runMemoryTool(t, tool, memoryToolArgs{Action: "search_workspace"}); err == nil {
		t.Fatal("search_workspace without query should fail")
	}
}

func TestBoundedRepoExcerptUsesRuneLimit(t *testing.T) {
	got := boundedRepoExcerpt("alpha 世界 omega", 8)
	if got != "alpha 世界\n… [excerpt truncated]" {
		t.Fatalf("bounded excerpt = %q", got)
	}
}

func TestRepositoryIndexBindingsExposeCoverageAndOmissions(t *testing.T) {
	tool, root := newRepositoryMemoryToolTestApp(t)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Indexed workspace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "fixture", "ignored.js"), []byte("ignored dependency"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := tool.app.IndexWorkspaceRepository()
	if err != nil {
		t.Fatalf("IndexWorkspaceRepository: %v", err)
	}
	if !status.Available || !status.Active || !status.Complete || status.GenerationID == "" || status.ManifestDigest == "" {
		t.Fatalf("status not active and content-addressed: %#v", status)
	}
	if status.FilesSeen != 2 || status.FilesIndexed != 1 || status.OmissionCount != 2 || len(status.Omissions) != 2 {
		t.Fatalf("coverage/omissions = %#v", status)
	}
	statuses := map[string]bool{}
	for _, omission := range status.Omissions {
		statuses[omission.Status] = true
	}
	if !statuses["unsupported_binary"] || !statuses["excluded"] {
		t.Fatalf("explicit omission statuses = %#v", status.Omissions)
	}

	loaded, err := tool.app.GetRepositoryIndexStatus()
	if err != nil || loaded.GenerationID != status.GenerationID || loaded.OmissionCount != status.OmissionCount {
		t.Fatalf("GetRepositoryIndexStatus = %#v err=%v", loaded, err)
	}
}
