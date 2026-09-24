package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
)

func TestClearHistoryRejectsActiveRunWithoutChangingConversation(t *testing.T) {
	history := agent.NewHistory(4096)
	history.Append(llm.NewTextMessage(llm.RoleUser, "keep me"))
	app := &App{history: history, rollback: &agent.Rollback{}, agentRunning: true}

	err := app.ClearHistory()
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("ClearHistory error = %v, want active-run refusal", err)
	}
	if got := app.history.Messages(); len(got) != 1 {
		t.Fatalf("active-run clear changed history: %#v", got)
	}
}

func TestClearHistoryAdvancesConversationEpoch(t *testing.T) {
	history := agent.NewHistory(4096)
	history.Append(llm.NewTextMessage(llm.RoleUser, "retire me"))
	app := &App{history: history, rollback: &agent.Rollback{}, activeConversationName: "retire-me"}
	before := app.currentConversationEpoch()
	if err := app.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if after := app.currentConversationEpoch(); after <= before {
		t.Fatalf("conversation epoch did not advance: before=%d after=%d", before, after)
	}
	if app.activeConversationName != "" {
		t.Fatalf("cleared conversation retained active name %q", app.activeConversationName)
	}
}

func TestAutomaticConversationNamesAreUniqueAndAutosaveIntoLibrary(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
	}

	name, err := app.StartConversation("Review the repository next")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Review-the-repository-next" {
		t.Fatalf("automatic name = %q", name)
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "Review the repository next"))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, "Review complete"))
	app.autoSave()

	summaries, err := app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Name != name || summaries[0].MessageCount != 2 {
		t.Fatalf("automatic conversation summary = %#v", summaries)
	}
	if err := app.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	second, err := app.StartConversation("Review the repository next")
	if err != nil {
		t.Fatal(err)
	}
	if second != "Review-the-repository-next-2" {
		t.Fatalf("collision-safe automatic name = %q", second)
	}
}

func TestLegacyAutosaveRemainsHiddenFromConversationLibrary(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "recovery only"))
	app.autoSave()

	dir, err := sessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyAutosaveSessionName+".json")); err != nil {
		t.Fatalf("recovery autosave missing: %v", err)
	}
	summaries, err := app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("internal autosave leaked into library: %#v", summaries)
	}
	name, err := app.StartConversation("autosave")
	if err != nil {
		t.Fatal(err)
	}
	if name != "New-chat" {
		t.Fatalf("reserved recovery title became %q", name)
	}
	if err := app.SaveSession(legacyAutosaveSessionName); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("manual recovery-name save error = %v", err)
	}
}

func TestLoadSessionRejectsActiveRunBeforeReplacingConversation(t *testing.T) {
	history := agent.NewHistory(4096)
	history.Append(llm.NewTextMessage(llm.RoleUser, "keep me"))
	app := &App{history: history, rollback: &agent.Rollback{}, agentRunning: true}

	_, err := app.LoadSession("saved")
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("LoadSession error = %v, want active-run refusal", err)
	}
	if got := app.history.Messages(); len(got) != 1 {
		t.Fatalf("active-run load changed history: %#v", got)
	}
}

