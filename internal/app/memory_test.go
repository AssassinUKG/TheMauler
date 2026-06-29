package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
	"mauler/internal/store"
)

func TestContainsWordishRespectsBoundaries(t *testing.T) {
	cases := []struct {
		haystack, term string
		want           bool
	}{
		{"category management", "cat", false}, // substring trap must not match
		{"the cat sat", "cat", true},
		{"connected.htb is up", "connected", true}, // "." is a boundary
		{"connected.htb is up", "connected.htb", true},
		{"reconnaissance", "recon", false},
		{"run nmap now", "nmap", true},
		{"", "cat", false},
		{"cat", "", false},
	}
	for _, c := range cases {
		if got := containsWordish(c.haystack, c.term); got != c.want {
			t.Errorf("containsWordish(%q,%q) = %v, want %v", c.haystack, c.term, got, c.want)
		}
	}
}

func TestUsageBoostInfluencesScore(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	old := time.Now().Add(-90 * 24 * time.Hour).Format(time.RFC3339)
	terms := map[string]bool{"nmap": true}
	used := MemoryEntry{Title: "nmap tip", Content: "nmap scan", UpdatedAt: old, LastUsedAt: now}
	unused := MemoryEntry{Title: "nmap tip", Content: "nmap scan", UpdatedAt: old, LastUsedAt: old}
	if scoreMemory(used, terms, "nmap") <= scoreMemory(unused, terms, "nmap") {
		t.Fatal("recently-used entry should outrank an equivalent never-used one")
	}
}

func TestEvictionScoreKeepsPinnedAndUsed(t *testing.T) {
	old := time.Now().Add(-90 * 24 * time.Hour).Format(time.RFC3339)
	now := time.Now().Format(time.RFC3339)

	pinnedOld := MemoryEntry{Pinned: true, Importance: 1, UpdatedAt: old, LastUsedAt: old}
	freshUnpinned := MemoryEntry{Importance: 5, UpdatedAt: now, LastUsedAt: now}
	if evictionScore(pinnedOld) <= evictionScore(freshUnpinned) {
		t.Fatal("pinned entry should survive eviction over an unpinned recent one")
	}

	usedOld := MemoryEntry{Importance: 3, UpdatedAt: old, LastUsedAt: now}
	staleRecent := MemoryEntry{Importance: 3, UpdatedAt: now, LastUsedAt: old}
	// Same importance: recently-used (even if older by UpdatedAt) should not be
	// strictly worse than a recently-updated-but-never-used entry.
	if evictionScore(usedOld) < evictionScore(staleRecent)-0.5 {
		t.Fatalf("usage signal ignored: used=%v stale=%v", evictionScore(usedOld), evictionScore(staleRecent))
	}
}

func TestMemoryConfidenceAndSourceDefaults(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg}
	saved, err := app.SaveMemoryEntry(MemoryEntry{
		Title:      "Default confidence",
		Content:    "This is a curated note.",
		Kind:       "fact",
		Importance: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Confidence != "confirmed" || saved.Source != "user" {
		t.Fatalf("unexpected defaults: %#v", saved)
	}
}

func TestMemoryMigratesLegacyJSONIntoSQLite(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	legacy := []MemoryEntry{{
		ID:         "mem-legacy",
		Scope:      workspaceScope(),
		Title:      "Legacy note",
		Content:    "Keep this old project memory.",
		Tags:       []string{"legacy", "ops"},
		Kind:       "fact",
		Confidence: "confirmed",
		Source:     "user",
		Importance: 4,
		CreatedAt:  "2026-06-15T10:00:00Z",
		UpdatedAt:  "2026-06-15T10:00:00Z",
	}}
	if err := saveMemoryJSON(legacy); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	setMemoryDB(db)
	t.Cleanup(func() { setMemoryDB(nil) })

	imported, err := migrateMemoryJSONToDB(db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}
	entries, err := loadMemory()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Legacy note" || !hasTag(entries[0], "legacy") {
		t.Fatalf("unexpected migrated memory: %#v", entries)
	}
}

