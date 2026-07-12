package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"mauler/internal/settings"
)

var completionStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true,
	"by": true, "for": true, "from": true, "in": true, "into": true, "it": true,
	"of": true, "on": true, "or": true, "please": true, "the": true, "this": true,
	"to": true, "with": true, "you": true, "your": true, "only": true, "after": true,
	"before": true, "then": true, "now": true, "current": true, "existing": true,
}

var completionActionWords = map[string]bool{
	"add": true, "build": true, "create": true, "fix": true, "generate": true,
	"implement": true, "make": true, "patch": true, "refactor": true, "update": true,
	"write": true, "wire": true,
}

var shortGoalFeatures = map[string]bool{
	"api": true, "csv": true, "id": true, "max": true, "min": true, "ui": true, "ux": true,
}

// runCompletionRails checks goal coverage and deliverable existence. By default
// it records advisory verdicts; cfg controls whether failures block completion.
func runCompletionRails(run *TaskRun, cfg *settings.Settings) []VerifyVerdict {
	if run == nil || cfg == nil {
		return nil
	}
	blocking := cfg.Agents.ReviewLoop.CompletionBlocking
	var verdicts []VerifyVerdict
	features := extractGoalFeatures(run.Prompt)
	if len(features) > 0 {
		coverage := goalFeatureCoverage(*run, features)
		missing := uncoveredGoalFeatures(coverage, features)
		status := "pass"
		summary := "Completion rail passed: requested features appear covered."
		var improvements []string
		if len(missing) > 0 {
			status = "fail"
			summary = fmt.Sprintf("Completion rail found %d uncovered requested feature(s).", len(missing))
			for _, feature := range missing {
				improvements = append(improvements, fmt.Sprintf("Goal feature %q was not clearly addressed.", feature))
			}
		}
		verdicts = append(verdicts, VerifyVerdict{
			Gate:         "completion",
			Status:       status,
			Blocking:     blocking && status != "pass",
			Summary:      summary,
			Improvements: improvements,
			Evidence:     formatGoalFeatureEvidence(features, coverage),
		})
	}
	if promptImpliesDeliverable(run.Prompt) {
		status := "pass"
		summary := "Deliverable rail passed: file/artifact/evidence output exists."
		var improvements []string
		if !runHasDeliverable(*run) {
			status = "fail"
			summary = "Deliverable rail failed: no deliverable was produced for a task that asked for one."
			improvements = []string{"Produce the requested file, patch, report, artifact, or evidence before declaring done."}
		}
		verdicts = append(verdicts, VerifyVerdict{
			Gate:         "completion",
			Status:       status,
			Blocking:     blocking && status != "pass",
			Summary:      summary,
			Improvements: improvements,
		})
	}
	return verdicts
}

func extractGoalFeatures(goal string) []string {
	lower := strings.ToLower(goal)
	lower = strings.ReplaceAll(lower, "`", " ")
	var features []string
	seen := map[string]bool{}
	add := func(feature string) {
		feature = strings.Trim(feature, " .,:;!?\"'()[]{}")
		if len(feature) > 4 && strings.HasSuffix(feature, "s") {
			feature = strings.TrimSuffix(feature, "s")
		}
		if len(feature) < 2 || completionStopwords[feature] || seen[feature] {
			return
		}
		seen[feature] = true
		features = append(features, feature)
	}

	enumRe := regexp.MustCompile(`(?i)\b(?:add|create|implement|write|update|fix|build|make)\s+(?:an?\s+|the\s+)?([a-z0-9_\-/]+)`)
	for _, match := range enumRe.FindAllStringSubmatch(lower, -1) {
		add(match[1])
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/')
	}) {
		token = strings.TrimSpace(token)
		if completionActionWords[token] {
			continue
		}
		if shortGoalFeatures[token] {
			add(token)
			continue
		}
		if len(token) >= 3 && (strings.Contains(token, "_") || strings.Contains(token, "-") || strings.Contains(token, "/")) {
			add(token)
			continue
		}
		if len(token) >= 4 && !completionStopwords[token] {
			add(token)
		}
	}
	if len(features) > 12 {
		features = features[:12]
	}
	return features
}

func uncoveredGoalFeatures(coverage map[string]string, features []string) []string {
	var missing []string
	for _, feature := range features {
		if strings.TrimSpace(coverage[feature]) == "" {
			missing = append(missing, feature)
		}
	}
	return missing
}

