package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func restoreWorkingDir(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})
}

func TestBuildChatRequestUsesAllActiveProfileGenerationSettings(t *testing.T) {
	profile := settings.Profile{
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral: settings.GenerationParams{
			Temperature:     0.61,
			TopP:            0.91,
			TopK:            33,
			MinP:            0.07,
			PresencePenalty: 1.25,
			MaxTokens:       7777,
			Seed:            12345,
		},
		NoThink: settings.GenerationParams{
			Temperature: 0.11,
			MaxTokens:   222,
			Seed:        1,
		},
	}
	tools := []llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:       "read_file",
			Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}}
	msgs := []llm.Message{llm.NewTextMessage(llm.RoleUser, "hello")}

	req := buildChatRequest(profile, msgs, tools, "", false)

	if req.MaxTokens != 7777 {
		t.Fatalf("MaxTokens = %d, want 7777", req.MaxTokens)
	}
	if req.Temperature != 0.61 || req.TopP != 0.91 || req.TopK != 33 || req.MinP != 0.07 || req.PresencePenalty != 1.25 {
		t.Fatalf("sampling params not copied correctly: %#v", req)
	}
	if req.Seed != 12345 {
		t.Fatalf("Seed = %d, want 12345", req.Seed)
	}
	if !req.EnableThinking || !req.PreserveThinking {
		t.Fatalf("thinking flags not copied: %#v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != "hello" {
		t.Fatalf("messages not copied: %#v", req.Messages)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
		t.Fatalf("tools not copied: %#v", req.Tools)
	}
}

func TestBuildChatRequestUsesNoThinkSettingsWhenThinkingDisabled(t *testing.T) {
	profile := settings.Profile{
		Thinking: false,
		ThinkGeneral: settings.GenerationParams{
			Temperature: 1.0,
			MaxTokens:   8192,
			Seed:        99,
		},
		NoThink: settings.GenerationParams{
			Temperature:     0.44,
			TopP:            0.8,
			TopK:            20,
			MinP:            0.02,
			PresencePenalty: 0.5,
			MaxTokens:       2048,
			Seed:            88,
		},
	}

	req := buildChatRequest(profile, nil, nil, "", false)

	if req.MaxTokens != 2048 || req.Temperature != 0.44 || req.Seed != 88 {
		t.Fatalf("nothinking params not selected: %#v", req)
	}
	if req.EnableThinking {
		t.Fatalf("EnableThinking = true, want false")
	}
}

func TestEnsureModelLoadedOnlyLoadsOncePerModelKey(t *testing.T) {
	app := &App{}
	client := &countingLoader{}
	profile := settings.Profile{
		Backend:   "lmstudio",
		BaseURL:   "http://localhost:1234/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want 1", client.loads)
	}

	profile.CtxTokens = 4096
	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 2 {
		t.Fatalf("loads after context change = %d, want 2", client.loads)
	}
}

func TestEnsureModelLoadedPreventsDoubleLoadUnderConcurrentCalls(t *testing.T) {
	app := &App{}
	client := &countingLoader{}
	profile := settings.Profile{
		Backend:   "lmstudio",
		BaseURL:   "http://localhost:1234/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}

	const goroutines = 10
	errs := make(chan error, goroutines)
	start := make(chan struct{})

	for range goroutines {
		go func() {
			<-start
			errs <- app.ensureModelLoaded(context.Background(), client, profile)
		}()
	}
	close(start)

	for range goroutines {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	if client.loads != 1 {
		t.Fatalf("concurrent calls caused %d loads, want exactly 1", client.loads)
	}
}

func TestEnsureModelLoadedReloadsWhenBackendContextExceedsProfile(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 153088}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want reload when backend context is too large", client.loads)
	}
}

func TestEnsureModelLoadedReloadsWhenBackendContextBelowProfile(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "gemma",
		CtxTokens: 120000,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 32768}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want reload when backend context is too small", client.loads)
	}
}

func TestEnsureModelLoadedKeepsCacheWhenBackendContextMatchesProfile(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 32768}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 0 {
		t.Fatalf("loads = %d, want cached model to be reused", client.loads)
	}
}

