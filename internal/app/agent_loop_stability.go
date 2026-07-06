package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"mauler/internal/llm"
)

// idempotentReadTools are tools whose result is a pure function of their
// arguments within a single run: calling one again with byte-identical
// arguments returns the same data, so a second (or third) identical call is a
// sign the model is spinning rather than making progress. fetch_url and the
// shell tools have their own dedicated detectors (duplicateFetchURLSkip and the
// repeatedShell*Block family); write/edit are excluded because they
// legitimately re-target the same path.
var idempotentReadTools = map[string]bool{
	"read":      true,
	"read_file": true,
	"read_many": true,
	"read_pdf":  true,
	"glob":      true,
	"grep":      true,
}

// canonicalToolArgs returns a stable key for a tool call's JSON arguments so two
// calls that differ only in key order or whitespace compare equal. Falls back to
// the trimmed raw bytes when the arguments are not a JSON object, so unusual
// payloads are still compared literally rather than silently colliding.
func canonicalToolArgs(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return trimmed
	}
	if command, ok := obj["command"].(string); ok {
		obj["command"] = normalizeShellCommandForDedup(command)
	}
	// encoding/json marshals map keys in sorted order, giving a canonical form.
	canon, err := json.Marshal(obj)
	if err != nil {
		return trimmed
	}
	return string(canon)
}

// idempotentReadKey identifies a read-only tool call by name plus canonical
// arguments. Returns "" when the tool is not a tracked idempotent read or the
// arguments are unavailable (e.g. tool-input logging is disabled), so the
// detector silently no-ops instead of false-grouping unrelated calls.
func idempotentReadKey(name string, raw json.RawMessage) string {
	if !idempotentReadTools[name] {
		return ""
	}
	args := canonicalToolArgs(raw)
	if args == "" {
		return ""
	}
	return name + "\x00" + args
}

// repeatedIdenticalReadBlock fires when an idempotent read-only tool is about to
// run with arguments byte-identical to two or more prior successful calls this
// run. Re-reading the same file or re-running the same glob/grep a third time
// cannot yield new information, so it returns cached evidence and nudges the
// model to use what it already has instead of stopping the run.
//
// The threshold is two prior calls (so the block lands on the third), not one:
// a single legitimate re-read is common after context compaction drops an older
// tool result, and blocking that would strand the model without the file it
// needs. A third identical read is a reliable spin-out signal.
func repeatedIdenticalReadBlock(run TaskRun, tc llm.ToolCallDef) string {
	key := idempotentReadKey(tc.Function.Name, tc.Function.Arguments)
	if key == "" {
		return ""
	}
	matches := 0
	cached := ""
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if tool.Name != tc.Function.Name {
			continue
		}
		if idempotentReadKey(tool.Name, json.RawMessage(tool.Input)) != key {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(tool.Status), "done") {
			matches++
			if cached == "" {
				cached = strings.TrimSpace(truncateRunes(tool.Result, 1200))
			}
		}
	}
	if matches < 2 {
		return ""
	}
	if cached != "" {
		return fmt.Sprintf("%s cache hit: this exact call (same arguments) already ran %d times this run with the same result. Re-running it cannot produce new information - use the cached output below, change the arguments, or move on to the next step.\nCached result preview:\n%s", tc.Function.Name, matches, cached)
	}
	return fmt.Sprintf("%s cache hit: this exact call (same arguments) already ran %d times this run with the same result. Re-running it cannot produce new information - use the output you already have, change the arguments, or move on to the next step.", tc.Function.Name, matches)
}

// modelParamBillionsRE matches a parameter-count token like "27b", "8b", or the
// total/active pair in a MoE name like "26B-A4B". A digit must sit immediately
// before the "b" so quant tokens such as "q4_0" do not match.
var modelParamBillionsRE = regexp.MustCompile(`(\d+(?:\.\d+)?)b`)

// modelParamBillions extracts a model's parameter count in billions from its
// id ("qwen3.6-27b" -> 27, "gemma-4-26B-A4B" -> 26, "...-8b-instruct" -> 8).
// For MoE names with both a total and an active count it returns the larger
// (total) value, which is what governs tool-calling quality. Returns 0 when no
// count is present.
func modelParamBillions(modelID string) float64 {
	matches := modelParamBillionsRE.FindAllStringSubmatch(strings.ToLower(modelID), -1)
	best := 0.0
	for _, m := range matches {
		var v float64
		if _, err := fmt.Sscanf(m[1], "%g", &v); err != nil {
			continue
		}
		if v > best {
			best = v
		}
	}
	return best
}
