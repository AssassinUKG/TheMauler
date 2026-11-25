package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"mauler/internal/settings"
)

var completionStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true,
	"by": true, "for": true, "from": true, "in": true, "into": true, "it": true,
	"of": true, "on": true, "or": true, "please": true, "that": true, "the": true, "this": true,
	"to": true, "with": true, "you": true, "your": true, "only": true, "after": true,
	"before": true, "then": true, "now": true, "current": true, "existing": true,
	"both": true, "finish": true, "finishing": true, "present": true, "project": true, "package": true,
	"using": true, "tool": true, "shell": true, "redirection": true,
	"containing": true, "through": true, "numbered": true, "true": true,
	"later": true, "earlier": true,
}

var completionActionWords = map[string]bool{
	"add": true, "build": true, "create": true, "fix": true, "generate": true,
	"edit": true, "finish": true, "implement": true, "make": true, "patch": true, "refactor": true, "update": true,
	"write": true, "wire": true,
}

var shortGoalFeatures = map[string]bool{
	"api": true, "csv": true, "id": true, "max": true, "min": true, "ui": true, "ux": true,
}

// runCompletionRails checks goal coverage and deliverable existence. By default
// it records advisory verdicts; cfg controls whether failures block completion.
func runCompletionRails(run *TaskRun, cfg *settings.Settings, verification []VerifyVerdict) []VerifyVerdict {
	if run == nil || cfg == nil {
		return nil
	}
	blocking := cfg.Agents.ReviewLoop.CompletionBlocking
	var verdicts []VerifyVerdict
	trustedPrompt := taskRoutingPrompt(*run)
	exactVerdicts, exclusive := exactFileCompletionVerdicts(*run)
	verdicts = append(verdicts, exactVerdicts...)
	features := extractGoalFeatures(trustedPrompt)
	if exclusive {
		features = nil
	}
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
		// An operator-configured test gate measures behavior. Keyword coverage
		// still explains possible gaps, but wording alone cannot veto that gate.
		blockingCoverage := blocking && len(missing) > 0
		if blockingCoverage && controlNeedsProjectVerification(*run) && len(cfg.Agents.ReviewLoop.VerifyCommands) > 0 && slices.ContainsFunc(verification, func(verdict VerifyVerdict) bool {
			return verdict.Gate == "test" && verdict.Status == "pass"
		}) {
			blockingCoverage = false
			summary += " Configured project tests passed, so keyword coverage is advisory."
		}
		verdicts = append(verdicts, VerifyVerdict{
			Gate:         "completion",
			Status:       status,
			Blocking:     blockingCoverage,
			Summary:      summary,
			Improvements: improvements,
			Evidence:     formatGoalFeatureEvidence(features, coverage),
		})
	}
	if promptImpliesDeliverable(trustedPrompt) {
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
	if containsAnyFeature(features, "min", "max") {
		features = removeGoalFeature(features, "helper")
	}
	return features
}

func containsAnyFeature(features []string, candidates ...string) bool {
	for _, feature := range features {
		for _, candidate := range candidates {
			if feature == candidate {
				return true
			}
		}
	}
	return false
}

func removeGoalFeature(features []string, remove string) []string {
	filtered := features[:0]
	for _, feature := range features {
		if feature != remove {
			filtered = append(filtered, feature)
		}
	}
	return filtered
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
	source     string
	text       string
	toolName   string
	toolIndex  int
	toolInput  string
	toolResult string
}

func goalFeatureCoverage(run TaskRun, features []string) map[string]string {
	items := independentCompletionEvidence(run)
	coverage := make(map[string]string, len(features))
	for _, feature := range features {
		needle := strings.ToLower(feature)
		for _, item := range items {
			haystack := strings.ToLower(item.source + "\n" + item.text)
			textCoverage := item.toolName == "" && item.source != "successful verification command" && strings.Contains(haystack, needle)
			if textCoverage || verificationEvidenceCoversFeature(item, needle) || toolActionEvidenceCoversFeature(item, needle, features, run.Tools) {
				coverage[feature] = item.source
				break
			}
		}
	}
	return coverage
}

