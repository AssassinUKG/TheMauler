package app

import (
	"context"
	"strings"
	"testing"

	"mauler/internal/ledger"
)

func TestBuildLearningCandidatesFindsReflectionsAndSkills(t *testing.T) {
	events := []ledger.Event{
		{
			ID:      "evt-skill",
			RunID:   "run-1",
			Kind:    "learning_suggestion",
			Source:  "skills",
			Status:  "skill",
			Message: "Save run as skill?",
			Detail:  "Reusable workflow",
			Output:  "---\nname: test\n---\n",
		},
		{
			ID:     "evt-model",
			RunID:  "run-1",
			Kind:   "model_load",
			Source: "provider",
			Status: "retry",
			Error:  "bridge starting",
		},
		{
			ID:     "evt-tool",
			RunID:  "run-1",
			Kind:   "tool_result",
			Source: "tool",
			Tool:   "shell",
			Status: "blocked",
			Input:  `{"command":"nmap"}`,
			Output: "timed out",
		},
	}

	candidates := buildLearningCandidates(events)
	if len(candidates) < 3 {
		t.Fatalf("got %d candidates, want at least 3: %#v", len(candidates), candidates)
	}
	var sawSkill, sawModelReflection, sawToolReflection bool
	for _, candidate := range candidates {
		if candidate.Type == "skill" && candidate.Template != "" {
			sawSkill = true
		}
		if candidate.Type == "reflection" && candidate.Title == "Model load retry" {
			sawModelReflection = true
		}
		if candidate.Type == "reflection" && candidate.Title == "shell blocked" {
			sawToolReflection = true
		}
		if candidate.Content == "" || candidate.Title == "" {
			t.Fatalf("candidate missing title/content: %#v", candidate)
		}
	}
	if !sawSkill || !sawModelReflection || !sawToolReflection {
		t.Fatalf("missing expected candidates: %#v", candidates)
	}
}

func TestBuildLearningCandidatesSanitizesSecrets(t *testing.T) {
	candidates := buildLearningCandidates([]ledger.Event{
		{
			ID:     "evt-secret",
			Kind:   "tool_result",
			Tool:   "shell",
			Status: "error",
			Error:  "password=hunter2",
		},
	})
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].Content == "" || candidates[0].Content == "password=hunter2" {
		t.Fatalf("candidate was not summarized/sanitized: %#v", candidates[0])
	}
	if strings.Contains(candidates[0].Content, "hunter2") {
		t.Fatalf("secret leaked into candidate: %#v", candidates[0])
	}
}

func TestLearningDecisionFiltersHandledCandidates(t *testing.T) {
	for _, decision := range []string{"approved", "rejected", "deferred"} {
		t.Run(decision, func(t *testing.T) {
			t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
			app := New()
			t.Cleanup(func() { app.OnShutdown(context.Background()) })
			app.recordLedger(ledger.Event{
				ID:      "evt-learning-filter-" + decision,
				Kind:    "learning_suggestion",
				Source:  "skills",
				Status:  "skill",
				Message: "Save filtered skill?",
				Detail:  "Reusable workflow",
				Output:  "steps",
			})

			candidates, err := app.ListLearningCandidates(100)
			if err != nil {
				t.Fatalf("list candidates: %v", err)
			}
			if len(candidates) == 0 {
				t.Fatal("expected at least one learning candidate")
			}
			candidate := candidates[0]
			if err := app.RecordLearningDecision(candidate, decision, "decision in test"); err != nil {
				t.Fatalf("record decision: %v", err)
			}
			candidates, err = app.ListLearningCandidates(100)
			if err != nil {
				t.Fatalf("list after decision: %v", err)
			}
			for _, next := range candidates {
				if next.ID == candidate.ID {
					t.Fatalf("%s candidate should be filtered out: %#v", decision, candidates)
				}
			}
		})
	}
}