func TestShouldConfirmToolSeparatesShellExecFromWrites(t *testing.T) {
	cfg := &settings.Settings{}
	cfg.Tools.ConfirmExec = true
	cfg.Tools.ConfirmWrites = false

	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"go test ./..."}`)}}
	if !shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("shell should respect confirm_exec")
	}
	tc.Function.Name = "write_file"
	if shouldConfirmTool(&namedDestructiveTool{name: "write_file"}, cfg, tc) {
		t.Fatalf("write_file should not confirm when confirm_writes is false")
	}
}

func TestShouldConfirmToolSkipsSafeListedExactInput(t *testing.T) {
	input := `{"command":"go test ./..."}`
	cfg := &settings.Settings{}
	cfg.Tools.ConfirmExec = true
	cfg.Tools.SafeRules = []settings.ToolSafeRule{{
		Tool:      "shell",
		InputHash: safeToolInputHash(input),
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}

	if shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("safe-listed exact tool input should not prompt")
	}

	tc.Function.Arguments = json.RawMessage(`{"command":"go vet ./..."}`)
	if !shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("different tool input should still prompt")
	}
}

func TestComposeUserTextWithAttachments(t *testing.T) {
	got := composeUserTextWithAttachments("summarise this", []ChatAttachment{{
		Name:      "Pasted text.txt",
		Kind:      "document",
		MIME:      "text/plain",
		Content:   "b1 - response - 2",
		Truncated: true,
	}})

	for _, want := range []string{"summarise this", "Attached context from the user", "Pasted text.txt", "inline chat attachment", "do not call read_file", "b1 - response - 2", "attachment truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("composed attachment text missing %q:\n%s", want, got)
		}
	}
}

func TestSetWorkingDirAppliesWorkspaceAndClearsRunContext(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "idea.md"), []byte("image scrubber"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "old.txt")
	if err := os.WriteFile(snapshot, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	rb := &agent.Rollback{}
	if err := rb.Push(agent.OpWrite, snapshot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: rb,
		registry: tools.New(),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleSystem, "old workspace"))
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "what is this repo?"))

	if err := app.SetWorkingDir(project); err != nil {
		t.Fatal(err)
	}

	if got, want := app.GetWorkingDir(), filepath.ToSlash(project); got != want {
		t.Fatalf("working dir = %q, want %q", got, want)
	}
	if app.history.TokenCount() != 0 {
		t.Fatalf("history token count = %d, want cleared", app.history.TokenCount())
	}
	if app.rollback.Len() != 0 {
		t.Fatalf("rollback len = %d, want cleared", app.rollback.Len())
	}
	saved, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Context.WorkspaceDir != filepath.ToSlash(project) {
		t.Fatalf("saved workspace = %q, want %q", saved.Context.WorkspaceDir, filepath.ToSlash(project))
	}
}

func TestUpdateSettingsAppliesWorkspaceDir(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
		registry: tools.New(),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "stale chat"))

	next := cfg
	next.Context.WorkspaceDir = project
	if err := app.UpdateSettings(next); err != nil {
		t.Fatal(err)
	}

	if got, want := app.GetWorkingDir(), filepath.ToSlash(project); got != want {
		t.Fatalf("working dir = %q, want %q", got, want)
	}
	if app.cfg.Context.WorkspaceDir != filepath.ToSlash(project) {
		t.Fatalf("app cfg workspace = %q, want %q", app.cfg.Context.WorkspaceDir, filepath.ToSlash(project))
	}
	if app.history.TokenCount() != 0 {
		t.Fatalf("history token count = %d, want cleared", app.history.TokenCount())
	}
}

func TestBuildSystemPromptIncludesAuthoritativeWorkspace(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "idea.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPrompt(settings.DefaultSettings(), AgentMode{}, nil, nil)
	for _, want := range []string{
		"Current workspace context (authoritative for this run)",
		filepath.ToSlash(project),
		"idea.md",
		"app.py",
		"ignore stale project names",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestStopReasonForBudgetBlock(t *testing.T) {
	tests := map[string]string{
		"web research stopped after 2 failed/no-result web attempts": "web_research_failed",
		"web_search budget exhausted (4 searches)":                   "search_budget_exhausted",
		"fetch_url budget exhausted (6 fetches)":                     "fetch_budget_exhausted",
		"browser automation budget exhausted (20 actions)":           "browser_budget_exhausted",
	}
	for msg, want := range tests {
		if got := stopReasonForBudgetBlock("web_search", msg); got != want {
			t.Fatalf("stopReasonForBudgetBlock(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestTaskRunKeepsFirstStopReason(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.stop("tool_denied", "denied")
	run.stop("tool_error", "later")
	run.finish("done", "summary")

	if run.StopReason != "tool_denied" || run.StopDetail != "denied" {
		t.Fatalf("unexpected stop fields: %#v", run)
	}
}

func TestTaskRunEventsCaptureTimeline(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.addEvent("continue", "Auto-continue 1/4", strings.Repeat("x", 2500))

	if len(run.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(run.Events))
	}
	if run.Events[0].Kind != "continue" || run.Events[0].Message != "Auto-continue 1/4" {
		t.Fatalf("unexpected event: %#v", run.Events[0])
	}
	if len(run.Events[0].Detail) > 2005 || !strings.HasSuffix(run.Events[0].Detail, "\n...") {
		t.Fatalf("event detail was not trimmed: len=%d suffix=%q", len(run.Events[0].Detail), run.Events[0].Detail[len(run.Events[0].Detail)-4:])
	}
}

func TestTaskRunStateTransitionsAreLogged(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.setState("reading", "read_file")
	run.setState("reading", "read_file again")
	run.setState("editing", "edit_file")

	if run.State != "editing" {
		t.Fatalf("state = %q, want editing", run.State)
	}
	stateEvents := 0
	for _, event := range run.Events {
		if event.Kind == "state" {
			stateEvents++
		}
	}
	if stateEvents != 2 {
		t.Fatalf("duplicate state should not log extra events, got %d state events: %#v", stateEvents, run.Events)
	}
}

func TestToolChoiceDisablesToolsForSmallTalkButNotShortTasks(t *testing.T) {
	for _, text := range []string{"hello", "hi there", "thanks", "good morning"} {
		if !looksConversational(text) {
			t.Fatalf("%q should be conversational", text)
		}
		if got := toolChoiceFor(text, 0, 0); got != "none" {
			t.Fatalf("toolChoiceFor(%q) = %q, want none", text, got)
		}
	}

	for _, text := range []string{"fix bug", "run tests", "read README.md", "list files", "search for config"} {
		if looksConversational(text) {
			t.Fatalf("%q should be treated as a tool-capable task", text)
		}
		if got := toolChoiceFor(text, 0, 0); got != "auto" {
			t.Fatalf("toolChoiceFor(%q) = %q, want auto", text, got)
		}
	}
}

func TestToolDefsOmittedForConversationalTurn(t *testing.T) {
	registry := tools.New()
	defs, choice := toolDefsAndChoiceForTurn(registry, settings.DefaultSettings().Tools, "hello", 0, 0)
	if choice != "none" {
		t.Fatalf("choice = %q, want none", choice)
	}
	if len(defs) != 0 {
		t.Fatalf("conversational turn should not expose tool schemas, got %d", len(defs))
	}

	defs, choice = toolDefsAndChoiceForTurn(registry, settings.DefaultSettings().Tools, "fix bug", 0, 0)
	if choice != "auto" || len(defs) == 0 {
		t.Fatalf("task turn should expose tools, choice=%q defs=%d", choice, len(defs))
	}
}

func TestToolErrorResultPreservesCapturedOutput(t *testing.T) {
	got := toolErrorResult("[stderr]\nInvoke-WebRequest failed\n[powershell exit 1, 10ms]", errors.New("exit code 1"))
	if !strings.Contains(got, "Invoke-WebRequest failed") || !strings.Contains(got, "error: exit code 1") {
		t.Fatalf("tool error result lost captured output: %q", got)
	}
	if got := toolErrorResult("", errors.New("exit code 1")); got != "error: exit code 1" {
		t.Fatalf("empty output fallback = %q", got)
	}
}

func TestStateForTool(t *testing.T) {
	tests := map[string]string{
		"web_search":     "researching",
		"browser_open":   "researching",
		"read_file":      "reading",
		"read_pdf":       "reading",
		"session_search": "reading",
		"edit_file":      "editing",
		"shell":          "testing",
		"unknown_tool":   "using_tools",
	}
	for tool, want := range tests {
		if got := stateForTool(tool); got != want {
			t.Fatalf("stateForTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestMalformedToolArgsErrorDetection(t *testing.T) {
	err := errors.New("write_file: bad params: unexpected end of JSON input")
	if !isMalformedToolArgsError(err) {
		t.Fatalf("expected malformed JSON tool error to be detected")
	}
	if isMalformedToolArgsError(errors.New("write_file: permission denied")) {
		t.Fatalf("permission denied should not be classified as malformed JSON")
	}
}

func TestSaveTaskRunHonoursDisabledLogging(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	if err := saveTaskRun(run, &settings.LoggingConfig{Enabled: false}); err != nil {
		t.Fatalf("disabled logging should be a no-op: %v", err)
	}
}

func TestAutoContinueDelay(t *testing.T) {
	if autoContinueDelay(0) != 0 {
		t.Fatalf("attempt 0 should not delay")
	}
	if autoContinueDelay(1) != 500*time.Millisecond {
		t.Fatalf("attempt 1 delay = %s, want 500ms", autoContinueDelay(1))
	}
}

func TestLooksAboutToActCatchesQwenTruncationIntent(t *testing.T) {
	text := "The updated structure is clear. Right - let me write the updated document now."
	if !looksAboutToAct(text) {
		t.Fatalf("expected Qwen-style action intent to be detected")
	}
}

func TestLooksAboutToActCatchesRepairIntent(t *testing.T) {
	text := "I need to fix the app.py - it got corrupted. Let me rewrite it cleanly, then build out the frontend files in sequence."
	if !looksAboutToAct(text) {
		t.Fatalf("expected repair/rewrite intent to be detected")
	}
}

func TestLooksAboutToActIgnoresOrdinaryFinishedText(t *testing.T) {
	text := "The build passed and the document was updated."
	if looksAboutToAct(text) {
		t.Fatalf("ordinary finished text should not look about to act")
	}
}

func TestBuildDirectivePromptRequiresToolCall(t *testing.T) {
	prompt := buildDirectivePrompt("Right - let me write the updated document now.")
	if !strings.Contains(prompt, "Call write_file or edit_file RIGHT NOW") || !strings.Contains(prompt, "tool call") {
		t.Fatalf("directive prompt is not forceful enough: %s", prompt)
	}
}

func TestSanitizeVisibleModelTextRemovesGemmaChannelTokens(t *testing.T) {
	got := sanitizeVisibleModelText("<|channel|>thought <channel|>Hello! I am TheMauler.")
	if got != "Hello! I am TheMauler." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextRemovesMalformedGemmaChannelTokens(t *testing.T) {
	got := sanitizeVisibleModelText("<|channel>thought <channel|>Hello! I am TheMauler.")
	if got != "Hello! I am TheMauler." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextDropsFakeSystemTail(t *testing.T) {
	got := sanitizeVisibleModelText(`Hello<end_of_turn> <start_of_turn>system {"stdout":"fake"}`)
	if got != "Hello" {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextHandlesSplitChannelLeakAfterAccumulation(t *testing.T) {
	raw := "<|chan" + "nel|>thought <channel|>Hello."
	got := sanitizeVisibleModelText(raw)
	if got != "Hello." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextRemovesBareThoughtPrefix(t *testing.T) {
	got := sanitizeVisibleModelText("thoughtIt is currently 2026-05-30.")
	if got != "It is currently 2026-05-30." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestParseInlineToolMarkupRepairsLocalModelToolText(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
		{Function: llm.ToolFunctionDef{Name: "session_search"}},
	}
	text := `<shell><command>ls -la</command></shell>
