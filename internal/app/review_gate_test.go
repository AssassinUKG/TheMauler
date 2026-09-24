package app

import (
	"strings"
	"testing"
)

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
		{
			name: "HTTP method inventory is not a delete or patch request",
			run:  TaskRun{Prompt: "how many POST, PUT, PATCH, DELETE endpoints exist and what is 20% coverage?", Mode: "Auto"},
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

func TestAPIInventoryStillRecognisesAnExplicitFollowUpMutation(t *testing.T) {
	prompt := "Count the GET, POST, PATCH, and DELETE endpoints, then delete the obsolete generated file."
	if promptLooksReadOnly(prompt) || !promptExplicitlyRequestsMutation(strings.ToLower(prompt)) {
		t.Fatalf("explicit mutation hidden by HTTP method inventory: %q", prompt)
	}
}

func TestScopedWithoutChangingClauseDoesNotHideRequestedFix(t *testing.T) {
	prompt := "Fix the cloud context defaults for OpenRouter without changing the normal local inference path."
	if promptLooksReadOnly(prompt) {
		t.Fatalf("scoped protection clause hid the requested fix: %q", prompt)
	}
}

func TestAnswerOutputLanguageDoesNotImplyWorkspaceMutation(t *testing.T) {
	for _, prompt := range []string{
		"Create a table of the attached API endpoints",
		"Generate a report and show me the method counts",
		"Create a plan for the next review",
		"Tell me what this file does",
	} {
		if !promptLooksReadOnly(prompt) || promptExplicitlyRequestsMutation(strings.ToLower(prompt)) {
			t.Fatalf("answer output routed as mutation: %q", prompt)
		}
	}
}

func TestAnswerAndExplicitWorkspaceChangeStillMutates(t *testing.T) {
	for _, prompt := range []string{
		"Create a table, then update the README",
		"Show me the problem and fix the parser",
		"Generate a report and save it to a file",
	} {
		if promptLooksReadOnly(prompt) || !promptExplicitlyRequestsMutation(strings.ToLower(prompt)) {
			t.Fatalf("explicit mixed mutation was hidden: %q", prompt)
		}
	}
}

func TestArtifactPlacementLanguageRequestsWorkspaceMutation(t *testing.T) {
	for _, prompt := range []string{
		"Can you go online and get me the PoC? Leave it in the directory.",
		"Try to write the PoC now in the directory; you should be able to now.",
		"Fetch the public checker and put it in this folder.",
		"Find the script and save it in the workspace.",
	} {
		if !promptExplicitlyRequestsMutation(strings.ToLower(prompt)) || promptLooksReadOnly(prompt) {
			t.Fatalf("artifact placement was not classified as mutation: %q", prompt)
		}
	}
}

func TestArtifactChatOnlyLanguageStaysReadOnly(t *testing.T) {
	for _, prompt := range []string{
		"Show me the PoC in chat; do not save it.",
		"Paste the script in the chat so I can review it.",
	} {
		if promptExplicitlyRequestsMutation(strings.ToLower(prompt)) {
			t.Fatalf("chat-only artifact request was classified as workspace mutation: %q", prompt)
		}
	}
}

func TestCommandOutputLanguageDoesNotAuthoriseExecution(t *testing.T) {
	for _, prompt := range []string{
		"Can you give me the curl command for this PoC and use this callback URL callback.example?",
		"Show me a PowerShell one-liner for the check.",
		"Provide the HTTP request in chat so I can review it.",
	} {
		if !promptClearlyRequestsCommandOutput(prompt) || !promptLooksReadOnly(prompt) {
			t.Fatalf("command composition did not stay read-only: %q", prompt)
		}
	}
}

func TestExplicitCommandExecutionDoesNotUseAnswerOnlyRoute(t *testing.T) {
	for _, prompt := range []string{
		"Run this curl command against the authorised target.",
		"Can you run the curl command against the authorised target?",
		"Give me the curl command and then run it.",
		"Create the HTTP request, validate it, and report the response.",
		"Write the curl command into a workspace file.",
	} {
		if promptClearlyRequestsCommandOutput(prompt) {
			t.Fatalf("explicit execution was classified as command composition: %q", prompt)
		}
	}
}
