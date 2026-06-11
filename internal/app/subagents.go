package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

type subagentKind string

const (
	subagentResearcher subagentKind = "Researcher"
	subagentReviewer   subagentKind = "Reviewer"
	subagentTestFix    subagentKind = "Test/Fix"
	subagentSummarizer subagentKind = "Summarizer"
)

type subagentSpec struct {
	ToolName      string
	ModeName      string
	Kind          subagentKind
	Toolset       string
	TimeoutSecs   int
	MaxTurns      int
	MaxToolCalls  int
	MaxOutput     int
	ContextBudget int
	Destructive   bool
	Contract      string
}

type subagentTool struct {
	app  *App
	spec subagentSpec
}

type subagentArgs struct {
	Task           string `json:"task"`
	Context        string `json:"context"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxToolCalls   int    `json:"max_tool_calls"`
}

func (a *App) registerAppTools() {
	for _, spec := range subagentSpecs() {
		a.registry.Register(&subagentTool{app: a, spec: spec})
	}
}

func subagentSpecs() []subagentSpec {
	return []subagentSpec{
		{
			ToolName:      "subagent_research",
			ModeName:      "Researcher",
			Kind:          subagentResearcher,
			Toolset:       "web-research",
			TimeoutSecs:   300,
			MaxTurns:      8,
			MaxToolCalls:  16,
			MaxOutput:     2400,
			ContextBudget: 24576,
			Contract:      "Return: Findings, Sources/Evidence, Searches tried, Uncertainty, Recommended next step. If no good sources are found, say exactly what searches/tools failed and propose the next local enumeration step.",
		},
		{
			ToolName:      "subagent_review",
			ModeName:      "Reviewer",
			Kind:          subagentReviewer,
			Toolset:       "safe",
			TimeoutSecs:   150,
			MaxTurns:      4,
			MaxToolCalls:  8,
			MaxOutput:     1800,
			ContextBudget: 24576,
			Contract:      "Return: Findings ordered by severity, Evidence with file/path references, Open questions, Residual risk.",
		},
		{
			ToolName:      "subagent_testfix",
			ModeName:      "Fixer",
			Kind:          subagentTestFix,
			Toolset:       "local-code",
			TimeoutSecs:   240,
			MaxTurns:      6,
			MaxToolCalls:  12,
			MaxOutput:     2200,
			ContextBudget: 32768,
			Destructive:   true,
			Contract:      "Return: Root cause, Changes made, Verification run, Remaining risk. Keep edits narrow.",
		},
		{
			ToolName:      "subagent_summarize",
			ModeName:      "Planner",
			Kind:          subagentSummarizer,
			Toolset:       "safe",
			TimeoutSecs:   90,
			MaxTurns:      2,
			MaxToolCalls:  3,
			MaxOutput:     1400,
			ContextBudget: 16384,
			Contract:      "Return: Concise summary, Decisions, Files/commands mentioned, Next actions.",
		},
	}
}

func (t *subagentTool) Name() string { return t.spec.ToolName }

func (t *subagentTool) Description() string {
	return fmt.Sprintf("Run a bounded %s subagent with its own scratch context, %s toolset, timeout, and output contract.", t.spec.Kind, t.spec.Toolset)
}

func (t *subagentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"task":{"type":"string","description":"Specific task for the bounded subagent."},
			"context":{"type":"string","description":"Optional concise context the subagent should consider."},
			"timeout_seconds":{"type":"integer","minimum":10,"maximum":600,"description":"Optional timeout override within the tool's hard cap."},
			"max_tool_calls":{"type":"integer","minimum":0,"maximum":30,"description":"Optional tool-call budget override within the tool's hard cap."}
		},
		"required":["task"]
	}`)
}

func (t *subagentTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args subagentArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	args.Task = strings.TrimSpace(args.Task)
	if args.Task == "" {
		return "", fmt.Errorf("task is required")
	}
	return t.app.runBoundedSubagent(ctx, t.spec, args)
}

