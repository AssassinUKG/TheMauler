package app

import "strings"

// canHandOffRecoveredAnswer keeps the control-plane stop in the audit trail but
// lets channels present a useful, evidence-backed read-only answer as delivered.
// It deliberately excludes mutation/execution tasks and empty/tool-shaped prose.
func canHandOffRecoveredAnswer(run TaskRun, summary string) bool {
	return readOnlyRecoveryEvidenceReady(run) && answerCheckpointCandidate(summary, nil, false)
}

func readOnlyRecoveryEvidenceReady(run TaskRun) bool {
	if strings.TrimSpace(run.StopReason) != "loop_circuit_breaker" || !promptLooksReadOnly(run.Prompt) || runHasFileMutation(run) {
		return false
	}
	for _, tool := range run.Tools {
		status := strings.ToLower(strings.TrimSpace(tool.Status))
		if status != "done" && status != "cached" && status != "routed" {
			continue
		}
		if strings.TrimSpace(tool.Result) == "" || !recoveryEvidenceTool(tool.Name) {
			continue
		}
		return true
	}
	return false
}

func recoveryEvidenceTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "read_pdf", "read_tool_result", "glob", "grep", "session_search", "sqlite",
		"web_search", "fetch_url", "http_probe", "browser", "shell", "terminal_read":
		return true
	default:
		return false
	}
}
