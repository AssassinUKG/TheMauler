package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mauler/internal/agent"
	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

type subagentKind string

const (
	subagentResearcher subagentKind = "Researcher"
	subagentExplorer   subagentKind = "Explorer"
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

type taskTool struct {
	app   *App
	specs map[string]subagentSpec
}

type subagentArgs struct {
	Type           string `json:"type"`
	Task           string `json:"task"`
	Context        string `json:"context"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxToolCalls   int    `json:"max_tool_calls"`
	EngagementID   string `json:"engagement_id"`
	WorkKind       string `json:"work_kind"`
	PhaseID        string `json:"phase_id"`
	EndpointID     string `json:"endpoint_id"`
	WorkID         string `json:"work_id"`
}

func (a *App) registerAppTools() {
	task := &taskTool{app: a, specs: map[string]subagentSpec{}}
	for _, spec := range subagentSpecs() {
		task.specs[subagentTypeName(spec.ToolName)] = spec
	}
	a.registry.Register(task)
	a.registry.Register(&memoryTool{app: a})
	a.registry.Register(&fileChangesTool{app: a})
	a.registry.Register(&readToolResultTool{app: a})
	a.registry.Register(&progressTool{app: a})
	a.registry.Register(&runScriptTool{app: a})
	a.registry.Register(&httpProbeTool{app: a})
	a.registry.Register(&evidenceBundleTool{app: a})
	a.registry.Register(&terminalSendTool{app: a})
	a.registry.Register(&terminalReadTool{app: a})
	a.registry.Register(&startListenerTool{app: a})
	a.registry.Register(&generateImageTool{app: a})
	if a.engagements != nil {
		a.registry.Register(&engagementTool{app: a})
	}
}

func subagentSpecs() []subagentSpec {
	return []subagentSpec{
		{
			ToolName:      "subagent_explore",
			ModeName:      "Planner",
			Kind:          subagentExplorer,
			Toolset:       "explore",
			TimeoutSecs:   180,
			MaxTurns:      6,
			MaxToolCalls:  20,
			MaxOutput:     2200,
			ContextBudget: 24576,
			Contract:      "Return: Scope inspected, Key files/symbols found, Relevant snippets or line references, Gaps/uncertainty, Recommended next inspection or implementation step. Do not edit files.",
		},
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
			,"engagement_id":{"type":"string","description":"Optional active engagement id for a focused assigned work item."}
			,"work_kind":{"type":"string","enum":["step","global_check","endpoint_check"]}
			,"phase_id":{"type":"string"}
			,"endpoint_id":{"type":"string"}
			,"work_id":{"type":"string","description":"Exact delegated engagement work id."}
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

func (t *taskTool) Name() string { return "task" }

func (t *taskTool) Description() string {
	return "Delegate a bounded sub-task to type explore, research, review, testfix, or summarize; returns a compact report and keeps main context clean."
}

func (t *taskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"type":{"type":"string","enum":["explore","research","review","testfix","summarize"],"description":"Subagent type."},
			"task":{"type":"string","description":"Specific task for the bounded subagent."},
			"context":{"type":"string","description":"Optional concise context the subagent should consider."},
			"timeout_seconds":{"type":"integer","minimum":10,"maximum":600,"description":"Optional timeout override within the type's hard cap."},
			"max_tool_calls":{"type":"integer","minimum":0,"maximum":30,"description":"Optional tool-call budget override within the type's hard cap."}
			,"engagement_id":{"type":"string","description":"Optional active engagement id for a focused assigned work item."}
			,"work_kind":{"type":"string","enum":["step","global_check","endpoint_check"]}
			,"phase_id":{"type":"string"}
			,"endpoint_id":{"type":"string"}
			,"work_id":{"type":"string","description":"Exact delegated engagement work id."}
		},
		"required":["type","task"]
	}`)
}

func (t *taskTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args subagentArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	typ := strings.ToLower(strings.TrimSpace(args.Type))
	spec, ok := t.specs[typ]
	if !ok {
		return "", fmt.Errorf("task: unknown type %q", args.Type)
	}
	args.Task = strings.TrimSpace(args.Task)
	if args.Task == "" {
		return "", fmt.Errorf("task is required")
	}
	return t.app.runBoundedSubagent(ctx, spec, args)
}

func (t *taskTool) Destructive() bool { return true }

func subagentTypeName(toolName string) string {
	return strings.TrimPrefix(strings.TrimSpace(toolName), "subagent_")
}