func (t *subagentTool) Destructive() bool { return t.spec.Destructive }

func (a *App) runBoundedSubagent(parent context.Context, spec subagentSpec, args subagentArgs) (string, error) {
	a.mu.Lock()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()

	profile := activeProfile(&cfg, &profiles)
	timeout := boundedValue(args.TimeoutSeconds, spec.TimeoutSecs, 10, spec.TimeoutSecs)
	maxToolCalls := boundedValue(args.MaxToolCalls, spec.MaxToolCalls, 0, spec.MaxToolCalls)

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()

	client, err := buildClient(profile)
	if err != nil {
		return "", err
	}
	if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
		return "", err
	}

	registry := tools.New()
	toolCfg := cfg.Tools
	toolCfg.ActiveToolset = spec.Toolset
	toolDefs := registry.ToEnabledToolDefs(settings.EffectiveEnabledTools(toolCfg))
	if !cfg.Tools.Enabled {
		toolDefs = nil
	}

	msgs := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, buildSubagentSystemPrompt(spec, profile, timeout, maxToolCalls)),
		llm.NewTextMessage(llm.RoleUser, buildSubagentUserPrompt(args)),
	}

	var final strings.Builder
	var evidence []string
	toolCallsUsed := 0
	for turn := 0; turn < spec.MaxTurns; turn++ {
		if ctx.Err() != nil {
			return finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, "timeout or cancellation"), ctx.Err()
		}
		reqToolDefs := toolDefs
		toolChoice := "auto"
		if maxToolCalls >= 0 && toolCallsUsed >= maxToolCalls {
			reqToolDefs = nil
			toolChoice = "none"
			msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, "Your subagent tool budget is exhausted. Do not call tools. Return the required output contract now using only the evidence already gathered, including searches tried and uncertainty."))
		}
		req := buildChatRequest(profile, msgs, reqToolDefs, toolChoice, false, subagentUsesCodingParams(spec.Kind))
		req.MaxTokens = spec.MaxOutput
		req.Temperature = 0.2
		ch, err := client.Chat(ctx, req)
		if err != nil {
			return finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, err.Error()), err
		}
		var text strings.Builder
		var calls []llm.ToolCallDef
		for delta := range ch {
			if delta.Error != nil {
				return finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, delta.Error.Error()), delta.Error
			}
			text.WriteString(delta.Content)
			if len(delta.ToolCalls) > 0 {
				calls = append(calls, delta.ToolCalls...)
			}
		}
		if strings.TrimSpace(text.String()) != "" {
			final.WriteString(text.String())
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: text.String(), ToolCalls: calls})
		} else if len(calls) > 0 {
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: "", ToolCalls: calls})
		}
		if len(calls) == 0 {
			return finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, ""), nil
		}
		for _, call := range calls {
			if maxToolCalls >= 0 && toolCallsUsed >= maxToolCalls {
				msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, "subagent tool-call budget exhausted; finish with current evidence."))
				continue
			}
			toolCallsUsed++
			if tool, ok := registry.Get(call.Function.Name); ok && tool.Destructive() && !spec.Destructive {
				msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, "blocked: this subagent is read-only."))
				continue
			}
			if spec.Destructive && isWriteTool(call.Function.Name) {
				if snapPath := extractPath(call); snapPath != "" {
					_ = a.rollback.Push(agent.OpWrite, tools.NormalizeHostPath(snapPath))
				}
			}
			result, runErr := registry.Run(ctx, call)
			if runErr != nil {
				result = toolErrorResult(result, runErr)
			} else if spec.Destructive && isWriteTool(call.Function.Name) {
				if verification := verifyMutationResult(call); verification != "" {
					result = result + "\n" + verification
				}
			}
			if guarded, findings := guardToolResult(call.Function.Name, result, cfg.Tools.RedactSecrets); len(findings) > 0 {
				result = guarded
			}
			evidence = appendSubagentEvidence(evidence, call.Function.Name, result)
			msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, truncateToolResult(result, cfg.Tools.MaxToolResultChars)))
		}
	}
	return finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, "turn budget exhausted"), nil
}

