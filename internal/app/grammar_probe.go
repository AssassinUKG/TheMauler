package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mauler/internal/llm"
)

type GrammarToolArgsProbeResult struct {
	Profile        string `json:"profile"`
	Backend        string `json:"backend"`
	ModelID        string `json:"model_id"`
	Supported      bool   `json:"supported"`
	StructuredCall bool   `json:"structured_call"`
	ValidArguments bool   `json:"valid_arguments"`
	ToolName       string `json:"tool_name,omitempty"`
	Arguments      string `json:"arguments,omitempty"`
	Text           string `json:"text,omitempty"`
	Error          string `json:"error,omitempty"`
	Recommendation string `json:"recommendation"`
}

func (a *App) RunGrammarToolArgsProbe(profileName string) GrammarToolArgsProbeResult {
	result := GrammarToolArgsProbeResult{Profile: strings.TrimSpace(profileName)}
	if result.Profile == "" {
		result.Error = "profile name is required"
		result.Recommendation = "Select a profile before running the grammar tool-args probe."
		return result
	}
	a.mu.Lock()
	profiles := a.profiles
	a.mu.Unlock()
	if profiles == nil {
		result.Error = "profiles are not loaded"
		result.Recommendation = "Refresh profiles and try again."
		return result
	}
	profile, ok := profiles.Profiles[result.Profile]
	if !ok {
		result.Error = "profile not found"
		result.Recommendation = "Select an existing profile and try again."
		return result
	}
	profile = applyProvider(profile, profiles)
	result.Backend = profile.Backend
	result.ModelID = profile.ModelID

	client, err := buildClientForAgent(profile)
	if err != nil {
		result.Error = err.Error()
		result.Recommendation = "Fix the profile/provider settings, then rerun the probe."
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
		result.Error = err.Error()
		result.Recommendation = "The model could not be prepared for the probe."
		return result
	}

	tool := grammarProbeToolDef()
	req := buildChatRequest(profile, []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "You are testing tool-call formatting. Do not answer in prose."),
		llm.NewTextMessage(llm.RoleUser, `Call the probe_read_file tool exactly once with path "probe.txt".`),
	}, []llm.ToolDef{tool}, "required", true, false, "minimal")
	req.MaxTokens = 256
	req.Temperature = 0
	req.TopP = 1
	req.TopK = 1
	req.MinP = 0
	req.PresencePenalty = 0
	req.JSONSchema = tool.Function.Parameters

	ch, err := client.Chat(ctx, req)
	if err != nil {
		result.Error = err.Error()
		result.Recommendation = "The constrained request failed before a response was streamed."
		return result
	}
	var text strings.Builder
	var calls []llm.ToolCallDef
	for delta := range ch {
		if delta.Error != nil {
			result.Error = delta.Error.Error()
			result.Recommendation = "The constrained stream failed. Keep grammar tool args disabled for this profile."
			return result
		}
		text.WriteString(delta.Content)
		calls = append(calls, delta.ToolCalls...)
	}
	result.Text = strings.TrimSpace(text.String())
	if len(calls) == 0 {
		result.Recommendation = "Unsafe for tool arguments: the backend did not return a structured tool_calls entry while constrained."
		return result
	}
	call := normalizeToolCallArguments(calls[0])
	result.StructuredCall = true
	result.ToolName = call.Function.Name
	result.Arguments = string(call.Function.Arguments)

	var args struct {
		Path string `json:"path"`
	}
	result.ValidArguments = json.Unmarshal(call.Function.Arguments, &args) == nil && args.Path == "probe.txt"
	result.Supported = call.Function.Name == "probe_read_file" && result.ValidArguments
	if result.Supported {
		result.Recommendation = "Safe candidate: this profile preserved structured tool_calls with constrained arguments. You can consider enabling an opt-in grammar_tool_args flag after a few repeat passes."
	} else {
		result.Recommendation = fmt.Sprintf("Unsafe for tool arguments: expected probe_read_file with path probe.txt, got %s with args %s.", result.ToolName, result.Arguments)
	}
	return result
}

func grammarProbeToolDef() llm.ToolDef {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "const": "probe.txt",
      "description": "The exact probe path."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "probe_read_file",
			Description: "Probe-only read_file shape test. The tool is never executed.",
			Parameters:  params,
		},
	}
}