<session_search><query>project build complete</query><limit>10</limit></session_search>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "shell" || !strings.Contains(string(calls[0].Function.Arguments), `"command":"ls -la"`) {
		t.Fatalf("bad shell repair: %#v", calls[0])
	}
	if calls[1].Function.Name != "session_search" || !strings.Contains(string(calls[1].Function.Arguments), `"limit":10`) {
		t.Fatalf("bad session_search repair: %#v", calls[1])
	}
}

func TestParseInlineToolMarkupRepairsFunctionStyleToolText(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "read_file"}},
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	text := `I'll inspect the workspace now.

glob("**/*.go")
read_file('AGENTS.md')
shell(` + "`" + `go test ./...` + "`" + `)`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3: %#v", len(calls), calls)
	}
	got := map[string]string{}
	for _, call := range calls {
		got[call.Function.Name] = string(call.Function.Arguments)
	}
	if !strings.Contains(got["glob"], `"pattern":"**/*.go"`) {
		t.Fatalf("bad glob repair: %s", got["glob"])
	}
	if !strings.Contains(got["read_file"], `"path":"AGENTS.md"`) {
		t.Fatalf("bad read_file repair: %s", got["read_file"])
	}
	if !strings.Contains(got["shell"], `"command":"go test ./..."`) {
		t.Fatalf("bad shell repair: %s", got["shell"])
	}
}

