package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestThinkingSiblingProfileMatchesSameProviderAndModel(t *testing.T) {
	base := settings.Profile{Name: "qwen3.6-nothink", Provider: "ib", ModelID: "qwen.gguf", Thinking: false}
	app := &App{profiles: &settings.ProfilesFile{Profiles: map[string]settings.Profile{
		"wrong-model-think": {Provider: "ib", ModelID: "other.gguf", Thinking: true},
		"qwen3.6-think":     {Provider: "ib", ModelID: "qwen.gguf", Thinking: true},
	}}, history: agent.NewHistory(8192)}
	got, ok := app.thinkingSiblingProfile(base)
	if !ok || !got.Thinking || got.ModelID != base.ModelID || got.Provider != base.Provider {
		t.Fatalf("thinking sibling = %#v ok=%v", got, ok)
	}
}

func TestEffortToThinkingMapping(t *testing.T) {
	profile := settings.Profile{
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral:  settings.GenerationParams{MaxTokens: 8192},
		ThinkCoding:   settings.GenerationParams{MaxTokens: 4096},
		NoThink:       settings.GenerationParams{MaxTokens: 2048},
	}
	cases := []struct {
		effort       string
		wantThinking bool
		wantCap      int
		wantCoding   bool
	}{
		{"minimal", false, 1024, true},
		{"low", false, 0, true},
		{"medium", true, 0, false},
		{"high", true, 0, false},
		{"xhigh", true, 0, false},
		{"bad", true, 0, false},
	}
	for _, tc := range cases {
		got := effortToThinking(tc.effort, profile)
		if got.enableThinking != tc.wantThinking || got.maxTokensCap != tc.wantCap || got.coding != tc.wantCoding {
			t.Fatalf("%s => %#v, want thinking=%v cap=%d coding=%v", tc.effort, got, tc.wantThinking, tc.wantCap, tc.wantCoding)
		}
	}
}

func TestEffortToThinkingDoesNotEnableDisabledProfileThinking(t *testing.T) {
	profile := settings.Profile{
		Thinking: false,
		NoThink:  settings.GenerationParams{MaxTokens: 2048},
	}
	got := effortToThinking("high", profile)
	if got.enableThinking {
		t.Fatalf("high effort must not force thinking on for a no-thinking profile: %#v", got)
	}
}

func TestChatThinkingModeForcesSupportedQwenOnAndOff(t *testing.T) {
	base := settings.Profile{
		ModelID:       "Qwen3.8-27B-Q4_K_M.gguf",
		Thinking:      false,
		PreserveThink: false,
		ThinkGeneral:  settings.GenerationParams{Temperature: 1.0, TopP: 0.95, TopK: 20, MaxTokens: 8192},
		ThinkCoding:   settings.GenerationParams{Temperature: 1.0, TopP: 0.95, TopK: 20, MaxTokens: 8192},
		NoThink:       settings.GenerationParams{Temperature: 0.7, TopP: 0.8, TopK: 20, MaxTokens: 4096},
	}

	on, effective, supported := applyThinkingMode(base, "on")
	if !supported || effective != "on" || !on.Thinking || !on.PreserveThink {
		t.Fatalf("Qwen thinking-on override failed: supported=%v effective=%q profile=%#v", supported, effective, on)
	}
	if effort := effectiveEffortForThinkingMode("none", effective, on); effort != "medium" {
		t.Fatalf("forced-on direct effort = %q, want medium", effort)
	}
	req := buildChatRequest(on, nil, nil, "none", false, false, effectiveEffortForThinkingMode("none", effective, on))
	if !req.EnableThinking || !req.PreserveThinking || req.Temperature != 1.0 {
		t.Fatalf("forced-on request did not use thinking path: %#v", req)
	}
	if shouldForceNoThinking(on, effective, 50, 2, 3) {
		t.Fatal("forced-on mode must not be adaptively switched off")
	}

	off, effective, supported := applyThinkingMode(on, "off")
	if !supported || effective != "off" || off.Thinking || off.PreserveThink {
		t.Fatalf("thinking-off override failed: supported=%v effective=%q profile=%#v", supported, effective, off)
	}
	req = buildChatRequest(off, nil, nil, "none", false, false, "xhigh")
	if req.EnableThinking || req.PreserveThinking || req.Temperature != 0.7 {
		t.Fatalf("forced-off request did not use direct path: %#v", req)
	}
}

func TestChatThinkingModeDoesNotInventUnsupportedCapability(t *testing.T) {
	profile := settings.Profile{ModelID: "supergemma4-26b-uncensored-fast-v2-Q4_K_M.gguf"}
	got, effective, supported := applyThinkingMode(profile, "on")
	if supported || effective != "unsupported" || got.Thinking {
		t.Fatalf("unsupported model should retain its profile capability: supported=%v effective=%q profile=%#v", supported, effective, got)
	}
}

