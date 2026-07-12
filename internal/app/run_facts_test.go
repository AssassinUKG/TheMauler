package app

import (
	"strings"
	"testing"

	"mauler/internal/ledger"
)

func TestBuildRunFactsPromptFromEvidencePins(t *testing.T) {
	events := []ledger.Event{
		{
			Kind:   "evidence_pin",
			Source: "tool_result",
			Tool:   "shell",
			Detail: strings.Join([]string{
				"Target IP: 10.129.23.158",
				"https://connected.htb/ekx2wj1wzq/lpazncch.php?cmd=id",
				"uid=999(asterisk) gid=1000(asterisk) groups=1000(asterisk)",
				"password: [REDACTED]",
				"error: exit code 7",
			}, "\n"),
		},
		{Kind: "artifact", Source: "http_probe", Artifacts: []string{".mauler_artifacts/http_probe/target.txt"}},
	}

	prompt := buildRunFactsPrompt(events)
	for _, want := range []string{
		"Current run facts",
		"target: 10.129.23.158",
		"webshell: https://connected.htb/ekx2wj1wzq/lpazncch.php?cmd=id",
		"shell-user: uid=999(asterisk)",
		"credential: password: [REDACTED]",
		"artifact: .mauler_artifacts/http_probe/target.txt",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("facts prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestDeriveRunFactsCapsAndDedupes(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "Target IP: 10.10.10.10")
		lines = append(lines, "artifact: /tmp/artifact-"+string(rune('a'+i))+".txt")
	}
	facts := deriveRunFacts([]ledger.Event{{Kind: "evidence_pin", Detail: strings.Join(lines, "\n")}}, 4)
	if len(facts) != 4 {
		t.Fatalf("facts len=%d, want cap 4: %#v", len(facts), facts)
	}
	targets := 0
	for _, fact := range facts {
		if fact.Kind == "target" {
			targets++
		}
	}
	if targets != 1 {
		t.Fatalf("expected one deduped target, got %d facts: %#v", targets, facts)
	}
}

func TestAuthoritativeTargetIPForRunPrefersCurrentPrompt(t *testing.T) {
	run := TaskRun{
		Prompt: "Target: 10.129.26.26\nContinue the box.",
		Events: []TaskRunEvent{{
			Kind:   "evidence_pin",
			Detail: "Target IP: 10.129.14.129",
		}},
	}
	if got := authoritativeTargetIPForRun(run, "10.129.23.41"); got != "10.129.26.26" {
		t.Fatalf("authoritative target = %q, want current prompt target", got)
	}
}

func TestAuthoritativeTargetIPForRunFallsBackToRunFactsThenSettings(t *testing.T) {
	run := TaskRun{Events: []TaskRunEvent{{
		Kind:   "evidence_pin",
		Detail: "confirmed target: 10.129.26.26",
	}}}
	if got := authoritativeTargetIPForRun(run, "10.129.23.41"); got != "10.129.26.26" {
		t.Fatalf("authoritative target = %q, want current run fact", got)
	}
	if got := authoritativeTargetIPForRun(TaskRun{}, "target 10.129.23.41"); got != "10.129.23.41" {
		t.Fatalf("authoritative target fallback = %q, want configured target", got)
	}
}