func TestParseInlineToolMarkupRepairsQwenToolCallTemplate(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "read_file"}},
	}
	text := `<tool_call>
<function=glob>
<parameter=pattern>
**/*.go
</parameter>
</function>
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad Qwen tool-call repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsHermesJSONToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "read_file"}},
	}
	text := `<tool_call>
{"name":"read_file","arguments":{"path":"AGENTS.md","start_line":1,"end_line":20}}
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read_file" || !strings.Contains(args, `"path":"AGENTS.md"`) || !strings.Contains(args, `"end_line":20`) {
		t.Fatalf("bad Hermes tool-call repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsNamedParametersToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_create"}},
		{Function: llm.ToolFunctionDef{Name: "read_many"}},
	}
	text := `<tool_call name="todo_create">
<parameters>
{
  "items": [
    "Explore project structure and read existing files",
    "Fix identified issues in the code"
  ]
}
</parameters>
</tool_call>
<tool_call name="read_many">
<parameters>{"paths":["main.go","AGENTS.md"]}</parameters>
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "todo_create" || !strings.Contains(string(calls[0].Function.Arguments), "Explore project structure") {
		t.Fatalf("bad todo_create repair: %#v args=%s", calls[0], calls[0].Function.Arguments)
	}
	if calls[1].Function.Name != "read_many" || !strings.Contains(string(calls[1].Function.Arguments), `"paths":["`) {
		t.Fatalf("bad read_many repair: %#v args=%s", calls[1], calls[1].Function.Arguments)
	}
}