func TestMemoryJSONExportImportRoundTrip(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(context.Background()) })
	if _, err := app.AddMemory("Shell preference", "Use WSL for pentest work.", []string{"ops", "wsl"}); err != nil {
		t.Fatalf("add memory: %v", err)
	}
	raw, err := app.ExportMemoryJSON()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var exported []MemoryEntry
	if err := json.Unmarshal([]byte(raw), &exported); err != nil {
		t.Fatalf("exported json invalid: %v", err)
	}
	if len(exported) != 1 || exported[0].Title != "Shell preference" {
		t.Fatalf("unexpected exported memory: %#v", exported)
	}
	if err := app.ClearMemoryEntries(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	imported, err := app.ImportMemoryJSON(raw)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}
	entries, err := app.ListMemory()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "Use WSL for pentest work." {
		t.Fatalf("unexpected imported memory: %#v", entries)
	}
}

func TestMemoryPromptPrefixLabelsUnverifiedEntries(t *testing.T) {
	cases := []struct {
		confidence string
		want       string
	}{
		{"hypothesis", "UNVERIFIED hypothesis"},
		{"likely", "Likely but unverified"},
		{"stale", "STALE memory"},
		{"confirmed", ""},
	}
	for _, tc := range cases {
		got := memoryPromptPrefix(MemoryEntry{Confidence: tc.confidence})
		if tc.want == "" && got != "" {
			t.Fatalf("confidence %q got unexpected prefix %q", tc.confidence, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Fatalf("confidence %q prefix %q missing %q", tc.confidence, got, tc.want)
		}
	}
}

func TestSelectRelevantMemoryWithholdsConflictingTarget(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.Lab.Target = "10.129.15.218"
	app := &App{cfg: &cfg}
	if _, err := app.SaveMemoryEntry(MemoryEntry{
		Title:      "Old FreePBX run",
		Content:    "Target: 10.129.12.172. FreePBX exploit path was ajax.php.",
		Tags:       []string{"target-10-129-12-172", "htb", "run"},
		Kind:       "fact",
		Confidence: "confirmed",
		Source:     "previous_run",
		Importance: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveMemoryEntry(MemoryEntry{
		Title:      "General FreePBX lesson",
		Content:    "For FreePBX research, verify current CVE dates before choosing an exploit.",
		Tags:       []string{"freepbx", "research"},
		Kind:       "constraint",
		Confidence: "confirmed",
		Source:     "user",
		Importance: 5,
	}); err != nil {
		t.Fatal(err)
	}

	selection := selectRelevantMemory(cfg, "Research FreePBX on 10.129.15.218")
	if len(selection.Withheld) != 1 || selection.Withheld[0].Title != "Old FreePBX run" {
		t.Fatalf("expected old target memory withheld, got entries=%#v withheld=%#v conflicts=%#v", selection.Entries, selection.Withheld, selection.Conflicts)
	}
	for _, entry := range selection.Entries {
		if entry.Title == "Old FreePBX run" {
			t.Fatalf("conflicting target memory was injected: %#v", selection)
		}
	}
	if len(selection.Entries) == 0 || selection.Entries[0].Title != "General FreePBX lesson" {
		t.Fatalf("expected general memory to remain injectable, got %#v", selection.Entries)
	}
}

func TestPlanMemoryRetrievalKeepsLayerSlots(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Format(time.RFC3339)
	entries := []MemoryEntry{
		{ID: "pref", Scope: workspaceScope(), Title: "Shell preference", Content: "Use WSL for nmap scans.", Kind: "preference", Confidence: "confirmed", Source: "user", Importance: 3, UpdatedAt: now},
		{ID: "fact", Scope: workspaceScope(), Title: "Nmap fact", Content: "nmap service detection uses -sV.", Kind: "fact", Confidence: "confirmed", Source: "user", Importance: 3, UpdatedAt: now},
		{ID: "run", Scope: workspaceScope(), Title: "Prior nmap run", Content: "Previous run saved nmap output under scans.", Kind: "note", Confidence: "confirmed", Source: "previous_run", Importance: 3, UpdatedAt: now},
		{ID: "hyp", Scope: workspaceScope(), Title: "Likely nmap issue", Content: "Likely UDP scans need more time.", Kind: "note", Confidence: "likely", Source: "agent", Importance: 3, UpdatedAt: now},
	}

	plan := planMemoryRetrieval(entries, "resume the prior nmap target work", 4)
	if plan.Intent != "recall" {
		t.Fatalf("intent = %q, want recall", plan.Intent)
	}
	if len(plan.Selected) != 4 {
		t.Fatalf("selected = %d, want 4: %#v", len(plan.Selected), plan.Selected)
	}
	seen := map[string]bool{}
	for _, selected := range plan.Selected {
		seen[selected.Layer] = true
		if selected.Reason == "" {
			t.Fatalf("selection missing reason: %#v", selected)
		}
	}
	for _, layer := range []string{"preferences", "confirmed", "previous_run", "unverified"} {
		if !seen[layer] {
			t.Fatalf("planner did not preserve %s slot: %#v", layer, plan.Selected)
		}
	}
}

func TestSelectRelevantMemoryReturnsRetrievalPlan(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg}
	if _, err := app.SaveMemoryEntry(MemoryEntry{
		Title:      "Provider fact",
		Content:    "LM Studio provider URL should include /v1.",
		Kind:       "fact",
		Confidence: "confirmed",
		Source:     "user",
		Importance: 4,
	}); err != nil {
		t.Fatal(err)
	}

	selection := selectRelevantMemory(cfg, "fix the LM Studio provider URL")
	if len(selection.Entries) != 1 {
		t.Fatalf("expected one injected memory, got %#v", selection.Entries)
	}
	if selection.Plan.Intent != "code" || len(selection.Plan.Selected) != 1 {
		t.Fatalf("unexpected retrieval plan: %#v", selection.Plan)
	}
	detail := memoryRetrievalPlanDetail(selection.Plan)
	if !strings.Contains(detail, "intent=code") || !strings.Contains(detail, "Provider fact") {
		t.Fatalf("plan detail missing expected content: %s", detail)
	}
}

func TestSessionRecallPointerEntriesAreCompact(t *testing.T) {
	results := []sessionstore.SearchResult{{
		SessionID:   "scope::debug",
		SessionName: "debug",
		MessageID:   42,
		Role:        "assistant",
		Content:     strings.Repeat("build failed because provider URL missed /v1 ", 20),
		UpdatedAt:   "2026-06-20T10:00:00Z",
	}}

	entries := sessionRecallMemoryEntries(results)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if memoryLayer(entries[0]) != "session_recall" {
		t.Fatalf("entry layer = %q, want session_recall", memoryLayer(entries[0]))
	}
	if len(entries[0].Content) > 420 || !strings.Contains(entries[0].Content, "snippet=") {
		t.Fatalf("session recall should be compact pointer content: %q", entries[0].Content)
	}
}

func TestEvidencePointerEntriesUseArtifactsNotRawOutput(t *testing.T) {
	events := []ledger.Event{{
		ID:        "evt-1",
		Kind:      "tool_result",
		Tool:      "http_probe",
		Status:    "done",
		Message:   "HTTP probe found FreePBX login",
		Output:    strings.Repeat("raw http body ", 80),
		Artifacts: []string{".mauler_artifacts/http_probe/freepbx.txt"},
		Timestamp: "2026-06-20T10:00:00Z",
	}}

	entries := evidenceMemoryEntries(events, []string{"freepbx"})
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if memoryLayer(entries[0]) != "evidence" {
		t.Fatalf("entry layer = %q, want evidence", memoryLayer(entries[0]))
	}
	if !strings.Contains(entries[0].Content, ".mauler_artifacts/http_probe/freepbx.txt") {
		t.Fatalf("evidence pointer missing artifact path: %q", entries[0].Content)
	}
	if strings.Count(entries[0].Content, "raw http body") > 2 {
		t.Fatalf("evidence pointer leaked too much raw output: %q", entries[0].Content)
	}
}

func TestBuildSystemPromptSeparatesSessionAndEvidencePointers(t *testing.T) {
	cfg := settings.DefaultSettings()
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Auto"}, []MemoryEntry{
		{ID: "session:one", Title: "Session recall: debug", Content: "Prior saved chat match; snippet=provider URL missed /v1", Tags: []string{"session-recall"}, Confidence: "likely", Source: "previous_run"},
		{ID: "evidence:one", Title: "Evidence pointer: HTTP probe", Content: "Ledger evidence; artifacts=.mauler_artifacts/http_probe/out.txt", Tags: []string{"evidence"}, Kind: "fact", Confidence: "confirmed", Source: "tool"},
	}, nil)
	for _, want := range []string{
		"Relevant prior-session pointers - compact recall only",
		"Relevant evidence pointers - inspect artifacts/files before relying on details",
		"Session recall: debug",
		".mauler_artifacts/http_probe/out.txt",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