// toolActionEvidenceCoversFeature translates a small set of controller-owned,
// successful tool events into procedural evidence. It deliberately inspects
// tool identity, ordering, structured arguments, and controller status rather
// than trusting assistant prose or arbitrary tool-result text.
func toolActionEvidenceCoversFeature(item completionEvidenceItem, feature string, goalFeatures []string, tools []TaskToolEvent) bool {
	name := strings.ToLower(strings.TrimSpace(item.toolName))
	if name == "" {
		return false
	}
	if feature == "first" {
		if containsAnyFeature(goalFeatures, "grep") {
			return name == "grep" && toolPrecedesFirstSuccessful(item.toolIndex, tools, "edit", "edit_file")
		}
		if containsAnyFeature(goalFeatures, "todo_write") {
			return name == "todo_write" && toolPrecedesFirstSuccessful(item.toolIndex, tools, "write", "write_file")
		}
		return item.toolIndex == 0
	}
	obj := toolInputObject(item.toolInput)
	switch name {
	case "todo_write":
		switch feature {
		case "todo_write", "plan":
			return todoPlanItemCount(obj) > 0
		case "short":
			count := todoPlanItemCount(obj)
			return count > 0 && count <= 8
		}
	case "grep":
		if !toolResultHasObservedMatch(item.toolResult) {
			return false
		}
		switch feature {
		case "grep", "find", "marker", "verify":
			return true
		case "todo":
			return toolStringArgumentContains(obj, "pattern", feature)
		}
	case "edit", "edit_file":
		if !hasStructuredReplacement(obj) {
			return false
		}
		switch feature {
		case "edit", "replace", "definition":
			return true
		}
	case "write", "write_file":
		switch feature {
		case "write", "model-facing":
			return true
		case "append", "chunk", "overwrite":
			return toolBoolArgument(obj, "append")
		}
	}
	return false
}

func toolPrecedesFirstSuccessful(toolIndex int, tools []TaskToolEvent, laterNames ...string) bool {
	for index, tool := range tools {
		if !strings.EqualFold(tool.Status, "done") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		if slices.Contains(laterNames, name) {
			return toolIndex < index
		}
	}
	return false
}

func toolInputObject(input string) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		return nil
	}
	return obj
}

func todoPlanItemCount(obj map[string]any) int {
	if obj == nil || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(obj["action"])), "replace") {
		return 0
	}
	items, ok := obj["items"].([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func toolStringArgumentContains(obj map[string]any, key, want string) bool {
	value, ok := obj[key].(string)
	return ok && strings.Contains(strings.ToLower(value), strings.ToLower(want))
}

func toolBoolArgument(obj map[string]any, key string) bool {
	value, ok := obj[key].(bool)
	return ok && value
}

func hasStructuredReplacement(obj map[string]any) bool {
	if obj == nil {
		return false
	}
	oldValue, oldOK := obj["old"].(string)
	newValue, newOK := obj["new"].(string)
	return oldOK && newOK && oldValue != newValue
}

func toolResultHasObservedMatch(result string) bool {
	lower := strings.ToLower(strings.TrimSpace(result))
	return lower != "" && !hasAny(lower, "no match", "not found", "0 matches")
}

// verificationEvidenceCoversFeature supplies narrow semantic aliases for a
// successful, independently executed build command. A compiler does not need
// to print the words "compile error fixed" when exit-zero build evidence proves
// exactly that outcome. Keep this restricted to trusted verification tools;
// model prose and ordinary tool claims must never satisfy blocking coverage.
func verificationEvidenceCoversFeature(item completionEvidenceItem, feature string) bool {
	if item.source != "successful verification command" {
		return false
	}
	if feature == "verify" {
		return true
	}
	_, compileVerified := verificationCommandEvidence(item.toolInput)
	if !compileVerified {
		return false
	}
	switch feature {
	case "build", "compile", "error", "fixed":
		return true
	default:
		return false
	}
}

func independentCompletionEvidence(run TaskRun) []completionEvidenceItem {
	var items []completionEvidenceItem
	paths := map[string]bool{}
	for index, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		switch name {
		case "todo_write", "grep", "edit", "edit_file", "write", "write_file":
			items = append(items, completionEvidenceItem{
				source:     "successful " + name + " action",
				toolName:   name,
				toolIndex:  index,
				toolInput:  tool.Input,
				toolResult: tool.Result,
			})
		}
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
			command := shellCommandFromToolArgs([]byte(tool.Input))
			if check, _ := verificationCommandEvidence(command); check {
				items = append(items, completionEvidenceItem{source: "successful verification command", toolInput: command, text: tool.Result})
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