func TestParseInlineToolMarkupRepairsSelfClosingParametersAttribute(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read_file",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<|channel>thought
<channel|><tool_call name="read_file" parameters={"path": "idea.md"} />`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read_file" || !strings.Contains(args, `"path":"idea.md"`) {
		t.Fatalf("bad self-closing repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsQuotedParametersAttributeWithRawQuotes(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read_file",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<|channel>thought <channel|><tool_call name="read_file" parameters="{ "path": "idea.md" }"/>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read_file" || !strings.Contains(args, `"path":"idea.md"`) {
		t.Fatalf("bad quoted parameters repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsEscapedQuotedParametersAttribute(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "todo_create",
			Parameters: json.RawMessage(`{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"string"}}}}`),
		}},
	}
	text := `<tool_call name="todo_create" parameters="{\"items\":[\"Inspect existing app.py\",\"Test the application\"]}"/>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_create" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Test the application") {
		t.Fatalf("bad escaped parameters repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupDropsRepairedCallMissingRequiredArgs(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read_file",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<tool_call name="read_file" />`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 0 {
		t.Fatalf("expected invalid repaired call to be dropped, got %#v", calls)
	}
}

func TestParseInlineToolMarkupInfersMalformedQwenToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "grep"}},
	}
	text := "```\n\n```\n\n<tool_call>\n{\"pattern\":\"**/*.go\"}"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad malformed Qwen repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsFencedToolInputJSON(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
	}
	text := "```json\n{\n  \"tool\": \"glob\",\n  \"input\": {\n    \"pattern\": \"**/*.go\"\n  }\n}\n```"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad fenced JSON repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsGemmaPipeToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "web_search"}},
	}
	text := `<tool_call>call:web_search|limit:3,query:'<|'>top three car wash places near Bradley Stoke Bristol UK<|'}></tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "web_search" || !strings.Contains(args, `"limit":3`) || !strings.Contains(args, "Bradley Stoke Bristol UK") {
		t.Fatalf("bad Gemma pipe repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaTodoArrayPipeToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_create"}},
	}
	text := `thought<tool_call>call:todo_create|items:["Inspect current settings and profiles in the repository","Research optimal settings/parameters for Gemma4(LLM)","Compare research with current configuration","Suggest specific updates to settings/profiles"]<tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_create" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Gemma4") {
		t.Fatalf("bad Gemma todo repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaTodoArrayBraceToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_create"}},
	}
	text := `thought<|tool_call>call:todo_create{items:[
    "Inspect current settings and profiles in the repository",
    "Research optimal settings/parameters for Gemma4(LLM)",
    "Compare research with current configuration",
    "Suggest specific updates to settings/profiles"
]}<tool_call|>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_create" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Gemma4") {
		t.Fatalf("bad Gemma todo brace repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaFencedFunctionArray(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_create"}},
	}
	text := "<|channel>thought\n<channel|>```json\n[\n  {\n    \"function\": \"todo_create\",\n    \"parameters\": {\n      \"items\": [\n        \"inspect settings\",\n        \"run tests\"\n      ]\n    }\n  }\n]\n```"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_create" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "run tests") {
		t.Fatalf("bad Gemma fenced function repair: %#v args=%s", calls[0], args)
	}
}

