package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"mauler/internal/llm"
)

var errToolInputConstraint = errors.New("explicit tool-input constraint violated")

// These narrow operator forms exclude an IP literal from every tool argument,
// including planning text. Mentioning a stale address in a file or tool result
// cannot create or relax a constraint. The sealed objective is authoritative.
var toolInputExclusionRE = regexp.MustCompile(`(?i)\b(?:never|do not|don't)\s+(?:place|put|include|copy|use|pass|send)\s+([^\n!?]{1,240}?)\s+(?:in|into|inside)\s+(?:(?:a|an|any|the|all)\s+)?tool\s+(?:inputs?|arguments?)\b`)

func excludedToolInputIPs(run TaskRun) []string {
	prompt := taskRoutingPrompt(run)
	if run.Contract != nil {
		prompt = run.Contract.Objective
	}
	var excluded []string
	for _, match := range toolInputExclusionRE.FindAllStringSubmatch(prompt, -1) {
		for _, ip := range ipv4TokenRe.FindAllString(match[1], -1) {
			if validIPv4(ip) && !slices.Contains(excluded, ip) {
				excluded = append(excluded, ip)
			}
		}
	}
	return excluded
}

func guardExplicitToolInputExclusions(run TaskRun, call llm.ToolCallDef) string {
	excluded := excludedToolInputIPs(run)
	if len(excluded) == 0 {
		return ""
	}
	var input any
	if json.Unmarshal(call.Function.Arguments, &input) != nil {
		return "" // The ordinary schema boundary owns malformed JSON.
	}
	var containsExcluded func(any) bool
	containsExcluded = func(value any) bool {
		switch value := value.(type) {
		case string:
			for _, ip := range ipv4TokenRe.FindAllString(value, -1) {
				if slices.Contains(excluded, ip) {
					return true
				}
			}
		case []any:
			for _, item := range value {
				if containsExcluded(item) {
					return true
				}
			}
		case map[string]any:
			for key, item := range value {
				if containsExcluded(key) || containsExcluded(item) {
					return true
				}
			}
		}
		return false
	}
	if containsExcluded(input) {
		return "The operator explicitly excluded an IP literal from all tool inputs. This also applies to todo_write plan text and details. The call was not executed."
	}
	return ""
}

func toolInputConstraintPrompt(run TaskRun) string {
	if len(excludedToolInputIPs(run)) == 0 {
		return ""
	}
	return strings.TrimSpace(`
Tool-input constraint: the operator's excluded IP literals must be absent from every tool argument,
including todo_write items, detail, and descriptions. Use "excluded target" when recording the
exclusion in a plan. A violating call stops the run before execution.`)
}
