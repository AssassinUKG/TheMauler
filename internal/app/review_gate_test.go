package app

import "testing"

func TestRunIsGateableMutation(t *testing.T) {
	run := TaskRun{
		Prompt: "fix the bug",
		Mode:   "Builder",
		Tools: []TaskToolEvent{{
			Name:   "edit",
			Status: "done",
		}},
	}

	if !runIsGateable(run, AgentMode{Name: "Builder"}) {
		t.Fatal("builder run with a file mutation should be gateable")
	}
}

func TestRunIsGateableDeliverablePrompt(t *testing.T) {
	run := TaskRun{
		Prompt: "add a min helper and update the tests",
		Mode:   "Auto",
	}

	if !runIsGateable(run, AgentMode{Name: "Auto"}) {
		t.Fatal("deliverable-style auto run should be gateable")
	}
}

func TestRunIsGateableSkipsResearch(t *testing.T) {
	tests := []struct {
		name string
		run  TaskRun
		mode AgentMode
	}{
		{
			name: "researcher mode",
			run:  TaskRun{Prompt: "research the best approach and summarize findings", Mode: "Researcher"},
			mode: AgentMode{Name: "Researcher"},
		},
		{
			name: "read only auto prompt",
			run:  TaskRun{Prompt: "map this repo and explain the architecture", Mode: "Auto"},
			mode: AgentMode{Name: "Auto"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if runIsGateable(tc.run, tc.mode) {
				t.Fatalf("read-only/research run should not be gateable: %#v", tc.run)
			}
		})
	}
}
