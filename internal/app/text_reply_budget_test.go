package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"mauler/internal/controlplane"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestPostToolCutoffUsesSavedOutcomeOnce(t *testing.T) {
	for _, tc := range []struct {
		name, path   string
		secondCutoff bool
	}{
		{"successful tool", "notes.txt", false},
		{"failed tool", "missing.txt", false},
		{"second cutoff", "notes.txt", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, profile := recoveryTestApp(t)
			restoreWorkingDir(t)
			if err := os.Chdir(a.cfg.Context.WorkspaceDir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("notes.txt", []byte("CUT_OFF_MARKER"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := &nativeFixtureClient{chat: func(_ context.Context, req llm.Request) (<-chan llm.Delta, error) {
				calls++
				var delta llm.Delta
				switch calls {
				case 1:
					args, _ := json.Marshal(map[string]string{"path": tc.path})
					delta = llm.Delta{Done: true, ToolCalls: []llm.ToolCallDef{{ID: "one-read", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: args}}}}
				case 2:
					delta = llm.Delta{Content: "The recorded tool outcome", Truncated: true, Done: true, Usage: &llm.Usage{PromptTokens: 200, CompletionTokens: 64}}
				case 3:
					if len(req.Tools) != 0 || req.ToolChoice != "none" || req.MaxTokens <= 2048 {
						t.Fatalf("invalid recovery request tools=%d choice=%q max=%d", len(req.Tools), req.ToolChoice, req.MaxTokens)
					}
					last := req.Messages[len(req.Messages)-1]
					if last.Role != llm.RoleUser || messageText(last) != postToolReplyRetryInstruction {
						t.Fatal("expected the final answer instruction after the recorded tool result")
					}
					recorded := 0
					for _, msg := range req.Messages {
						if msg.Role == llm.RoleTool && msg.ToolCallID == "one-read" {
							recorded++
						}
					}
					if recorded != 1 {
						t.Fatalf("recorded tool results = %d", recorded)
					}
					delta = llm.Delta{Content: "This is the complete report from the recorded read.", Truncated: tc.secondCutoff, Done: true}
				default:
					return nil, fmt.Errorf("unexpected additional model request %d", calls)
				}
				ch := make(chan llm.Delta, 1)
				ch <- delta
				close(ch)
				return ch, nil
			}}
			old := buildClientForAgent
			buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
			defer func() { buildClientForAgent = old }()
			prompt := "Read " + tc.path + " with the file read tool and report the result."
			run := startTaskRun(prompt, "Auto", profile.Name, profile.ModelID)
			run.conversationMode = conversationModeAdaptive
			result := a.runAgentLoop(context.Background(), llm.NewTextMessage(llm.RoleUser, prompt), profile, a.cfg, false, baseMode("Auto"), nil, nil, run)
			if calls != 3 || len(result.Tools) != 1 || result.Tools[0].Name != "read" || (tc.path == "missing.txt") != (result.Tools[0].Status == "error") {
				t.Fatalf("read was lost, repeated, or changed outcome: calls=%d tools=%#v summary=%s", calls, result.Tools, result.Summary)
			}
			if tc.secondCutoff && (result.StopReason != "output_limit" || !strings.Contains(result.Summary, "recorded tool outcome")) {
				t.Fatalf("first partial was not retained: %s: %s", result.StopReason, result.Summary)
			}
			if !tc.secondCutoff && !strings.Contains(result.Summary, "complete report") {
				t.Fatalf("recovered reply was lost: %s: %s", result.StopReason, result.Summary)
			}
		})
	}
}

func TestRVNPostToolCutoffLive(t *testing.T) {
	base := os.Getenv("MAULER_RVN_CUTOFF_URL")
	if base == "" {
		t.Skip("requires an explicit loopback InferenceBridge runtime")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "/v1" {
		t.Fatal("expected a loopback /v1 URL")
	}
	runtime := &localQualificationRuntime{base: base, key: os.Getenv("MAULER_RVN_CUTOFF_KEY"), http: &http.Client{Timeout: 10 * time.Second}, model: "RVN-Q4_K_M-multilingual-mtp.gguf"}
	state, err := runtime.get("runtime/status")
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := state["last_launch_preview"].(map[string]any)
	if state["model"] != runtime.model || preview["spec_type"] != "draft-mtp" || preview["spec_draft_n_max"] != float64(3) || !runtime.idle() {
		t.Fatal("expected idle RVN with native MTP x3")
	}
	ctx, _ := preview["context_size"].(float64)
	runtime.context = int(ctx)
	for _, path := range []string{"notes.txt", "missing.txt"} {
		t.Run(path, func(t *testing.T) {
			a, profile, real, restore := qualificationApp(t, t.TempDir(), runtime)
			defer restore()
			if err := os.WriteFile("notes.txt", []byte("RVN_MAULER_READ_MARKER"), 0600); err != nil {
				t.Fatal(err)
			}
			forced := false
			retryObserved := false
			buildClientForAgent = func(settings.Profile) (llm.Client, error) {
				return &nativeFixtureClient{chat: func(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
					for _, msg := range req.Messages {
						if msg.Role == llm.RoleUser && messageText(msg) == postToolReplyRetryInstruction {
							retryObserved = true
							if len(req.Tools) != 0 || req.ToolChoice != "none" {
								t.Error("live retry advertised tools")
							}
						}
						if !forced && msg.Role == llm.RoleTool {
							req.MaxTokens = 64
							forced = true
						}
					}
					return real.Chat(ctx, req)
				}}, nil
			}
			numbers := make([]string, 92)
			for i := range numbers {
				numbers[i] = fmt.Sprint(i + 1)
			}
			sequence := strings.Join(numbers, ", ")
			prompt := fmt.Sprintf("Read %s using the read tool exactly once. Use the marker if it exists or state the read failed. Then copy this entire sequence exactly as a separate line without any tools: %s", path, sequence)
			cancel, done := qualificationRun(a, profile, prompt)
			defer cancel()
			result, finished := qualificationResult(done)
			if !finished || !forced || !retryObserved || len(result.Tools) != 1 || !strings.Contains(result.Summary, sequence) || (path == "missing.txt") != (result.Tools[0].Status == "error") {
				t.Fatalf("post-tool live proof failed: finished=%v forced=%v retry=%v tools=%#v stop=%s summary=%q", finished, forced, retryObserved, result.Tools, result.StopReason, result.Summary)
			}
			outcome, _, _ := strings.Cut(strings.ToLower(result.Summary), "\n")
			reported := strings.Contains(outcome, "rvn_mauler_read_marker")
			if path == "missing.txt" {
				reported = strings.Contains(outcome, "failed") && strings.Contains(outcome, path)
			}
			if !reported {
				t.Fatalf("final reply did not report the observed tool outcome: %q", result.Summary)
			}
			t.Logf("RVN MTP x3 context=%d read_outcome=%s read_count=%d reply_complete=true", runtime.context, result.Tools[0].Status, len(result.Tools))
		})
	}
}

func TestTextReplyBudgetRespectsActualContextAndTools(t *testing.T) {
	req := llm.Request{MaxTokens: 1024, ToolChoice: "none", Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "Write a long answer, around 2000 words.")}}
	if changed, err := applyTextReplyResponseBudget(&req, "2000 words", true, 35072); err != nil || !changed || req.MaxTokens != 8192 {
		t.Fatalf("long answer budget = %d, changed=%v err=%v", req.MaxTokens, changed, err)
	}
	if _, err := applyTextReplyResponseBudget(&req, "2000 words", true, 4096); err != nil || req.MaxTokens >= 4096 {
		t.Fatalf("context headroom ignored: %d %v", req.MaxTokens, err)
	}
	req.Messages = []llm.Message{llm.NewTextMessage(llm.RoleUser, strings.Repeat("x", 12000))}
	if _, err := applyTextReplyResponseBudget(&req, "long answer", true, 4096); err == nil {
		t.Fatal("exhausted context silently accepted")
	}
	req = llm.Request{MaxTokens: 1024, ToolChoice: "required", Tools: []llm.ToolDef{{Function: llm.ToolFunctionDef{Name: "write"}}}}
	if changed, err := applyTextReplyResponseBudget(&req, "write 2000 words", false, 35072); err != nil || changed || req.MaxTokens != 1024 {
		t.Fatal("answer budget changed the tool-call boundary")
	}
}

