package app

import (
	"regexp"
	"strings"

	"mauler/internal/ledger"
)

const maxRunFactsPromptItems = 10

type runFact struct {
	Kind   string
	Text   string
	Source string
}

var (
	runFactTargetIPRe   = regexp.MustCompile(`(?i)\b(?:target(?:\s+ip)?\s*[:=]\s*)?([0-9]{1,3}(?:\.[0-9]{1,3}){3})\b`)
	runFactHostnameRe   = regexp.MustCompile(`(?i)\b(?:hostname|host)\s*[:=]\s*([a-z0-9][a-z0-9_.-]+)`)
	runFactWebshellRe   = regexp.MustCompile(`(?i)https?://[^\s"'<>]+(?:shell|cmd|webshell|\.php)[^\s"'<>]*`)
	runFactUIDRe        = regexp.MustCompile(`(?i)\buid=\d+\([^)]+\)\s+gid=\d+\([^)]+\)[^\r\n]*`)
	runFactCredRe       = regexp.MustCompile(`(?i)\b(?:user|username|password|passwd|hash|token|apikey|api_key|secret)\s*[:=]\s*[^\s"'<>]{3,}`)
	runFactArtifactRe   = regexp.MustCompile(`(?i)\b(?:artifact|saved|wrote|output)\s*[:=]\s*([A-Za-z]:[\\/][^\r\n]+|(?:\.?/|/)[^\r\n]+)`)
	runFactFailedPathRe = regexp.MustCompile(`(?i)\b(?:failed|not found|no such file|exit code|blocked|denied)\b[^\r\n]*`)
)

func buildRunFactsPromptFromLedger() string {
	l, err := ledger.NewDefault()
	if err != nil {
		return ""
	}
	events, err := l.List(300)
	if err != nil {
		return ""
	}
	return buildRunFactsPrompt(events)
}

func buildRunFactsPrompt(events []ledger.Event) string {
	facts := deriveRunFacts(events, maxRunFactsPromptItems)
	if len(facts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nCurrent run facts from ledger evidence (verify stale/conflicting facts before acting):\n")
	for _, fact := range facts {
		sb.WriteString("- ")
		if fact.Kind != "" {
			sb.WriteString(fact.Kind + ": ")
		}
		sb.WriteString(truncateRunes(fact.Text, 220))
		if fact.Source != "" {
			sb.WriteString(" [source=" + fact.Source + "]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func deriveRunFacts(events []ledger.Event, limit int) []runFact {
	if limit <= 0 {
		limit = maxRunFactsPromptItems
	}
	seen := map[string]bool{}
	var facts []runFact
	add := func(kind, text, source string) {
		text = strings.TrimSpace(truncateLine(text, 240))
		if text == "" || len(facts) >= limit {
			return
		}
		key := strings.ToLower(kind + ":" + text)
		if seen[key] {
			return
		}
		seen[key] = true
		facts = append(facts, runFact{Kind: kind, Text: text, Source: source})
	}
	for _, event := range events {
		if len(facts) >= limit {
			break
		}
		source := strings.TrimSpace(event.Tool)
		if source == "" {
			source = strings.TrimSpace(event.Source)
		}
		if strings.EqualFold(event.Kind, "evidence_pin") {
			for _, line := range strings.Split(event.Detail, "\n") {
				classifyRunFactLine(line, source, add)
				if len(facts) >= limit {
					break
				}
			}
			continue
		}
		for _, artifact := range event.Artifacts {
			add("artifact", artifact, source)
		}
		for _, file := range event.Files {
			add("file", file, source)
		}
	}
	return facts
}

func currentTargetIP(events []ledger.Event) string {
	for _, fact := range deriveRunFacts(events, maxRunFactsPromptItems) {
		if fact.Kind != "target" {
			continue
		}
		if ip := firstIPv4(fact.Text); ip != "" {
			return ip
		}
	}
	return ""
}

func authoritativeTargetIPForRun(run TaskRun, configuredTarget string) string {
	if ip := labelledTargetIP(run.Prompt); ip != "" {
		return ip
	}
	if ip := currentRunFactTargetIP(run); ip != "" {
		return ip
	}
	if ip := firstIPv4(configuredTarget); ip != "" {
		return ip
	}
	if ip := firstIPv4(run.Prompt); ip != "" {
		return ip
	}
	return ""
}

func labelledTargetIP(text string) string {
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "target") {
			continue
		}
		if ip := firstIPv4(line); ip != "" {
			return ip
		}
	}
	return ""
}

func currentRunFactTargetIP(run TaskRun) string {
	for _, event := range run.Events {
		if ip := labelledTargetIP(event.Message); ip != "" {
			return ip
		}
		if ip := labelledTargetIP(event.Detail); ip != "" {
			return ip
		}
		if strings.EqualFold(event.Kind, "evidence_pin") {
			if ip := firstTargetFactIP(event.Detail); ip != "" {
				return ip
			}
		}
	}
	for _, tool := range run.Tools {
		if strings.EqualFold(tool.Name, "progress") || strings.EqualFold(tool.Name, "memory") || strings.EqualFold(tool.Name, "evidence_bundle") {
			if ip := firstTargetFactIP(tool.Result); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func firstTargetFactIP(text string) string {
	for _, line := range strings.Split(text, "\n") {
		var found string
		classifyRunFactLine(line, "", func(kind, text, source string) {
			if found == "" && kind == "target" {
				found = firstIPv4(text)
			}
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func classifyRunFactLine(line, source string, add func(kind, text, source string)) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if m := runFactWebshellRe.FindString(line); m != "" {
		add("webshell", m, source)
		return
	}
	if m := runFactUIDRe.FindString(line); m != "" {
		add("shell-user", m, source)
		return
	}
	if m := runFactHostnameRe.FindStringSubmatch(line); len(m) == 2 {
		add("hostname", m[1], source)
		return
	}
	if m := runFactArtifactRe.FindStringSubmatch(line); len(m) == 2 {
		add("artifact", m[1], source)
		return
	}
	if m := runFactCredRe.FindString(line); m != "" {
		add("credential", m, source)
		return
	}
	if m := runFactFailedPathRe.FindString(line); m != "" {
		add("failed-attempt", m, source)
		return
	}
	if m := runFactTargetIPRe.FindStringSubmatch(line); len(m) == 2 {
		add("target", m[1], source)
		return
	}
	add("evidence", line, source)
}