func TestContainsInlineToolMarkupDetectsGemmaPipeToolCall(t *testing.T) {
	if !containsInlineToolMarkup(`<tool_call>call:web_search|query:'x'</tool_call>`) {
		t.Fatalf("expected Gemma-style tool markup to be detected")
	}
}

func TestParseInlineToolMarkupRepairsGemmaAngleCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	text := `<|channel>thought <channel|><call:shell command="ls -R" /> <end_of_turn> <start_of_turn>system {"stdout":"fake"}`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "shell" || !strings.Contains(string(calls[0].Function.Arguments), `"command":"ls -R"`) {
		t.Fatalf("bad Gemma angle call repair: %#v args=%s", calls[0], calls[0].Function.Arguments)
	}
}

func TestToolChoiceTreatsDirectoryQuestionAsTask(t *testing.T) {
	if looksConversational("whats in this directory?") {
		t.Fatalf("directory listing question should expose tools")
	}
	if got := toolChoiceFor("whats in this directory?", 0, 0); got != "auto" {
		t.Fatalf("toolChoiceFor directory question = %q, want auto", got)
	}
}

func TestLooksAboutToActCatchesLookIntent(t *testing.T) {
	text := "Let me look at the project structure and key files to understand what this is about."
	if !looksAboutToAct(text) {
		t.Fatalf("look intent should force an inspection tool directive")
	}
}

func TestLooksAboutToActCatchesFinishedSentenceIntent(t *testing.T) {
	text := "I now have everything I need. Let me create the new README with all the optimization details incorporated."
	if !looksAboutToAct(text) {
		t.Fatalf("finished sentence action intent should still force a tool directive")
	}
	if !strings.Contains(buildDirectivePrompt(text), "tool call") {
		t.Fatalf("directive prompt should require a tool call")
	}
}

func TestLooksAboutToActCatchesInspectionIntent(t *testing.T) {
	text := "Let me explore the project structure more broadly to find the real project README and understand what this project does."
	if !looksAboutToAct(text) {
		t.Fatalf("inspection intent should force a tool directive")
	}
	prompt := buildDirectivePrompt(text)
	if !strings.Contains(prompt, "glob") || !strings.Contains(prompt, "read_file") {
		t.Fatalf("inspection directive should suggest inspection tools: %s", prompt)
	}
}