func TestNativeReadOnlyFixtureFilenameRetainsReadTool(t *testing.T) {
	a, _ := recoveryTestApp(t)
	for _, prompt := range []string{
		"Read append-fixture.txt and show its exact contents. Use file tools only. Do not modify any file.",
		"Read weather.json and show its current value. Do not edit it.",
	} {
		defs, choice := toolDefsAndChoiceForTurnWithState(a.registry, a.cfg.Tools, prompt, 0, 0, TerminalStateSnapshot{})
		if !toolCallAdvertised(defs, "read") || choice == "none" || needsLiveExternalInfoTool(prompt) {
			t.Fatalf("filename misrouted: %s / %s", choice, toolProtocolToolNames(defs))
		}
	}
	if !needsLiveExternalInfoTool("Show today's football fixtures") || !needsLiveExternalInfoTool("What is the weather tomorrow?") {
		t.Fatal("real live facts lost their tool route")
	}
}

func TestTextReplyContinuationCompletesOneRetainedAnswer(t *testing.T) {
	a, profile := recoveryTestApp(t)
	profile.CtxTokens, a.contextWindow = 35072, 35072
	calls := 0
	client := &nativeFixtureClient{chat: func(_ context.Context, req llm.Request) (<-chan llm.Delta, error) {
		calls++
		if req.ToolChoice != "none" || len(req.Tools) != 0 || req.MaxTokens < 2048 {
			t.Fatalf("continuation changed tool boundary or tiny budget: %+v", req)
		}
		ch := make(chan llm.Delta, 1)
		if calls == 1 {
			ch <- llm.Delta{Content: "Our guild has achieved something special. There will", Truncated: true, Done: true}
		} else {
			found := false
			for _, msg := range req.Messages {
				found = found || msg.Role == llm.RoleSystem && messageText(msg) == textReplyContinuationInstruction
			}
			if !found {
				t.Fatal("continuation instruction missing")
			}
			ch <- llm.Delta{Content: "be more great moments together. Thank you, everyone!", Done: true}
		}
		close(ch)
		return ch, nil
	}}
	old := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
	t.Cleanup(func() { buildClientForAgent = old })
	prompt := "Write a long message for my guild, around 2000 words. No files or tools."
	run := startTaskRun(prompt, "Auto", profile.Name, profile.ModelID)
	run.conversationMode = conversationModeAdaptive
	result := a.runAgentLoop(context.Background(), llm.NewTextMessage(llm.RoleUser, prompt), profile, a.cfg, false, baseMode("Auto"), nil, nil, run)
	if calls != 2 || result.Status != "done" || result.StopReason != "" || result.Control.Phase != controlplane.PhaseComplete {
		t.Fatalf("text did not complete: calls=%d %s/%s", calls, result.Status, result.StopReason)
	}
	visible := toSessionChatMessages(a.history.Messages())
	if len(visible) != 2 || strings.Count(visible[1].Content, "There will") != 1 || !strings.HasSuffix(visible[1].Content, "everyone!") {
		t.Fatalf("retained answer missing or duplicated: %#v", visible)
	}
}

func TestTextReplyContinuationBoundAndRepeatedOutput(t *testing.T) {
	var c textReplyContinuation
	for _, text := range []string{"First partial", "Second partial", "Third partial"} {
		if !c.advance(text) {
			t.Fatal("continuation budget ended too soon")
		}
	}
	if c.advance("Fourth partial") || c.attempts != maxTextReplyContinuations {
		t.Fatal("unbounded continuation")
	}
	if got := joinTextReply(c.prefix, "Third partial"); got != c.prefix {
		t.Fatal("a repeated chunk duplicated the retained answer")
	}
	c = textReplyContinuation{}
	if !c.advance("same") || c.advance("same") || c.advance(" ") {
		t.Fatal("repeated or empty output was treated as progress")
	}
}
