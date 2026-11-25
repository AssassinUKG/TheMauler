package app

import (
	"strings"
	"testing"
)

func routerAssessmentRunFixture() TaskRun {
	return TaskRun{
		Prompt:     "Assess my own router for exposed services and vulnerabilities",
		StopReason: "loop_circuit_breaker",
		Tools: []TaskToolEvent{{
			Name:   "shell",
			Status: "done",
			Input:  `{"command":"nmap --top-ports 1000 -sV 192.0.2.1"}`,
			Result: "Not shown: 996 closed tcp ports\n53/tcp open domain\n80/tcp open http\n443/tcp open https\n1900/tcp open upnp\n# C:/work/router_nmap.txt",
		}},
	}
}

func TestNetworkEvidenceDetectsContradictoryFinalReport(t *testing.T) {
	run := routerAssessmentRunFixture()
	conflict := networkEvidenceSummaryConflict(run, "Only port 80 was open and there was no HTTPS service.")
	if !strings.Contains(conflict, "only port 80") || !strings.Contains(conflict, "443/tcp") {
		t.Fatalf("unexpected conflict: %q", conflict)
	}
	handoff := buildNetworkAssessmentEvidenceHandoff(run, conflict)
	for _, want := range []string{"53/tcp", "443/tcp", "1900/tcp", "top-1000", "No authenticated session", "replaced the generated report"} {
		if !strings.Contains(handoff, want) {
			t.Fatalf("handoff missing %q:\n%s", want, handoff)
		}
	}
}

func TestNetworkEvidenceRejectsUnsupportedFullScanClaim(t *testing.T) {
	run := routerAssessmentRunFixture()
	if conflict := networkEvidenceSummaryConflict(run, "The full TCP scan of all 65535 ports completed."); !strings.Contains(conflict, "full TCP coverage") {
		t.Fatalf("full-scan conflict = %q", conflict)
	}
}
