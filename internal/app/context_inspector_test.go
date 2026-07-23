package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestContextInspectorPreviewsCoreRelevantAndExpandedWithoutPromptContent(t *testing.T) {
	root := writeContextInspectorFixture(t)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.ActiveProfile = "test"
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.Context.ProjectDocMaxBytes = 128 * 1024
	profiles := settings.ProfilesFile{Profiles: map[string]settings.Profile{
		"test": {
			Name:      "test",
			ModelID:   "local/test",
			CtxTokens: 32768,
			NoThink:   settings.GenerationParams{MaxTokens: 4096},
		},
	}}
	app := &App{
		cfg:        &cfg,
		profiles:   &profiles,
		history:    agent.NewHistory(32768),
		registry:   tools.New(),
		autoAgents: true,
	}

	core, err := app.PreviewContext("fix the provider implementation", "core")
	if err != nil {
		t.Fatal(err)
	}
	relevant, err := app.PreviewContext("fix the provider implementation", "relevant")
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := app.PreviewContext("audit the complete repository architecture", "expanded")
	if err != nil {
		t.Fatal(err)
	}
	if core.EffectiveClass != "core" || len(core.Sources) != 1 || core.RouteID != "explicit-core" {
		t.Fatalf("unexpected core inspection: %#v", core)
	}
	if relevant.EffectiveClass != "relevant" || relevant.RouteID != "provider-and-models" || len(relevant.Sources) != 2 {
		t.Fatalf("unexpected relevant inspection: %#v", relevant)
	}
	if expanded.EffectiveClass != "expanded" || len(expanded.Sources) != 3 || expanded.PacketLimitTokens != 8000 {
		t.Fatalf("unexpected expanded inspection: %#v", expanded)
	}
	if expanded.ManifestStatus != contextManifestStatusActive || expanded.ManifestSHA256 == "" || expanded.ManifestPath == "" {
		t.Fatalf("manifest provenance missing: %#v", expanded)
	}
	if expanded.WorkingContextTokens != 24576 || expanded.OutputReserveTokens != 8192 || expanded.ModelMaxOutputTokens != 4096 {
		t.Fatalf("working/output budgets are wrong: %#v", expanded)
	}
	if expanded.Budget.CoreSystemTokens <= 0 || expanded.Budget.ProjectDocumentTokens <= 0 || expanded.Budget.ToolSchemaTokens <= 0 || expanded.Budget.TotalPreflightTokens <= 0 {
		t.Fatalf("context budget breakdown is incomplete: %#v", expanded.Budget)
	}
	if expanded.Budget.TotalPreflightTokens >= expanded.WorkingContextTokens || expanded.Budget.RemainingWorkingTokens <= 0 {
		t.Fatalf("preflight budget is unusable: %#v", expanded.Budget)
	}
	for _, source := range expanded.Sources {
		if len(source.SHA256) != 64 || source.SourceBytes <= 0 || source.PromptBytes <= 0 || len(source.ExcerptRanges) == 0 {
			t.Fatalf("source provenance incomplete: %#v", source)
		}
		if strings.Contains(source.Reason, "SAFE_INSPECTOR_SECRET") {
			t.Fatalf("source content leaked into metadata: %#v", source)
		}
	}
	foundArchive := false
	for _, excluded := range expanded.ExcludedSources {
		if strings.Contains(excluded.DisplayPath, "agents-handoff") {
			foundArchive = excluded.Large && strings.Contains(excluded.Reason, "never automatically injected")
		}
	}
	if !foundArchive {
		t.Fatalf("large archive exclusion was not explained: %#v", expanded.ExcludedSources)
	}
}

