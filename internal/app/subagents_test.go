package app

import (
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestTaskToolExposesExpectedSubagentTypes(t *testing.T) {
	specs := subagentSpecs()
	got := map[string]bool{}
	for _, spec := range specs {
		got[subagentTypeName(spec.ToolName)] = true
		if spec.TimeoutSecs <= 0 || spec.MaxTurns <= 0 || spec.MaxOutput <= 0 || spec.ContextBudget <= 0 {
			t.Fatalf("subagent spec has invalid bounds: %#v", spec)
		}
		if strings.TrimSpace(spec.Toolset) == "" || strings.TrimSpace(spec.Contract) == "" {
			t.Fatalf("subagent spec missing toolset/contract: %#v", spec)
		}
	}
	for _, name := range []string{"explore", "research", "review", "testfix", "summarize"} {
		if !got[name] {
			t.Fatalf("missing task type %q in %#v", name, got)
		}
	}
}

func TestSubagentExploreReadOnlyToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "explore"
	effective := settings.EffectiveEnabledTools(cfg)

	for _, name := range []string{"read", "glob", "grep"} {
		if !effective[name] {
			t.Fatalf("explore toolset should include %s: %#v", name, effective)
		}
	}
	for _, name := range []string{"write", "edit", "shell", "web_search", "fetch_url"} {
		if effective[name] {
			t.Fatalf("explore toolset should exclude %s: %#v", name, effective)
		}
	}
}

func TestBuildSubagentSystemPromptIncludesBoundsAndWorkspace(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_research")
	profile := settings.Profile{Name: "qwen-test", CtxTokens: 32768}
	prompt := buildSubagentSystemPrompt(spec, profile, 30, 2)

	for _, want := range []string{
		"bounded Researcher subagent",
		"Profile: qwen-test",
		"Toolset: web-research",
		"Timeout: 30s",
		"Tool-call budget: 2",
		"Current workspace context",
		spec.Contract,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSubagentFinalReportIncludesMetadata(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_review")
	out := finalSubagentReport(spec, "found issue", nil, 3, "turn budget exhausted")

	for _, want := range []string{"Subagent: Reviewer", "Toolset: safe", "Tool calls used: 3", "Stop: turn budget exhausted", "found issue"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}

func TestSubagentFinalReportFallsBackToEvidence(t *testing.T) {
	spec := mustSubagentSpec(t, "subagent_research")
	out := finalSubagentReport(spec, "", []string{
		`web_search: No results found for "FreePBX 16.0.40.7 exploit"`,
		`fetch_url: blocked by timeout`,
	}, 4, "turn budget exhausted")

	for _, want := range []string{
		"Subagent: Researcher",
		"Stop: turn budget exhausted",
		"The subagent stopped before writing a synthesis",
		`web_search: No results found for "FreePBX 16.0.40.7 exploit"`,
		"fetch_url: blocked by timeout",
		"Recommended next step",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fallback report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "No subagent output was produced") {
		t.Fatalf("fallback report should not return the old blank-output message:\n%s", out)
	}
}

func mustSubagentSpec(t *testing.T, toolName string) subagentSpec {
	t.Helper()
	for _, spec := range subagentSpecs() {
		if spec.ToolName == toolName {
			return spec
		}
	}
	t.Fatalf("missing subagent spec %q", toolName)
	return subagentSpec{}
}
