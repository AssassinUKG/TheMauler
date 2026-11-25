package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/controlplane"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
	"mauler/internal/tools"
)

func recoveryTestApp(t *testing.T) (*App, settings.Profile) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", filepath.Join(root, "config"))
	cfg := canonicalAgentEvalSettings(settings.DefaultSettings())
	cfg.Context.WorkspaceDir = root
	cfg.Context.MAULERMDPath = ""
	cfg.Agents.RequirePlan = false
	cfg.Agents.OfflineOnly = true
	cfg.Agents.ThinkingMode = "off"
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.ActiveProfile = "fixture"
	profile := settings.Profile{Name: "fixture", Provider: "fixture", Backend: "openai-compatible", BaseURL: "http://127.0.0.1:1/v1", ModelID: "fixture-model", CtxTokens: 8192, NoThink: settings.GenerationParams{MaxTokens: 2048}}
	profiles := settings.ProfilesFile{Profiles: map[string]settings.Profile{"fixture": profile}}
	db, err := store.Open(filepath.Join(root, "config", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := &App{cfg: &cfg, profiles: &profiles, db: db, suppressEvents: true, history: agent.NewHistory(8192), rollback: &agent.Rollback{}, registry: tools.New(), autoAgents: true, conversationMode: conversationModeAdaptive}
	return app, profile
}

func recoveryTestRun(t *testing.T, a *App, prompt string) TaskRun {
	t.Helper()
	run := startTaskRun(prompt, "Auto", "fixture", "fixture-model")
	run.conversationMode = conversationModeAdaptive
	if err := a.initializeRunControlPlane(&run, a.cfg, baseMode("Auto"), false); err != nil {
		t.Fatal(err)
	}
	a.history.Append(llm.NewTextMessage(llm.RoleUser, prompt))
	return run
}

func TestRecoverySnapshotBeforeFirstRequestAndIntent(t *testing.T) {
	a, _ := recoveryTestApp(t)
	run := recoveryTestRun(t, a, "Create artifact.txt containing exactly the text represented by this JSON string: \"original bytes\"")
	a.activeConversationName = "original-chat"
	if err := a.checkpointBeforeExternalWork(&run, *a.cfg); err != nil {
		t.Fatal(err)
	}
	cps, err := a.ListResumableRuns()
	if err != nil || len(cps) != 1 || len(cps[0].Messages) != 1 || cps[0].ConversationName != "original-chat" || cps[0].RecoveryVersion != 1 {
		t.Fatalf("first request not preserved: %v %#v", err, cps)
	}
	child, err := prepareCheckpointRun(cps[0], *a.cfg, "Auto", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	if child.Contract.RunID == run.ID || child.Contract.ParentDigest != run.Contract.Digest || child.Contract.Digest == run.Contract.Digest {
		t.Fatal("recovery did not bind fresh contract identity")
	}
	call := llm.ToolCallDef{ID: "call-a", Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(`{"file_path":"artifact.txt","content":"original bytes"}`)}}
	if err := a.checkpointToolIntent(&run, *a.cfg, call); err != nil {
		t.Fatal(err)
	}
	cps, err = a.ListResumableRuns()
	if err != nil || len(cps) != 1 || cps[0].ResumeBlockedReason == "" {
		t.Fatal("side-effect intent was not quarantined")
	}
	before := len(a.history.Messages())
	if err := a.ResumeRun(run.ID); err == nil || !strings.Contains(err.Error(), "replay refused") {
		t.Fatalf("unsafe replay admitted: %v", err)
	}
	if a.agentRunning || len(a.history.Messages()) != before {
		t.Fatal("rejected resume changed active state")
	}
}

func TestRecoverySnapshotFailureDoesNotErasePriorSnapshot(t *testing.T) {
	a, _ := recoveryTestApp(t)
	run := recoveryTestRun(t, a, "Hello")
	if err := a.checkpointBeforeExternalWork(&run, *a.cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_recovery BEFORE UPDATE ON run_checkpoints BEGIN SELECT RAISE(FAIL,'fixture storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	run.State = "must-not-publish"
	if err := a.checkpointBeforeExternalWork(&run, *a.cfg); err == nil {
		t.Fatal("snapshot failure silently acknowledged")
	}
	rows, err := a.listTaskRuns()
	if err != nil || len(rows) != 1 || rows[0].State == "must-not-publish" {
		t.Fatalf("partial transaction published: %v", err)
	}
	cps, err := a.ListResumableRuns()
	if err != nil || len(cps) != 1 {
		t.Fatalf("prior snapshot lost: %v", err)
	}
}

func TestRecoveryTerminalRetirementIsAtomic(t *testing.T) {
	a, _ := recoveryTestApp(t)
	run := recoveryTestRun(t, a, "Hello")
	if err := a.checkpointBeforeExternalWork(&run, *a.cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_retirement BEFORE DELETE ON run_checkpoints BEGIN SELECT RAISE(FAIL,'fixture storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	run.Status = "done"
	if err := a.commitRecoveryTerminal(run, true); err == nil {
		t.Fatal("retirement failure ignored")
	}
	rows, _ := a.listTaskRuns()
	if rows[0].Status == "done" {
		t.Fatal("terminal row committed without checkpoint retirement")
	}
	if _, err := a.db.Exec(`DROP TRIGGER fail_retirement`); err != nil {
		t.Fatal(err)
	}
	if err := a.commitRecoveryTerminal(run, true); err != nil {
		t.Fatal(err)
	}
	cps, err := a.ListResumableRuns()
	if err != nil || len(cps) != 0 {
		t.Fatalf("terminal snapshot not retired: %v", err)
	}
}

func TestNativeStartupOwnsReconciliationAndRetainsCheckpoint(t *testing.T) {
	a, _ := recoveryTestApp(t)
	run := recoveryTestRun(t, a, "Hello")
	if err := a.checkpointBeforeExternalWork(&run, *a.cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.reconcileInterruptedNativeRuns(); err == nil {
		t.Fatal("unowned reconciliation admitted")
	}
	if err := a.ownNativeRuntime(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if a.runtimeOwner != nil {
			_ = a.runtimeOwner.Close()
		}
	})
	path := a.runtimeOwner.Name()
	if second, err := acquireRuntimeOwnerFile(path); err == nil {
		_ = second.Close()
		t.Fatal("second native data owner admitted")
	}
	if err := a.reconcileInterruptedNativeRuns(); err != nil {
		t.Fatal(err)
	}
	rows, _ := a.listTaskRuns()
	if len(rows) != 1 || rows[0].Status != "stopped" || rows[0].State != "interrupted" || rows[0].Generation != run.Generation || rows[0].Control.Phase != controlplane.PhaseCancelled {
		t.Fatalf("stale running state retained: %#v", rows)
	}
	cps, err := a.ListResumableRuns()
	if err != nil || len(cps) != 1 || cps[0].RunID != run.ID {
		t.Fatal("startup lost recovery snapshot")
	}
	_ = a.runtimeOwner.Close()
	a.runtimeOwner = nil
	next, err := acquireRuntimeOwnerFile(path)
	if err != nil {
		t.Fatal("closed owner did not release lease")
	}
	_ = next.Close()
}

func TestSecondAppDoesNotOpenOrMigrateOwnedData(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	first := New()
	if first.runtimeOwner == nil || first.db == nil {
		t.Fatal("first data owner unavailable")
	}
	t.Cleanup(func() { _ = first.db.Close(); _ = first.runtimeOwner.Close(); tools.SetTodoDB(nil); setMemoryDB(nil) })
	second := New()
	if second.db != nil || second.ledger != nil || second.runtimeOwnerErr == nil {
		t.Fatal("second app opened shared mutable state")
	}
	if err := second.SendMessage("Hello", nil, nil); err == nil {
		t.Fatal("unowned app accepted new work")
	}
}

func TestWorkspaceSwitchCannotOverwritePriorSavedConversation(t *testing.T) {
	for _, viaSettings := range []bool{false, true} {
		t.Run(map[bool]string{false: "selector", true: "settings"}[viaSettings], func(t *testing.T) {
			a, _ := recoveryTestApp(t)
			target := t.TempDir()
			restoreWorkingDir(t)
			if err := os.Chdir(a.cfg.Context.WorkspaceDir); err != nil {
				t.Fatal(err)
			}
			name, err := a.StartConversation("Workspace-A")
			if err != nil {
				t.Fatal(err)
			}
			a.history.Append(llm.NewTextMessage(llm.RoleUser, "A request"))
			a.history.Append(llm.NewTextMessage(llm.RoleAssistant, "A reply"))
			if err := a.autoSave(); err != nil {
				t.Fatal(err)
			}
			dir, err := sessionsDir()
			if err != nil {
				t.Fatal(err)
			}
			originalPath := filepath.Join(dir, name+".json")
			before, err := os.ReadFile(originalPath)
			if err != nil {
				t.Fatal(err)
			}
			if viaSettings {
				next := *a.cfg
				next.Context.WorkspaceDir = target
				err = a.UpdateSettings(next)
			} else {
				err = a.SetWorkingDir(target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.activeConversationName != "" {
				t.Fatal("old saved identity survived workspace switch")
			}
			newName, err := a.StartConversation("Workspace-B")
			if err != nil {
				t.Fatal(err)
			}
			if newName == name {
				t.Fatal("new workspace reused old chat identity")
			}
			a.history.Append(llm.NewTextMessage(llm.RoleUser, "B request"))
			a.history.Append(llm.NewTextMessage(llm.RoleAssistant, "B reply"))
			if err := a.autoSave(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(originalPath)
			if err != nil || string(after) != string(before) {
				t.Fatal("cross-workspace autosave overwrote the prior chat")
			}
		})
	}
}

func TestRecoveryConversationForkRetainsNewerHistory(t *testing.T) {
	a, _ := recoveryTestApp(t)
	name, err := a.StartConversation("original")
	if err != nil {
		t.Fatal(err)
	}
	a.history.Append(llm.NewTextMessage(llm.RoleUser, "later user turn"))
	a.history.Append(llm.NewTextMessage(llm.RoleAssistant, "later answer"))
	if err := a.autoSave(); err != nil {
		t.Fatal(err)
	}
	recovered, err := a.recoveryConversationName(RunCheckpoint{ConversationName: name})
	if err != nil {
		t.Fatal(err)
	}
	if recovered == name || !strings.HasPrefix(recovered, "Recovered-") {
		t.Fatal("recovery did not fork")
	}
	a.activeConversationName = recovered
	a.history.Replace([]llm.Message{llm.NewTextMessage(llm.RoleUser, "older snapshot")})
	if err := a.autoSave(); err != nil {
		t.Fatal(err)
	}
	dir, _ := sessionsDir()
	original, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil || !strings.Contains(string(original), "later user turn") {
		t.Fatal("checkpoint recovery overwrote newer conversation history")
	}
	next, err := a.recoveryConversationName(RunCheckpoint{ConversationName: name})
	if err != nil || next == recovered {
		t.Fatal("recovery name collision not avoided")
	}
}

func TestRecoveryRetainsAppendExpectationAndRejectsWrongWorkspace(t *testing.T) {
	a, _ := recoveryTestApp(t)
	path := filepath.Join(a.cfg.Context.WorkspaceDir, "artifact.txt")
	if err := os.WriteFile(path, []byte("original-prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	run := recoveryTestRun(t, a, `Append exactly the text represented by this JSON string to artifact.txt, preserving every existing byte: "suffix"`)
	cp := RunCheckpoint{RunID: run.ID, Prompt: run.Prompt, Run: run, RecoveryVersion: 1}
	original := run.Contract.AcceptanceChecks[len(run.Contract.AcceptanceChecks)-1].ExactFile
	if original == nil {
		t.Fatal("exact append contract missing")
	}
	if err := os.WriteFile(path, []byte("changed-by-other-process"), 0600); err != nil {
		t.Fatal(err)
	}
	child, err := prepareCheckpointRun(cp, *a.cfg, "Auto", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	recovered := child.Contract.AcceptanceChecks[len(child.Contract.AcceptanceChecks)-1].ExactFile
	if recovered == nil || recovered.SHA256 != original.SHA256 {
		t.Fatal("append expectation recaptured after crash")
	}
	other := *a.cfg
	other.Context.WorkspaceDir = t.TempDir()
	if _, err := prepareCheckpointRun(cp, other, "Auto", "fixture-model"); err == nil {
		t.Fatal("wrong workspace accepted")
	}
	cp.RecoveryVersion = 0
	if checkpointResumeBlock(cp) == "" {
		t.Fatal("legacy recovery upgraded without proof")
	}
}

type nativeFixtureClient struct {
	chat func(context.Context, llm.Request) (<-chan llm.Delta, error)
}

func (c *nativeFixtureClient) Chat(ctx context.Context, r llm.Request) (<-chan llm.Delta, error) {
	return c.chat(ctx, r)
}
func (c *nativeFixtureClient) Models(context.Context) ([]string, error) {
	return []string{"fixture-model"}, nil
}
func (c *nativeFixtureClient) Ping(context.Context) error { return nil }
func (c *nativeFixtureClient) Name() string               { return "fixture-local" }

func TestTruncatedNoToolReplyIsNotCompletedAndRetainsOutcome(t *testing.T) {
	a, profile := recoveryTestApp(t)
	profile.CtxTokens, a.contextWindow = 35072, 35072
	client := &nativeFixtureClient{chat: func(_ context.Context, req llm.Request) (<-chan llm.Delta, error) {
		cps, err := a.ListResumableRuns()
		if err != nil || len(cps) != 1 {
			t.Fatal("no durable snapshot before first model request")
		}
		if req.ToolChoice != "none" || len(req.Tools) != 0 || req.MaxTokens == 1024 {
			t.Fatalf("long reply incorrectly constrained: choice=%s tokens=%d", req.ToolChoice, req.MaxTokens)
		}
		ch := make(chan llm.Delta, 1)
		ch <- llm.Delta{Content: "The road ahead is long. There will", Truncated: true, Done: true}
		close(ch)
		return ch, nil
	}}
	old := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
	t.Cleanup(func() { buildClientForAgent = old })
	prompt := "Write a long message for my guild. Aim for 1500 words. No files or tools."
	run := startTaskRun(prompt, "Auto", profile.Name, profile.ModelID)
	run.conversationMode = conversationModeAdaptive
	result := a.runAgentLoop(context.Background(), llm.NewTextMessage(llm.RoleUser, prompt), profile, a.cfg, false, baseMode("Auto"), nil, nil, run)
	if result.Status == "done" || result.StopReason != "output_limit" || result.Control.Phase == controlplane.PhaseComplete {
		t.Fatalf("truncation accepted as completion: %s/%s", result.Status, result.StopReason)
	}
	visible := toSessionChatMessages(a.history.Messages())
	if len(visible) < 2 || visible[len(visible)-1].StopReason != "output_limit" {
		t.Fatalf("partial reply metadata missing: %#v", visible)
	}
}

func TestNativeStopJoinsAndPreservesPartialResponse(t *testing.T) {
	a, profile := recoveryTestApp(t)
	profile.CtxTokens = 35072
	a.profiles.Profiles[profile.Name] = profile
	started := make(chan struct{})
	calls := 0
	client := &nativeFixtureClient{chat: func(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
		calls++
		ch := make(chan llm.Delta)
		if calls == 1 {
			go func() { defer close(ch); ch <- llm.Delta{Content: "There will"}; close(started); <-ctx.Done() }()
		} else {
			go func() { ch <- llm.Delta{Content: "RECOVERY_OK", Done: true}; close(ch) }()
		}
		return ch, nil
	}}
	old := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
	t.Cleanup(func() { buildClientForAgent = old })
	if _, err := a.StartConversation("native-stop"); err != nil {
		t.Fatal(err)
	}
	if err := a.SendMessage("Write a long message for my guild. No files or tools.", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not start")
	}
	a.mu.Lock()
	done := a.agentDone
	a.mu.Unlock()
	a.StopAgent()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("native stop did not join")
	}
	rows, err := a.listTaskRuns()
	if err != nil || len(rows) != 1 || rows[0].Status != "stopped" || rows[0].StopReason != "user_stopped" {
		t.Fatalf("cancel result=%#v err=%v", rows, err)
	}
	visible := toSessionChatMessages(a.history.Messages())
	if len(visible) < 2 || visible[len(visible)-1].Content != "There will" || visible[len(visible)-1].StopReason != "user_stopped" {
		t.Fatalf("cancelled visible bytes lost: %#v", visible)
	}
	if err := a.SendMessage("Reply exactly RECOVERY_OK and nothing else", nil, nil); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	done = a.agentDone
	a.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("follow-up did not join")
	}
	if calls != 2 {
		t.Fatalf("unexpected replay/model calls: %d", calls)
	}
}