type completionEvidenceItem struct {
	source string
	text   string
}

func goalFeatureCoverage(run TaskRun, features []string) map[string]string {
	items := independentCompletionEvidence(run)
	coverage := make(map[string]string, len(features))
	for _, feature := range features {
		needle := strings.ToLower(feature)
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.text), needle) {
				coverage[feature] = item.source
				break
			}
		}
	}
	return coverage
}

func independentCompletionEvidence(run TaskRun) []completionEvidenceItem {
	var items []completionEvidenceItem
	paths := map[string]bool{}
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		if isWriteTool(name) {
			if path := pathFromToolInput(tool.Input); path != "" {
				paths[path] = true
			}
			continue
		}
		switch name {
		case "evidence_bundle", "file_changes":
			items = append(items, completionEvidenceItem{source: name, text: tool.Result})
		case "shell":
			if looksLikeVerificationCommand(tool.Input) {
				items = append(items, completionEvidenceItem{source: "successful verification command", text: tool.Input + "\n" + tool.Result})
			}
		}
	}
	for path := range paths {
		data, err := os.ReadFile(filepath.Clean(filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		text := string(data)
		if len(text) > 4000 {
			text = text[:4000]
		}
		items = append(items, completionEvidenceItem{source: "file " + path, text: path + "\n" + text})
	}
	return items
}

func looksLikeVerificationCommand(input string) bool {
	lower := strings.ToLower(input)
	return hasAny(lower, " test", "test ", "pytest", "go vet", "lint", "build", "compile", "check-types")
}

func formatGoalFeatureEvidence(features []string, coverage map[string]string) string {
	lines := make([]string, 0, len(features))
	for _, feature := range features {
		source := strings.TrimSpace(coverage[feature])
		if source == "" {
			source = "missing"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", feature, source))
	}
	return strings.Join(lines, "\n")
}

func runToolEvidence(run TaskRun) string {
	var sb strings.Builder
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") {
			continue
		}
		sb.WriteString(tool.Name)
		sb.WriteString("\n")
		sb.WriteString(tool.Input)
		sb.WriteString("\n")
		sb.WriteString(tool.Result)
		sb.WriteString("\n")
	}
	return sb.String()
}

func runEventEvidence(run TaskRun) string {
	var sb strings.Builder
	for _, event := range run.Events {
		sb.WriteString(event.Kind)
		sb.WriteString("\n")
		sb.WriteString(event.Message)
		sb.WriteString("\n")
		sb.WriteString(event.Detail)
		sb.WriteString("\n")
	}
	return sb.String()
}

func touchedFileEvidence(run TaskRun) string {
	paths := map[string]bool{}
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") || !isWriteTool(tool.Name) {
			continue
		}
		if path := pathFromToolInput(tool.Input); path != "" {
			paths[path] = true
		}
	}
	var sb strings.Builder
	for path := range paths {
		sb.WriteString(path)
		sb.WriteString("\n")
		data, err := os.ReadFile(filepath.Clean(filepath.FromSlash(path)))
		if err == nil {
			text := string(data)
			if len(text) > 2000 {
				text = text[:2000]
			}
			sb.WriteString(text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func pathFromToolInput(input string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file", "file_path"} {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func promptImpliesDeliverable(prompt string) bool {
	lower := strings.ToLower(prompt)
	return hasAny(lower, "file", "report", "writeup", "write-up", "readme", "doc", "docs", "artifact", "patch", "pull request", "pr", ".md", ".go", ".ts", ".tsx", ".py", ".json")
}

func runHasDeliverable(run TaskRun) bool {
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") || !isWriteTool(tool.Name) {
			continue
		}
		if path := pathFromToolInput(tool.Input); path != "" {
			if info, err := os.Stat(filepath.Clean(filepath.FromSlash(path))); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	for _, event := range run.Events {
		if strings.Contains(strings.ToLower(event.Kind+" "+event.Message+" "+event.Detail), "artifact") ||
			strings.Contains(strings.ToLower(event.Kind+" "+event.Message+" "+event.Detail), "evidence") {
			return true
		}
	}
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		if name == "file_changes" || name == "evidence_bundle" {
			return true
		}
	}
	return false
}
