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
			TimeoutSecs:   180,
			MaxTurns:      4,
			MaxToolCalls:  8,
			MaxOutput:     1800,
			ContextBudget: 24576,
			Contract:      "Return: Findings, Sources/Evidence, Uncertainty, Recommended next step.",
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
	if spec.ContextBudget > 0 && spec.ContextBudget < profile.CtxTokens {
		profile.CtxTokens = spec.ContextBudget
	}
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
	toolCallsUsed := 0
	for turn := 0; turn < spec.MaxTurns; turn++ {
		if ctx.Err() != nil {
			return finalSubagentReport(spec, final.String(), toolCallsUsed, "timeout or cancellation"), ctx.Err()
		}
		req := buildChatRequest(profile, msgs, toolDefs, "auto", false)
		req.MaxTokens = spec.MaxOutput
		req.Temperature = 0.2
		ch, err := client.Chat(ctx, req)
		if err != nil {
			return finalSubagentReport(spec, final.String(), toolCallsUsed, err.Error()), err
		}
		var text strings.Builder
		var calls []llm.ToolCallDef
		for delta := range ch {
			if delta.Error != nil {
				return finalSubagentReport(spec, final.String(), toolCallsUsed, delta.Error.Error()), delta.Error
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
			return finalSubagentReport(spec, final.String(), toolCallsUsed, ""), nil
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
			if guarded, findings := guardToolResult(call.Function.Name, result); len(findings) > 0 {
				result = guarded
			}
			msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, truncateToolResult(result, cfg.Tools.MaxToolResultChars)))
		}
	}
	return finalSubagentReport(spec, final.String(), toolCallsUsed, "turn budget exhausted"), nil
}

func buildSubagentSystemPrompt(spec subagentSpec, profile settings.Profile, timeout, maxToolCalls int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are a bounded %s subagent inside TheMauler.\n", spec.Kind)
	fmt.Fprintf(&sb, "Profile: %s. Toolset: %s. Timeout: %ds. Context budget: %d tokens. Tool-call budget: %d.\n", profile.Name, spec.Toolset, timeout, spec.ContextBudget, maxToolCalls)
	sb.WriteString("Work only on the delegated task. Use tools when they materially improve evidence. Stop when the output contract is satisfied.\n")
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

func finalSubagentReport(spec subagentSpec, output string, toolCalls int, stop string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = "No subagent output was produced."
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
