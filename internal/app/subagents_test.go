package app

import (
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestSubagentSpecsExposeExpectedTools(t *testing.T) {
	specs := subagentSpecs()
	got := map[string]bool{}
	for _, spec := range specs {
		got[spec.ToolName] = true
		if spec.TimeoutSecs <= 0 || spec.MaxTurns <= 0 || spec.MaxOutput <= 0 || spec.ContextBudget <= 0 {
			t.Fatalf("subagent spec has invalid bounds: %#v", spec)
		}
		if strings.TrimSpace(spec.Toolset) == "" || strings.TrimSpace(spec.Contract) == "" {
			t.Fatalf("subagent spec missing toolset/contract: %#v", spec)
		}
	}
	for _, name := range []string{"subagent_research", "subagent_review", "subagent_testfix", "subagent_summarize"} {
		if !got[name] {
			t.Fatalf("missing subagent tool %q in %#v", name, got)
		}
	}
}

func TestBuildSubagentSystemPromptIncludesBoundsAndWorkspace(t *testing.T) {
	spec := subagentSpecs()[0]
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
	spec := subagentSpecs()[1]
	out := finalSubagentReport(spec, "found issue", 3, "turn budget exhausted")

	for _, want := range []string{"Subagent: Reviewer", "Toolset: safe", "Tool calls used: 3", "Stop: turn budget exhausted", "found issue"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}