func TestContextPacketPinIsEphemeralAndValidated(t *testing.T) {
	app := &App{}
	if pinned, err := app.SetNextContextPacketClass("expanded"); err != nil || pinned != "expanded" {
		t.Fatalf("pin expanded = %q, %v", pinned, err)
	}
	if got := app.GetNextContextPacketClass(); got != "expanded" {
		t.Fatalf("pinned class = %q", got)
	}
	if consumed := app.consumeNextContextPacketClass("telegram"); consumed != "" {
		t.Fatalf("remote lane consumed desktop pin: %q", consumed)
	}
	if got := app.GetNextContextPacketClass(); got != "expanded" {
		t.Fatalf("remote lane cleared desktop pin: %q", got)
	}
	if consumed := app.consumeNextContextPacketClass("desktop"); consumed != "expanded" {
		t.Fatalf("desktop task consumed %q", consumed)
	}
	if got := app.GetNextContextPacketClass(); got != "" {
		t.Fatalf("one-task pin was not consumed: %q", got)
	}
	if _, err := app.SetNextContextPacketClass("expanded"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetNextContextPacketClass("unsafe"); err == nil {
		t.Fatal("invalid context class should be rejected")
	}
	if got := app.GetNextContextPacketClass(); got != "expanded" {
		t.Fatalf("invalid request changed pin to %q", got)
	}
	if pinned, err := app.SetNextContextPacketClass("auto"); err != nil || pinned != "" {
		t.Fatalf("unpin = %q, %v", pinned, err)
	}
	if got := app.GetNextContextPacketClass(); got != "" {
		t.Fatalf("class remained pinned: %q", got)
	}
}

func TestMessagesWithPrimarySystemPromptRefreshesWithoutDuplicating(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "You are TheMauler, old packet"),
		llm.NewTextMessage(llm.RoleUser, "first task"),
		llm.NewTextMessage(llm.RoleSystem, "authoritative control packet"),
	}
	updated := messagesWithPrimarySystemPrompt(messages, "You are TheMauler, refreshed exact packet")
	if len(updated) != len(messages) {
		t.Fatalf("system refresh duplicated history: %d -> %d", len(messages), len(updated))
	}
	if got := messageText(updated[0]); got != "You are TheMauler, refreshed exact packet" {
		t.Fatalf("primary system prompt was not refreshed: %q", got)
	}
	if got := messageText(updated[2]); got != "authoritative control packet" {
		t.Fatalf("control packet was changed: %q", got)
	}
	inserted := messagesWithPrimarySystemPrompt([]llm.Message{llm.NewTextMessage(llm.RoleUser, "saved chat")}, "You are TheMauler, restored")
	if len(inserted) != 2 || inserted[0].Role != llm.RoleSystem {
		t.Fatalf("missing primary prompt was not restored: %#v", inserted)
	}
}

func writeContextInspectorFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module test\n")
	mustWrite(t, filepath.Join(root, "AGENTS.md"), "# Core\nSAFE_INSPECTOR_RULE\n")
	mustWrite(t, filepath.Join(root, "MAULER.md"), "# compatibility shim\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "architecture.md"), "# Architecture\nBackend and frontend boundaries.\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "feature-catalog.md"), "# Features\nContext inspector.\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "provider.md"), "# Provider\nOpenRouter resets after one task.\n")
	mustWrite(t, filepath.Join(root, "docs", "context", "not-selected.md"), "# Other\nNot selected.\n")
	mustWrite(t, filepath.Join(root, "docs", "archive", "agents-handoff-snapshot.md"), strings.Repeat("archived\n", 1200))
	mustWrite(t, filepath.Join(root, "docs", "context", "manifest.json"), `{
  "version": 1,
  "status": "active",
  "default_packet": "relevant_workspace",
  "packets": {
    "minimal_external_research": {"max_project_tokens": 500, "documents": []},
    "relevant_workspace": {"max_project_tokens": 1200, "documents": ["../../AGENTS.md"]},
    "expanded_workspace": {"max_project_tokens": 8000, "explicit_only": true, "documents": ["../../AGENTS.md", "architecture.md", "feature-catalog.md"]}
  },
  "routes": [
    {
      "id": "provider-and-models",
      "priority": 20,
      "signals": ["provider", "OpenRouter"],
      "packet": "relevant_workspace",
      "documents": ["../../AGENTS.md", "provider.md"]
    }
  ]
}`)
	return root
}