func (a *App) runBoundedSubagent(parent context.Context, spec subagentSpec, args subagentArgs) (string, error) {
	a.mu.Lock()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()

	profile := activeProfile(&cfg, &profiles)
	timeout := boundedValue(args.TimeoutSeconds, spec.TimeoutSecs, 10, spec.TimeoutSecs)
	maxToolCalls := boundedValue(args.MaxToolCalls, spec.MaxToolCalls, 0, spec.MaxToolCalls)
	subagentID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	assignmentPacket, assigned, err := a.subagentEngagementAssignment(parent, args)
	if err != nil {
		return "", err
	}
	parentClaimant, _ := engagementClaimantFromContext(parent)
	claimantID := subagentID
	if parentClaimant.ID != "" {
		claimantID = parentClaimant.ID + "/" + subagentID
	}
	claimantAlias := string(spec.Kind)
	a.recordLedger(ledger.Event{
		ID:      subagentID,
		Kind:    "subagent_start",
		Source:  "subagent",
		Tool:    spec.ToolName,
		Status:  "running",
		Message: string(spec.Kind),
		Input:   args.Task,
		Detail:  args.Context,
		Metadata: map[string]string{
			"toolset":             spec.Toolset,
			"timeout_secs":        fmt.Sprintf("%d", timeout),
			"max_tool_calls":      fmt.Sprintf("%d", maxToolCalls),
			"profile":             profile.Name,
			"model":               profile.ModelID,
			"claimant_id":         claimantID,
			"parent_claimant":     parentClaimant.ID,
			"engagement_assigned": fmt.Sprintf("%t", assigned),
		},
	})

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()
	stopEngagementHeartbeat := a.startEngagementClaimHeartbeat(ctx, claimantID, claimantAlias, "subagent")
	defer stopEngagementHeartbeat()

	client, err := buildClient(profile)
	if err != nil {
		a.recordLedger(ledger.Event{
			Kind:    "subagent_done",
			Source:  "subagent",
			Tool:    spec.ToolName,
			Status:  "error",
			Message: subagentID,
			Error:   err.Error(),
		})
		return "", err
	}
	if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
		a.recordLedger(ledger.Event{
			Kind:    "subagent_done",
			Source:  "subagent",
			Tool:    spec.ToolName,
			Status:  "error",
			Message: subagentID,
			Error:   err.Error(),
		})
		return "", err
	}

	registry := tools.New()
	if assigned {
		registry.Register(&engagementTool{app: a})
		registry.Register(&httpProbeTool{app: a})
		registry.Register(&evidenceBundleTool{app: a})
	}
	toolCfg := cfg.Tools
	toolCfg.ActiveToolset = spec.Toolset
	enabledTools := settings.EffectiveEnabledTools(toolCfg)
	if assigned {
		enabledTools["engagement"] = true
	}
	toolDefs := registry.ToEnabledToolDefs(enabledTools)
	if !cfg.Tools.Enabled {
		toolDefs = nil
	}

	msgs := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, buildSubagentSystemPrompt(spec, profile, timeout, maxToolCalls)),
	}
	if assignmentPacket != "" {
		msgs = append(msgs, llm.NewTextMessage(llm.RoleSystem, assignmentPacket))
	}
	msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, buildSubagentUserPrompt(args)))

	var final strings.Builder
	var evidence []string
	toolCallsUsed := 0
	for turn := 0; turn < spec.MaxTurns; turn++ {
		if ctx.Err() != nil {
			report := finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, "timeout or cancellation")
			a.recordLedger(ledger.Event{
				Kind:    "subagent_done",
				Source:  "subagent",
				Tool:    spec.ToolName,
				Status:  "cancelled",
				Message: subagentID,
				Output:  report,
				Error:   ctx.Err().Error(),
			})
			return report, ctx.Err()
		}
		reqToolDefs := toolDefs
		toolChoice := "auto"
		if maxToolCalls >= 0 && toolCallsUsed >= maxToolCalls {
			reqToolDefs = nil
			toolChoice = "none"
			msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, "Your subagent tool budget is exhausted. Do not call tools. Return the required output contract now using only the evidence already gathered, including searches tried and uncertainty."))
		}
		req := buildChatRequest(profile, msgs, reqToolDefs, toolChoice, false, subagentUsesCodingParams(spec.Kind), "medium")
		req.MaxTokens = spec.MaxOutput
		req.Temperature = 0.2
		ch, err := client.Chat(ctx, req)
		if err != nil {
			report := finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, err.Error())
			a.recordLedger(ledger.Event{
				Kind:    "subagent_done",
				Source:  "subagent",
				Tool:    spec.ToolName,
				Status:  "error",
				Message: subagentID,
				Output:  report,
				Error:   err.Error(),
			})
			return report, err
		}
		var text strings.Builder
		var calls []llm.ToolCallDef
		for delta := range ch {
			if delta.Error != nil {
				report := finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, delta.Error.Error())
				a.recordLedger(ledger.Event{
					Kind:    "subagent_done",
					Source:  "subagent",
					Tool:    spec.ToolName,
					Status:  "error",
					Message: subagentID,
					Output:  report,
					Error:   delta.Error.Error(),
				})
				return report, delta.Error
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
			report := finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, "")
			a.recordLedger(ledger.Event{
				Kind:    "subagent_done",
				Source:  "subagent",
				Tool:    spec.ToolName,
				Status:  "done",
				Message: subagentID,
				Output:  report,
			})
			return report, nil
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
			var beforeMutation fileChangeSnapshot
			if spec.Destructive && isWriteTool(call.Function.Name) {
				beforeMutation = snapshotToolTarget(call)
				if snapPath := extractPath(call); snapPath != "" {
					_ = a.rollback.Push(agent.OpWrite, tools.NormalizeHostPath(snapPath))
				}
			}
			result, runErr := registry.Run(withEngagementClaimant(ctx, claimantID, claimantAlias), call)
			if runErr != nil {
				result = toolErrorResult(result, runErr)
			} else if spec.Destructive && isWriteTool(call.Function.Name) {
				if verification := verifyMutationResult(call); verification != "" {
					result = result + "\n" + verification
					a.recordFileChange("", call, beforeMutation, verification, 0)
				}
			}
			status := "done"
			if runErr != nil {
				status = "error"
			}
			a.recordLedger(ledger.Event{
				Kind:    "subagent_tool",
				Source:  "subagent",
				Tool:    call.Function.Name,
				Status:  status,
				Message: subagentID,
				Input:   string(call.Function.Arguments),
				Output:  result,
			})
			if guarded, findings := guardToolResult(call.Function.Name, result, cfg.Tools.RedactSecrets); len(findings) > 0 {
				result = guarded
			}
			evidence = appendSubagentEvidence(evidence, call.Function.Name, result)
			msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, a.toolResultForContext(subagentID, call.Function.Name, result, cfg.Tools)))
		}
	}
	report := finalSubagentReport(spec, final.String(), evidence, toolCallsUsed, "turn budget exhausted")
	a.recordLedger(ledger.Event{
		Kind:    "subagent_done",
		Source:  "subagent",
		Tool:    spec.ToolName,
		Status:  "exhausted",
		Message: subagentID,
		Output:  report,
	})
	return report, nil
}