func buildSubagentSystemPrompt(spec subagentSpec, profile settings.Profile, timeout, maxToolCalls int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are a bounded %s subagent inside TheMauler.\n", spec.Kind)
	fmt.Fprintf(&sb, "Profile: %s. Toolset: %s. Timeout: %ds. Context budget: %d tokens. Tool-call budget: %d.\n", profile.Name, spec.Toolset, timeout, spec.ContextBudget, maxToolCalls)
	sb.WriteString("Work only on the delegated task. Use tools when they materially improve evidence. Stop when the output contract is satisfied.\n")
	sb.WriteString("You must always produce a final textual report. If searches fail, report the failed queries/tools and uncertainty rather than returning blank output.\n")
	if !spec.Destructive {
		sb.WriteString("This subagent is read-only. Do not request write/edit/shell mutations.\n")
	}
	sb.WriteString(spec.Contract)
	sb.WriteString(buildWorkspaceContextPrompt())
	return sb.String()
}

func buildSubagentUserPrompt(args subagentArgs) string {
	if strings.TrimSpace(args.Context) == "" {
		return args.Task
	}
	return args.Task + "\n\nProvided context:\n" + strings.TrimSpace(args.Context)
}

func finalSubagentReport(spec subagentSpec, output string, evidence []string, toolCalls int, stop string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = fallbackSubagentOutput(spec, evidence)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Subagent: %s\nToolset: %s\nTool calls used: %d\n", spec.Kind, spec.Toolset, toolCalls)
	if strings.TrimSpace(stop) != "" {
		fmt.Fprintf(&sb, "Stop: %s\n", strings.TrimSpace(stop))
	}
	sb.WriteString("\n")
	sb.WriteString(output)
	return sb.String()
}

func subagentUsesCodingParams(kind subagentKind) bool {
	switch kind {
	case subagentReviewer, subagentTestFix:
		return true
	default:
		return false
	}
}

func appendSubagentEvidence(evidence []string, toolName, result string) []string {
	result = strings.TrimSpace(result)
	if result == "" {
		result = "(empty result)"
	}
	if len(result) > 500 {
		result = result[:500] + "... [truncated]"
	}
	evidence = append(evidence, fmt.Sprintf("%s: %s", toolName, result))
	if len(evidence) > 8 {
		evidence = evidence[len(evidence)-8:]
	}
	return evidence
}

func fallbackSubagentOutput(spec subagentSpec, evidence []string) string {
	var sb strings.Builder
	sb.WriteString("Findings:\n")
	if len(evidence) == 0 {
		sb.WriteString("- No usable evidence was gathered before the subagent stopped.\n")
	} else {
		sb.WriteString("- The subagent stopped before writing a synthesis. Recent evidence/tool results are preserved below.\n")
	}
	sb.WriteString("\nSources/Evidence:\n")
	if len(evidence) == 0 {
		sb.WriteString("- None.\n")
	} else {
		for _, item := range evidence {
			sb.WriteString("- ")
			sb.WriteString(strings.ReplaceAll(item, "\n", "\n  "))
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nUncertainty:\n- Search/subagent budget ended before a reliable conclusion was produced.\n")
	if spec.Kind == subagentResearcher {
		sb.WriteString("\nRecommended next step:\n- Continue with local enumeration and targeted searches using explicit queries; avoid treating this incomplete subagent result as evidence.\n")
	} else {
		sb.WriteString("\nRecommended next step:\n- Continue from the preserved evidence above.\n")
	}
	return sb.String()
}

func boundedValue(value, fallback, minValue, maxValue int) int {
	if value <= 0 {
		value = fallback
	}
	if value < minValue {
		return minValue
	}
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}
