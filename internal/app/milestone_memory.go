package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	ipv4RE           = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	flagLikeRE       = regexp.MustCompile(`(?i)\b((?:user|root)\.txt)\s*[:=]?\s*([a-f0-9]{16,}|[A-Za-z0-9+/=]{20,})`)
	credentialLineRE = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|api[_-]?key|private[_-]?key)\b\s*[:=]\s*\S+`)
)

func (a *App) saveRunMilestoneMemory(run *TaskRun) error {
	if run == nil || strings.TrimSpace(run.Prompt) == "" {
		return nil
	}
	mem := buildRunMilestoneMemory(run)
	if mem == nil {
		return nil
	}
	_, err := a.SaveMemoryEntry(*mem)
	return err
}

func buildRunMilestoneMemory(run *TaskRun) *MemoryEntry {
	target := detectRunTarget(run)
	milestones := deriveRunMilestones(run)
	if len(milestones) == 0 && run.Status == "done" {
		milestones = append(milestones, "Run completed.")
	}
	if len(milestones) == 0 && run.StopReason == "" {
		return nil
	}

	lines := []string{
		"Prompt: " + truncateLine(run.Prompt, 220),
		"Status: " + run.Status,
	}
	if run.StopReason != "" {
		lines = append(lines, "Stop: "+run.StopReason)
	}
	if target != "" {
		lines = append(lines, "Target: "+target)
	}
	if len(milestones) > 0 {
		lines = append(lines, "Milestones:")
		for _, milestone := range milestones {
			lines = append(lines, "- "+milestone)
		}
	}
	if next := deriveNextAction(run); next != "" {
		lines = append(lines, "Next: "+next)
	}

	title := "Run memory"
	if target != "" {
		title = "Run memory: " + target
	}
	return &MemoryEntry{
		ID:         "runmem-" + run.ID,
		Title:      title,
		Content:    sanitizeMilestoneMemory(strings.Join(lines, "\n")),
		Kind:       "fact",
		Importance: milestoneImportance(run, milestones),
		Tags:       milestoneTags(run, target),
	}
}

func deriveRunMilestones(run *TaskRun) []string {
	var out []string
	for _, tool := range run.Tools {
		lowerName := strings.ToLower(tool.Name)
		text := tool.Input + "\n" + tool.Result
		lower := strings.ToLower(text)
		switch {
		case lowerName == "shell" && strings.Contains(lower, "nmap") && strings.Contains(lower, "open"):
			if ports := extractOpenPorts(tool.Result); ports != "" {
				out = append(out, "Recon found open ports/services: "+ports+".")
			} else {
				out = append(out, "Recon command ran and produced service output.")
			}
		case lowerName == "web_search":
			if query := jsonField(tool.Input, "query"); query != "" {
				out = append(out, "Web research query: "+truncateLine(query, 140)+".")
			}
		case lowerName == "fetch_url":
			if url := jsonField(tool.Input, "url"); url != "" {
				out = append(out, "Fetched source: "+truncateLine(url, 160)+".")
			}
		case lowerName == "write_file" || lowerName == "edit_file":
			if path := firstNonEmpty(jsonField(tool.Input, "path"), jsonField(tool.Input, "file")); path != "" {
				out = append(out, "Updated file: "+path+".")
			} else {
				out = append(out, "Updated a workspace file.")
			}
		case strings.Contains(lower, "user.txt"):
			out = append(out, "User flag path/output was encountered; verify and document without storing the flag value in memory.")
		case strings.Contains(lower, "root.txt"):
			out = append(out, "Root flag path/output was encountered; verify and document without storing the flag value in memory.")
		}
	}
	return uniqueStrings(out, 8)
}

func deriveNextAction(run *TaskRun) string {
	if run.StopReason != "" {
		switch {
		case strings.Contains(run.StopReason, "search_budget"):
			return "Resume with expanded exploit research budget or narrow the query to the most promising FreePBX/CVE lead."
		case strings.Contains(run.StopReason, "tool") || strings.Contains(run.StopReason, "shell"):
			return "Inspect the failed tool result, adjust command syntax/timeouts, then continue from the latest successful milestone."
		default:
			if run.StopDetail != "" {
				return truncateLine(run.StopDetail, 220)
			}
		}
	}
	for _, tool := range run.Tools {
		if tool.Status == "error" || tool.Status == "blocked" || tool.Status == "disabled" {
			return fmt.Sprintf("Review %s result and continue from the last successful tool.", tool.Name)
		}
	}
	return ""
}

func detectRunTarget(run *TaskRun) string {
	if ip := ipv4RE.FindString(run.Prompt); ip != "" {
		return ip
	}
	for _, tool := range run.Tools {
		if ip := ipv4RE.FindString(tool.Input + "\n" + tool.Result); ip != "" {
			return ip
		}
	}
	return ""
}

func extractOpenPorts(text string) string {
	var ports []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "/tcp") || !strings.Contains(strings.ToLower(line), "open") {
			continue
		}
		ports = append(ports, truncateLine(line, 100))
		if len(ports) >= 6 {
			break
		}
	}
	return strings.Join(ports, "; ")
}

func milestoneImportance(run *TaskRun, milestones []string) int {
	if run.Status == "done" {
		return 4
	}
	if len(milestones) >= 3 {
		return 4
	}
	return 3
}

func milestoneTags(run *TaskRun, target string) []string {
	tags := []string{"run", "milestone"}
	prompt := strings.ToLower(run.Prompt)
	if target != "" {
		tags = append(tags, "target-"+strings.ReplaceAll(target, ".", "-"))
	}
	if strings.Contains(prompt, "htb") || strings.Contains(prompt, "hackthebox") || strings.Contains(prompt, "flag") {
		tags = append(tags, "htb")
	}
	if strings.Contains(prompt, "readme") || strings.Contains(prompt, "writeup") || strings.Contains(prompt, "doc") {
		tags = append(tags, "docs")
	}
	return tags
}

func sanitizeMilestoneMemory(text string) string {
	text = flagLikeRE.ReplaceAllString(text, "$1=[redacted]")
	text = credentialLineRE.ReplaceAllString(text, "$1=[redacted]")
	return text
}

func jsonField(raw, key string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string, limit int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, min(len(values), limit))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func truncateLine(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if limit > 0 && len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}