func TestRenameSessionMovesTranscriptAndRecallIndex(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	writeSavedSessionFixture(t, "old-title", []llm.Message{llm.NewTextMessage(llm.RoleUser, "rename lifecycle sentinel")})
	if err := sessionstore.StoreDefaultSession("old-title", workspaceScope(), "qwen3.6", []sessionstore.Message{{Role: "user", Content: "rename lifecycle sentinel"}}); err != nil {
		t.Fatal(err)
	}
	app := &App{history: agent.NewHistory(4096), rollback: &agent.Rollback{}, activeConversationName: "old-title"}
	if err := app.RenameSession("old-title", "new title"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	dir, err := sessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old-title.json")); !os.IsNotExist(err) {
		t.Fatalf("old transcript still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new-title.json")); err != nil {
		t.Fatalf("new transcript missing: %v", err)
	}
	names, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "new-title" {
		t.Fatalf("session names = %#v", names)
	}
	if app.activeConversationName != "new-title" {
		t.Fatalf("active conversation name = %q, want renamed identity", app.activeConversationName)
	}
	results, err := sessionstore.SearchDefault("lifecycle sentinel", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SessionName != "new-title" {
		t.Fatalf("recall results = %#v", results)
	}
}

func TestRenameSessionRejectsCollisionWithoutMovingFiles(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	oldPath := writeSavedSessionFixture(t, "one", []llm.Message{llm.NewTextMessage(llm.RoleUser, "one")})
	newPath := writeSavedSessionFixture(t, "two", []llm.Message{llm.NewTextMessage(llm.RoleUser, "two")})
	app := &App{history: agent.NewHistory(4096), rollback: &agent.Rollback{}}
	if err := app.RenameSession("one", "two"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("rename collision error = %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("collision removed source: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("collision removed destination: %v", err)
	}
}

func TestRenameSessionRejectsActiveRun(t *testing.T) {
	app := &App{history: agent.NewHistory(4096), rollback: &agent.Rollback{}, agentRunning: true}
	err := app.RenameSession("one", "two")
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("RenameSession error = %v, want active-run refusal", err)
	}
}

func TestListSessionSummariesNewestFirstAndFlagsMalformedTranscript(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	olderPath := writeSavedSessionFixture(t, "older", []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "first"),
		llm.NewTextMessage(llm.RoleAssistant, "second"),
	})
	dir, err := sessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	newerPath := filepath.Join(dir, "newer.json")
	if err := os.WriteFile(newerPath, []byte(`{"not":"a transcript"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(olderPath, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newerPath, now, now); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	summaries, err := app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %#v", summaries)
	}
	if summaries[0].Name != "newer" || summaries[0].Status != "needs-review" || summaries[0].MessageCount != 0 {
		t.Fatalf("newer summary = %#v", summaries[0])
	}
	if summaries[1].Name != "older" || summaries[1].Status != "saved" || summaries[1].MessageCount != 2 {
		t.Fatalf("older summary = %#v", summaries[1])
	}
	names, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "newer" || names[1] != "older" {
		t.Fatalf("names = %#v", names)
	}
}

func TestSessionTagsPersistAcrossRenameAndClearOnDelete(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	writeSavedSessionFixture(t, "tagged", []llm.Message{llm.NewTextMessage(llm.RoleUser, "tag me")})
	app := &App{history: agent.NewHistory(4096), rollback: &agent.Rollback{}}
	if err := app.SetSessionTags("tagged", []string{" Client A ", "urgent", "client a"}); err != nil {
		t.Fatalf("SetSessionTags: %v", err)
	}
	if err := app.SetConversationMode("tagged", conversationModeDirect); err != nil {
		t.Fatalf("SetConversationMode: %v", err)
	}
	summaries, err := app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || len(summaries[0].Tags) != 2 || summaries[0].Tags[0] != "Client A" || summaries[0].Tags[1] != "urgent" || summaries[0].ConversationMode != conversationModeDirect {
		t.Fatalf("tagged summary = %#v", summaries)
	}
	if err := app.RenameSession("tagged", "renamed"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	summaries, err = app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Name != "renamed" || len(summaries[0].Tags) != 2 || summaries[0].ConversationMode != conversationModeDirect {
		t.Fatalf("renamed tags = %#v", summaries)
	}
	if _, err := app.LoadSession("renamed"); err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if app.activeConversationName != "renamed" {
		t.Fatalf("loaded active conversation = %q", app.activeConversationName)
	}
	if got := app.GetConversationMode(); got != conversationModeDirect {
		t.Fatalf("loaded conversation mode = %q, want direct", got)
	}
	if err := app.DeleteSession("renamed"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if app.activeConversationName != "" {
		t.Fatalf("deleted conversation retained active identity %q", app.activeConversationName)
	}
	metadata, err := loadSessionLibraryMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Tags) != 0 {
		t.Fatalf("deleted session tags remain: %#v", metadata.Tags)
	}
	if len(metadata.Modes) != 0 {
		t.Fatalf("deleted session modes remain: %#v", metadata.Modes)
	}
}

func TestSetSessionTagsRejectsUnsafeBounds(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	writeSavedSessionFixture(t, "tagged", []llm.Message{llm.NewTextMessage(llm.RoleUser, "tag me")})
	app := &App{}
	if err := app.SetSessionTags("tagged", []string{"one", "two", "three", "four", "five", "six", "seven"}); err == nil {
		t.Fatal("expected tag-count limit")
	}
	if err := app.SetSessionTags("tagged", []string{strings.Repeat("x", 25)}); err == nil {
		t.Fatal("expected tag-length limit")
	}
}

func TestSetSavedConversationModeDoesNotChangeActiveConversation(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	writeSavedSessionFixture(t, "active", []llm.Message{llm.NewTextMessage(llm.RoleUser, "active")})
	writeSavedSessionFixture(t, "background", []llm.Message{llm.NewTextMessage(llm.RoleUser, "background")})
	app := &App{conversationMode: conversationModeDirect}

	if err := app.SetSavedConversationMode("background", conversationModeAgent); err != nil {
		t.Fatalf("SetSavedConversationMode: %v", err)
	}
	if got := app.GetConversationMode(); got != conversationModeDirect {
		t.Fatalf("active conversation mode = %q, want %q", got, conversationModeDirect)
	}
	summaries, err := app.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, summary := range summaries {
		if summary.Name == "background" && summary.ConversationMode != conversationModeAgent {
			t.Fatalf("background conversation mode = %q, want %q", summary.ConversationMode, conversationModeAgent)
		}
		if summary.Name == "background" {
			found = true
		}
	}
	if !found {
		t.Fatalf("background conversation missing from summaries: %#v", summaries)
	}
}

func TestClearHistoryRestoresConfiguredConversationDefault(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.DefaultConversationMode = conversationModeAgent
	app := &App{
		cfg:              &cfg,
		history:          agent.NewHistory(4096),
		rollback:         &agent.Rollback{},
		conversationMode: conversationModeDirect,
	}
	if err := app.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := app.GetConversationMode(); got != conversationModeAgent {
		t.Fatalf("cleared conversation mode = %q, want configured agent default", got)
	}
}

func TestListSessionSummariesSurvivesDamagedOptionalTagMetadata(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", configDir)
	writeSavedSessionFixture(t, "still-visible", []llm.Message{llm.NewTextMessage(llm.RoleUser, "keep visible")})
	if err := os.WriteFile(filepath.Join(configDir, "session-library.json"), []byte(`{"broken"`), 0o640); err != nil {
		t.Fatal(err)
	}
	summaries, err := (&App{}).ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Name != "still-visible" {
		t.Fatalf("damaged optional metadata hid transcripts: %#v", summaries)
	}
	if err := (&App{}).DeleteSession("still-visible"); err != nil {
		t.Fatalf("damaged optional metadata blocked transcript deletion: %v", err)
	}
}