func TestBuildChatRequestAppliesMinimalEffortAndForceNoThinkFloor(t *testing.T) {
	profile := settings.Profile{
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral:  settings.GenerationParams{Temperature: 1.0, MaxTokens: 8192, Seed: 1},
		ThinkCoding:   settings.GenerationParams{Temperature: 0.5, MaxTokens: 4096, Seed: 2},
		NoThink:       settings.GenerationParams{Temperature: 0.7, MaxTokens: 2048, Seed: 3},
	}
	req := buildChatRequest(profile, nil, nil, "", false, false, "minimal")
	if req.EnableThinking || req.PreserveThinking {
		t.Fatalf("minimal effort should disable thinking: %#v", req)
	}
	if req.MaxTokens != 1024 || req.Temperature != 0.7 || req.ReasoningEffort != "none" {
		t.Fatalf("minimal effort should use capped no-thinking params: %#v", req)
	}

	req = buildChatRequest(profile, nil, nil, "", true, false, "high")
	if req.EnableThinking || req.PreserveThinking {
		t.Fatalf("forceNoThink must override high effort: %#v", req)
	}
}

func TestQwen38LowEffortKeepsNativeReasoningEnabled(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.8-agent-stability",
		ModelID:       "Qwen3.8-27B-Q4_K_M.gguf",
		Thinking:      true,
		PreserveThink: true,
		ThinkCoding:   settings.GenerationParams{Temperature: 1.0, MaxTokens: 4096},
		NoThink:       settings.GenerationParams{Temperature: 0.7, MaxTokens: 2048},
	}
	req := buildChatRequest(profile, nil, nil, "", false, true, "low")
	if !req.EnableThinking || !req.PreserveThinking || req.ReasoningEffort != "low" || req.Temperature != 1.0 {
		t.Fatalf("Qwen3.8 low effort should use the native thinking path: %#v", req)
	}
}

func TestQwen38ReasoningEffortUsesOnlyOfficialValues(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.8-agent-stability",
		ModelID:       "Qwen3.8-27B-Q4_K_M.gguf",
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral:  settings.GenerationParams{Temperature: 1.0, MaxTokens: 4096},
		NoThink:       settings.GenerationParams{Temperature: 0.7, MaxTokens: 2048},
	}

	high := buildChatRequest(profile, nil, nil, "", false, false, "high")
	if high.ReasoningEffort != "xhigh" {
		t.Fatalf("Qwen3.8 high alias = %q, want xhigh", high.ReasoningEffort)
	}
	direct := buildChatRequest(profile, nil, nil, "", true, false, "high")
	if direct.EnableThinking || !direct.PreserveThinking || direct.ReasoningEffort != "" {
		t.Fatalf("Qwen3.8 direct turn should preserve history and omit reasoning_effort: %#v", direct)
	}
}

func TestSetReasoningEffortInterceptValidatesAndCaps(t *testing.T) {
	current := "medium"
	changes := 0
	out, ok := applyReasoningEffortTool(&current, &changes, json.RawMessage(`{"effort":"low"}`))
	if !ok || current != "low" || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("valid effort not applied: current=%s changes=%d out=%s ok=%v", current, changes, out, ok)
	}
	out, ok = applyReasoningEffortTool(&current, &changes, json.RawMessage(`{"effort":"warp"}`))
	if ok || current != "low" || !strings.Contains(out, "none, minimal, low, medium, high, xhigh") {
		t.Fatalf("invalid effort should be rejected: current=%s out=%s ok=%v", current, out, ok)
	}
	changes = maxEffortChangesPerRun
	out, ok = applyReasoningEffortTool(&current, &changes, json.RawMessage(`{"effort":"high"}`))
	if ok || current != "low" || !strings.Contains(out, "cap reached") {
		t.Fatalf("cap should block effort changes: current=%s out=%s ok=%v", current, out, ok)
	}
}

func TestConfiguredReasoningEffortOverridesModeDefault(t *testing.T) {
	cfg := settings.DefaultSettings()
	mode := AgentMode{Name: "Ops", DefaultEffort: "high"}
	if got := configuredReasoningEffort(cfg, mode); got != "high" {
		t.Fatalf("auto effort = %q, want mode default high", got)
	}
	cfg.Agents.ReasoningEffort = "low"
	if got := configuredReasoningEffort(cfg, mode); got != "low" {
		t.Fatalf("configured effort = %q, want low", got)
	}
	cfg.Agents.ReasoningEffort = "warp"
	if got := configuredReasoningEffort(cfg, mode); got != "high" {
		t.Fatalf("invalid configured effort = %q, want fallback high", got)
	}
}

func TestReasoningEffortToolDefExposedThroughToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	defs, choice := toolDefsAndChoiceForTurn(tools.New(), cfg, "implement the next feature", 0, 0)
	if choice == "none" {
		t.Fatal("expected tool-enabled task turn")
	}
	for _, def := range defs {
		if def.Function.Name == reasoningEffortToolName {
			return
		}
	}
	t.Fatalf("missing %s tool def in %#v", reasoningEffortToolName, defs)
}
