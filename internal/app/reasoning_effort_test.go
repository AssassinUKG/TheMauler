package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

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
		{"bad", true, 0, false},
	}
	for _, tc := range cases {
		got := effortToThinking(tc.effort, profile)
		if got.enableThinking != tc.wantThinking || got.maxTokensCap != tc.wantCap || got.coding != tc.wantCoding {
			t.Fatalf("%s => %#v, want thinking=%v cap=%d coding=%v", tc.effort, got, tc.wantThinking, tc.wantCap, tc.wantCoding)
		}
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
	if req.MaxTokens != 1024 || req.Temperature != 0.5 || req.ReasoningEffort != "minimal" {
		t.Fatalf("minimal effort should use capped coding params: %#v", req)
	}

	req = buildChatRequest(profile, nil, nil, "", true, false, "high")
	if req.EnableThinking || req.PreserveThinking {
		t.Fatalf("forceNoThink must override high effort: %#v", req)
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
	if ok || current != "low" || !strings.Contains(out, "minimal, low, medium, high") {
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
