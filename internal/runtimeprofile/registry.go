package runtimeprofile

import (
	"strings"

	"mauler/internal/settings"
)

// Supports describes high-level model/runtime capabilities used by Doctor,
// routing, and future eval harnesses.
type Supports struct {
	Tools    bool `json:"tools"`
	Thinking bool `json:"thinking"`
	MTP      bool `json:"mtp"`
}

// Defaults describes conservative generation/runtime defaults for a model family.
type Defaults struct {
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
	TopK          int     `json:"top_k"`
	MinP          float64 `json:"min_p"`
	RepeatPenalty float64 `json:"repeat_penalty"`
}

// RuntimeProfile is a stable capability record for a model family.
type RuntimeProfile struct {
	Name           string   `json:"name"`
	Family         string   `json:"family"`
	Backend        string   `json:"backend"`
	Quant          string   `json:"quant"`
	Adapter        string   `json:"adapter"`
	ToolProtocol   string   `json:"tool_protocol"` // native-openai | repair-text | mixed
	Supports       Supports `json:"supports"`
	Defaults       Defaults `json:"defaults"`
	RecommendedCtx int      `json:"recommended_ctx"`
	KVTypeK        string   `json:"kv_type_k"`
	KVTypeV        string   `json:"kv_type_v"`
}

// Registry returns built-in runtime profiles. Disk-loaded registries can layer
// over this later without changing Doctor or routing call sites.
func Registry() []RuntimeProfile {
	return []RuntimeProfile{
		{
			Name:         "qwen3.6-27b",
			Family:       "qwen3.6",
			Backend:      "llama.cpp",
			Quant:        "UD-Q4_K_XL",
			Adapter:      "qwen36",
			ToolProtocol: "native-openai",
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.6,
				TopP:          0.95,
				TopK:          20,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 32768,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:         "gemma4-31b",
			Family:       "gemma4",
			Backend:      "llama.cpp",
			Quant:        "Q4_K_S",
			Adapter:      "gemma4",
			ToolProtocol: "repair-text",
			Supports: Supports{
				Tools:    true,
				Thinking: false,
				MTP:      false,
			},
			Defaults: Defaults{
				Temperature:   0.7,
				TopP:          0.9,
				TopK:          40,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 65536,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
	}
}

// Match returns the best known runtime profile for the configured model.
func Match(profile settings.Profile) (RuntimeProfile, bool) {
	haystack := strings.ToLower(profile.Name + " " + profile.ModelID)
	for _, rp := range Registry() {
		for _, needle := range matchNeedles(rp.Family) {
			if strings.Contains(haystack, needle) {
				return rp, true
			}
		}
	}
	return RuntimeProfile{}, false
}

func matchNeedles(family string) []string {
	switch family {
	case "qwen3.6":
		return []string{"qwen3.6", "qwen-3.6", "qwen_3.6"}
	case "gemma4":
		return []string{"gemma4", "gemma-4", "gemma_4"}
	default:
		return []string{strings.ToLower(family)}
	}
}

// LooksMTPModel reports whether the model id/name appears to be an MTP-capable
// artifact. This is deliberately conservative because non-MTP GGUFs cannot use
// draft-mtp just because the model family has an MTP variant.
func LooksMTPModel(profile settings.Profile) bool {
	haystack := strings.ToLower(profile.Name + " " + profile.ModelID)
	return strings.Contains(haystack, "mtp")
}