func (a *App) subagentEngagementAssignment(ctx context.Context, args subagentArgs) (string, bool, error) {
	values := []string{args.EngagementID, args.WorkKind, args.PhaseID, args.EndpointID, args.WorkID}
	hasAny := false
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return "", false, nil
	}
	if strings.TrimSpace(args.EngagementID) == "" || strings.TrimSpace(args.WorkKind) == "" || strings.TrimSpace(args.WorkID) == "" {
		return "", false, fmt.Errorf("task engagement assignment requires engagement_id, work_kind, and work_id")
	}
	ref, err := engagementWorkRef(engagementToolArgs{
		Kind: args.WorkKind, PhaseID: args.PhaseID, EndpointID: args.EndpointID, WorkID: args.WorkID,
	})
	if err != nil {
		return "", false, err
	}
	record, err := (&engagementTool{app: a}).resolveRecord(ctx, args.EngagementID)
	if err != nil {
		return "", false, err
	}
	work := engagementWorkState(record.State, ref)
	if work == nil {
		return "", false, fmt.Errorf("task engagement assignment references unknown work %s/%s", ref.Kind, ref.ID)
	}
	if work.Finished {
		return "", false, fmt.Errorf("task engagement assignment %s/%s is already finished", ref.Kind, ref.ID)
	}
	available, err := a.engagements.Available(ctx, record.State.ID, "", 12, time.Now())
	if err != nil {
		return "", false, fmt.Errorf("task engagement assignment is not currently available: %w", err)
	}
	assignable := false
	for _, candidate := range available.Items {
		if candidate.Ref == ref {
			assignable = true
			break
		}
	}
	if !assignable {
		detail := strings.TrimSpace(available.Blocker)
		if detail == "" {
			detail = "the item is outside the current deterministic queue"
		}
		return "", false, fmt.Errorf("task engagement assignment %s/%s is not currently available: %s", ref.Kind, ref.ID, detail)
	}
	scope := append([]string(nil), record.State.Scope...)
	if len(scope) > 4 {
		scope = append(scope[:4], fmt.Sprintf("+%d more", len(record.State.Scope)-4))
	}
	packet := fmt.Sprintf(
		"Focused Engagement Assignment (authoritative and exclusive):\n- engagement_id=%s\n- work_kind=%s phase_id=%s endpoint_id=%s work_id=%s\n- title=%s status=%s revision=%d\n- locked_scope=%v entries=%s\n- discipline: operate only this assigned work item. Call engagement claim before testing; attach real RunLedger/file evidence where applicable; then observe and finish with returned revisions. Do not select another grid item or expand scope.",
		record.State.ID, ref.Kind, ref.PhaseID, ref.EndpointID, ref.ID, engagementPromptValue(work.Title, 140), work.Status, work.Revision,
		record.State.ScopeLocked, engagementPromptValue(strings.Join(scope, ", "), 240),
	)
	return packet, true, nil
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
	case subagentExplorer, subagentReviewer, subagentTestFix:
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
