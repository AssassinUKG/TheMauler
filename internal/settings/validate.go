package settings

import (
	"fmt"
	"strings"
)

const (
	defaultCompactionAt = 0.85
	defaultMaxToolCalls = 200
	maxToolCallsLimit   = 2000
	defaultCtxTokens    = 8192
	defaultMaxTokens    = 2048
)

// Validate clamps dangerous or out-of-range settings to safe values. It never
// errors because settings load should always produce a usable configuration.
func (s *Settings) Validate() []string {
	if s == nil {
		return nil
	}
	var adjustments []string
	if s.Context.CompactionAt <= 0 || s.Context.CompactionAt >= 1 {
		adjustments = append(adjustments, fmt.Sprintf("context.compaction_at clamped from %v to %.2f", s.Context.CompactionAt, defaultCompactionAt))
		s.Context.CompactionAt = defaultCompactionAt
	}
	if s.Agents.MaxToolCalls <= 0 {
		adjustments = append(adjustments, fmt.Sprintf("agents.max_tool_calls clamped from %d to %d", s.Agents.MaxToolCalls, defaultMaxToolCalls))
		s.Agents.MaxToolCalls = defaultMaxToolCalls
	} else if s.Agents.MaxToolCalls > maxToolCallsLimit {
		adjustments = append(adjustments, fmt.Sprintf("agents.max_tool_calls clamped from %d to %d", s.Agents.MaxToolCalls, maxToolCallsLimit))
		s.Agents.MaxToolCalls = maxToolCallsLimit
	}
	if s.Agents.MaxRunSeconds < 0 {
		adjustments = append(adjustments, fmt.Sprintf("agents.max_run_seconds clamped from %d to 0", s.Agents.MaxRunSeconds))
		s.Agents.MaxRunSeconds = 0
	}
	switch strings.ToLower(strings.TrimSpace(s.Agents.ReasoningEffort)) {
	case "", "auto":
		s.Agents.ReasoningEffort = "auto"
	case "minimal", "low", "medium", "high":
		s.Agents.ReasoningEffort = strings.ToLower(strings.TrimSpace(s.Agents.ReasoningEffort))
	default:
		adjustments = append(adjustments, fmt.Sprintf("agents.reasoning_effort reset from %q to auto", s.Agents.ReasoningEffort))
		s.Agents.ReasoningEffort = "auto"
	}
	return adjustments
}

// Validate clamps profile generation settings and records profiles that point at
// unknown providers. Unknown providers are left intact so Doctor can surface the
// wiring problem without destroying user intent.
func (pf *ProfilesFile) Validate() []string {
	if pf == nil {
		return nil
	}
	var adjustments []string
	for name, profile := range pf.Profiles {
		label := name
		if profile.Name != "" {
			label = profile.Name
		}
		if profile.CtxTokens <= 0 {
			adjustments = append(adjustments, fmt.Sprintf("profiles.%s.ctx_tokens clamped from %d to %d", label, profile.CtxTokens, defaultCtxTokens))
			profile.CtxTokens = defaultCtxTokens
		}
		if profile.Provider != "" && pf.Providers != nil {
			if _, ok := pf.Providers[profile.Provider]; !ok {
				adjustments = append(adjustments, fmt.Sprintf("profiles.%s.provider references unknown provider %q", label, profile.Provider))
			}
		}
		profile.ThinkGeneral, adjustments = validateGenerationParams(label, "thinking_general", profile.CtxTokens, profile.ThinkGeneral, adjustments)
		profile.ThinkCoding, adjustments = validateGenerationParams(label, "thinking_coding", profile.CtxTokens, profile.ThinkCoding, adjustments)
		profile.NoThink, adjustments = validateGenerationParams(label, "nothinking", profile.CtxTokens, profile.NoThink, adjustments)
		pf.Profiles[name] = profile
	}
	return adjustments
}

func validateGenerationParams(profileName, preset string, ctxTokens int, params GenerationParams, adjustments []string) (GenerationParams, []string) {
	prefix := fmt.Sprintf("profiles.%s.%s", profileName, preset)
	if params.MaxTokens <= 0 {
		adjustments = append(adjustments, fmt.Sprintf("%s.max_tokens clamped from %d to %d", prefix, params.MaxTokens, defaultMaxTokens))
		params.MaxTokens = defaultMaxTokens
	}
	if ctxTokens > 0 {
		halfCtx := ctxTokens / 2
		if halfCtx < 1 {
			halfCtx = 1
		}
		if params.MaxTokens > halfCtx {
			adjustments = append(adjustments, fmt.Sprintf("%s.max_tokens clamped from %d to %d", prefix, params.MaxTokens, halfCtx))
			params.MaxTokens = halfCtx
		}
	}
	if params.Temperature < 0 {
		adjustments = append(adjustments, fmt.Sprintf("%s.temperature clamped from %v to 0", prefix, params.Temperature))
		params.Temperature = 0
	} else if params.Temperature > 2 {
		adjustments = append(adjustments, fmt.Sprintf("%s.temperature clamped from %v to 2", prefix, params.Temperature))
		params.Temperature = 2
	}
	if params.TopP <= 0 {
		adjustments = append(adjustments, fmt.Sprintf("%s.top_p clamped from %v to 1", prefix, params.TopP))
		params.TopP = 1
	} else if params.TopP > 1 {
		adjustments = append(adjustments, fmt.Sprintf("%s.top_p clamped from %v to 1", prefix, params.TopP))
		params.TopP = 1
	}
	if params.TopK < 0 {
		adjustments = append(adjustments, fmt.Sprintf("%s.top_k clamped from %d to 0", prefix, params.TopK))
		params.TopK = 0
	}
	if params.MinP < 0 {
		adjustments = append(adjustments, fmt.Sprintf("%s.min_p clamped from %v to 0", prefix, params.MinP))
		params.MinP = 0
	} else if params.MinP > 1 {
		adjustments = append(adjustments, fmt.Sprintf("%s.min_p clamped from %v to 1", prefix, params.MinP))
		params.MinP = 1
	}
	return params, adjustments
}
