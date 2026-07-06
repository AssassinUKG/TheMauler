package app

import (
	"regexp"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/llm"
)

var evidencePinPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\btarget(?:\s+ip)?\s*[:=]\s*([0-9]{1,3}(?:\.[0-9]{1,3}){3})`),
	regexp.MustCompile(`(?i)\b(?:https?://)[^\s"'<>]+(?:shell|cmd|webshell|\.php)[^\s"'<>]*`),
	regexp.MustCompile(`(?i)\buid=\d+\([^)]+\)\s+gid=\d+\([^)]+\)[^\r\n]*`),
	regexp.MustCompile(`(?i)\bCVE-\d{4}-\d{4,7}\b`),
	regexp.MustCompile(`(?i)\b(?:user|username|password|passwd|hash|token|apikey|api_key|secret)\s*[:=]\s*[^\s"'<>]{3,}`),
	regexp.MustCompile(`(?i)\b(?:user|root)\.txt\b[^\r\n]*`),
	regexp.MustCompile(`(?i)\b(?:artifact|saved|wrote|output)\s*[:=]\s*[A-Za-z]:[\\/][^\r\n]+`),
	regexp.MustCompile(`(?i)\b(?:artifact|saved|wrote|output)\s*[:=]\s*(?:\.?/|/)[^\r\n]+`),
}

func (a *App) recordPinnedEvidenceLedger(runID string, tc llm.ToolCallDef, result string) {
	if shouldSkipEvidencePin(tc, result) {
		return
	}
	pins := extractEvidencePins(result, 6)
	if len(pins) == 0 {
		return
	}
	a.recordLedger(ledger.Event{
		RunID:   runID,
		Kind:    "evidence_pin",
		Source:  "tool_result",
		Tool:    tc.Function.Name,
		Status:  "candidate",
		Message: "Pinned compact evidence from tool output",
		Detail:  strings.Join(pins, "\n"),
		Metadata: map[string]string{
			"review_required": "true",
			"promotion":       "manual",
		},
	})
}

func shouldSkipEvidencePin(tc llm.ToolCallDef, result string) bool {
	lower := strings.ToLower(result)
	if strings.Contains(lower, "[tool_state_machine ") {
		return true
	}
	if strings.Contains(lower, "allowed=false") || strings.Contains(lower, "do_not_repeat: do not retry the same blocked tool call") {
		return true
	}
	if strings.Contains(lower, "[verifier_required:") && strings.Contains(lower, "before reporting") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(tc.Function.Name))
	if name == "terminal_send" && strings.Contains(lower, "recommended=terminal_read") {
		return true
	}
	return false
}

func extractEvidencePins(text string, limit int) []string {
	if limit <= 0 {
		limit = 6
	}
	seen := map[string]bool{}
	var out []string
	for _, re := range evidencePinPatterns {
		for _, match := range re.FindAllString(text, -1) {
			pin := strings.TrimSpace(truncateLine(match, 240))
			if pin == "" {
				continue
			}
			key := strings.ToLower(pin)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, pin)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}