func TestClassifyAgentMode(t *testing.T) {
	tests := map[string]string{
		"please review this code":        "Reviewer",
		"latest news about local models": "Researcher",
		"fix the failing build error":    "Fixer",
		"plan the architecture":          "Planner",
		"implement the settings page":    "Builder",
		"hello there":                    "Auto",
	}
	for input, want := range tests {
		if got := classifyAgentMode(input).Name; got != want {
			t.Fatalf("classifyAgentMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestManualAgentMode(t *testing.T) {
	mode := manualAgentMode()
	if mode.Name != "Manual" || mode.Instructions == "" {
		t.Fatalf("manual mode not populated: %#v", mode)
	}
}

func TestGetHistoryStatsReturnsConcreteValues(t *testing.T) {
	app := &App{
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "hello"))

	stats := app.GetHistoryStats()

	if stats.Budget != 4096 {
		t.Fatalf("Budget = %d, want 4096", stats.Budget)
	}
	if stats.TokenCount == 0 {
		t.Fatalf("TokenCount should be populated: %#v", stats)
	}
}

func TestSaveFileContentSnapshotsExistingFileForRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{rollback: &agent.Rollback{}}

	if err := app.SaveFileContent(path, "after"); err != nil {
		t.Fatal(err)
	}
	if app.rollback.Len() != 1 {
		t.Fatalf("rollback depth = %d, want 1", app.rollback.Len())
	}
	msg := app.Undo()
	if !strings.Contains(msg, "restored") {
		t.Fatalf("Undo() = %q, want restored message", msg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before" {
		t.Fatalf("file content after undo = %q, want before", data)
	}
}

func TestDoCompactCanBeCalledWithoutCallerHoldingLock(t *testing.T) {
	app := &App{history: agent.NewHistory(4096)}
	for i := 0; i < 14; i++ {
		app.history.Append(llm.NewTextMessage(llm.RoleUser, "important details"))
	}

	app.doCompact(context.Background(), &summaryClient{}, settings.Profile{})

	msgs := app.history.Messages()
	joined := ""
	for _, msg := range msgs {
		joined += messageText(msg)
	}
	if !strings.Contains(joined, "summary ok") {
		t.Fatalf("history was not compacted with summary: %#v", msgs)
	}
}

func TestSelectAgentModeOverrideAndPresetInstructions(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Reviewer"
	cfg.Agents.Presets["Reviewer"] = settings.AgentModePreset{
		Enabled:      true,
		Instructions: "custom review instructions",
	}

	mode := selectAgentMode("please implement this", cfg)

	if mode.Name != "Reviewer" || mode.Instructions != "custom review instructions" {
		t.Fatalf("override/preset not applied: %#v", mode)
	}
}

func TestApplyAgentPresetOfflineDisablesExternalTools(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.OfflineOnly = true
	profiles := settings.DefaultProfiles()
	profile := activeProfile(&cfg, &profiles)
	autonomous := false

	applyAgentPreset(&cfg, &profiles, AgentMode{Name: "Researcher"}, &profile, &autonomous)

	for _, name := range []string{"web_search", "fetch_url", "browser_open"} {
		if cfg.Tools.EnabledTools[name] {
			t.Fatalf("%s should be disabled by offline mode", name)
		}
	}
	if cfg.Tools.ActiveToolset != "web-research" {
		t.Fatalf("researcher preset should select web-research toolset, got %q", cfg.Tools.ActiveToolset)
	}
}

func TestAgentPresetContextBudgetDoesNotShrinkLoadContext(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Builder"
	cfg.Agents.Presets["Builder"] = settings.AgentModePreset{
		Enabled:       true,
		ContextBudget: 32768,
	}
	profiles := settings.DefaultProfiles()
	profile := settings.Profile{
		Name:      "gemma4-31b",
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf",
		CtxTokens: 120000,
	}
	autonomous := false

	mode := selectAgentMode("build this", cfg)
	applyAgentPreset(&cfg, &profiles, mode, &profile, &autonomous)

	if profile.CtxTokens != 120000 {
		t.Fatalf("profile ctx_tokens = %d, want backend load context preserved", profile.CtxTokens)
	}
	if mode.ContextBudget != 32768 {
		t.Fatalf("mode context budget = %d, want preset working budget", mode.ContextBudget)
	}
}

func TestApplySafetyPresetUnrestrictedEnablesFullAccess(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Tools.ConfirmExec = true
	cfg.Tools.ConfirmWrites = true
	cfg.Agents.OfflineOnly = true
	cfg.Tools.EnabledTools["web_search"] = false
	app := &App{cfg: &cfg}

	if err := app.ApplySafetyPreset("unrestricted"); err != nil {
		t.Fatal(err)
	}

	if !app.autonomous || app.cfg.Agents.OfflineOnly || app.cfg.Tools.ConfirmExec || app.cfg.Tools.ConfirmWrites {
		t.Fatalf("unrestricted preset did not enable full access: autonomous=%v cfg=%#v", app.autonomous, app.cfg)
	}
	if !app.cfg.Tools.EnabledTools["web_search"] || !app.cfg.Tools.EnabledTools["shell"] {
		t.Fatalf("unrestricted preset should enable default tools: %#v", app.cfg.Tools.EnabledTools)
	}
	if app.cfg.Tools.ActiveToolset != "unrestricted" {
		t.Fatalf("unrestricted preset should select unrestricted toolset, got %q", app.cfg.Tools.ActiveToolset)
	}
}

func TestApplySafetyPresetOfflineSelectsOfflineToolsetAndBlocksBrowserAgent(t *testing.T) {
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg}

	if err := app.ApplySafetyPreset("offline"); err != nil {
		t.Fatal(err)
	}

	if app.autonomous || !app.cfg.Agents.OfflineOnly || app.cfg.Tools.ActiveToolset != "offline" {
		t.Fatalf("offline preset did not set expected state: autonomous=%v cfg=%#v", app.autonomous, app.cfg)
	}
	if app.cfg.Tools.EnabledTools["web_search"] || app.cfg.Tools.EnabledTools["browser_agent"] {
		t.Fatalf("offline preset should disable external/browser-agent tools: %#v", app.cfg.Tools.EnabledTools)
	}
}

func TestBuildSystemPromptInjectsRelevantMemory(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Auto"}, []MemoryEntry{{
		Title:      "Provider",
		Content:    "LM Studio runs on the LAN endpoint.",
		Kind:       "preference",
		Importance: 5,
		Tags:       []string{"local"},
	}}, nil)

	if !strings.Contains(prompt, "Relevant project memory") || !strings.Contains(prompt, "LM Studio runs") || !strings.Contains(prompt, "tags=local") {
		t.Fatalf("memory was not injected into prompt: %s", prompt)
	}
}

func TestMemoryScoringPrefersTaggedImportantPinnedEntries(t *testing.T) {
	terms := keywordSet("use lm studio local provider")
	plain := MemoryEntry{
		Title:      "Provider",
		Content:    "LM Studio is available.",
		Kind:       "note",
		Importance: 1,
		UpdatedAt:  "2020-01-01T00:00:00Z",
	}
	rich := MemoryEntry{
		Title:      "Local provider preference",
		Content:    "Use the LAN endpoint for LM Studio.",
		Tags:       []string{"lm", "studio", "local", "provider"},
		Kind:       "preference",
		Importance: 5,
		Pinned:     true,
		UpdatedAt:  timeNowRFC3339(),
	}

	if scoreMemory(rich, terms, "provider") <= scoreMemory(plain, terms, "provider") {
		t.Fatalf("rich memory should outrank plain memory")
	}
}

func TestMemoryNormalisation(t *testing.T) {
	tags := normaliseTags([]string{" Local ", "#local", "Qwen", ""})
	if strings.Join(tags, ",") != "local,qwen" {
		t.Fatalf("tags were not normalized/deduped: %#v", tags)
	}
	if normaliseMemoryKind("bad-kind") != "note" {
		t.Fatalf("unknown memory kind should fall back to note")
	}
	if clampInt(9, 1, 5, 3) != 5 || clampInt(0, 1, 5, 3) != 3 {
		t.Fatalf("clampInt returned unexpected values")
	}
}

func timeNowRFC3339() string {
	return "2099-01-01T00:00:00Z"
}

type countingLoader struct {
	loads         int
	actualContext int
}

type summaryClient struct{}

type namedDestructiveTool struct {
	name string
}

func (t *namedDestructiveTool) Name() string                                         { return t.name }
func (t *namedDestructiveTool) Description() string                                  { return "" }
func (t *namedDestructiveTool) Schema() json.RawMessage                              { return nil }
func (t *namedDestructiveTool) Run(context.Context, json.RawMessage) (string, error) { return "", nil }
func (t *namedDestructiveTool) Destructive() bool                                    { return true }

func (c *countingLoader) Chat(context.Context, llm.Request) (<-chan llm.Delta, error) {
	return nil, nil
}

func (c *countingLoader) Models(context.Context) ([]string, error) {
	return nil, nil
}

func (c *countingLoader) Ping(context.Context) error {
	return nil
}

func (c *countingLoader) Name() string {
	return "test"
}

func (c *countingLoader) LoadModel(context.Context) error {
	c.loads++
	if c.actualContext > 0 {
		c.actualContext = 32768
	}
	return nil
}

func (c *countingLoader) ActualContextLength(context.Context) int {
	return c.actualContext
}

func (c *summaryClient) Chat(context.Context, llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	ch <- llm.Delta{Content: "summary ok"}
	close(ch)
	return ch, nil
}

func (c *summaryClient) Models(context.Context) ([]string, error) { return nil, nil }
func (c *summaryClient) Ping(context.Context) error               { return nil }
func (c *summaryClient) Name() string                             { return "summary" }
