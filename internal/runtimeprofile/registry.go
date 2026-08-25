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
	Temperature     float64 `json:"temperature"`
	TopP            float64 `json:"top_p"`
	TopK            int     `json:"top_k"`
	MinP            float64 `json:"min_p"`
	PresencePenalty float64 `json:"presence_penalty"`
	RepeatPenalty   float64 `json:"repeat_penalty"`
}

// RuntimeProfile is a stable capability record for a model family.
type RuntimeProfile struct {
	Name            string   `json:"name"`
	Family          string   `json:"family"`
	HuggingFaceRepo string   `json:"huggingface_repo,omitempty"`
	MatchAliases    []string `json:"match_aliases,omitempty"`
	Backend         string   `json:"backend"`
	Quant           string   `json:"quant"`
	Adapter         string   `json:"adapter"`
	ToolProtocol    string   `json:"tool_protocol"` // native-openai | repair-text | mixed
	ChatTemplate    string   `json:"chat_template"` // embedded-gguf-jinja | runtime-default
	RequiresJinja   bool     `json:"requires_jinja"`
	AliasOnly       bool     `json:"-"`
	Supports        Supports `json:"supports"`
	Defaults        Defaults `json:"defaults"`
	RecommendedCtx  int      `json:"recommended_ctx"`
	KVTypeK         string   `json:"kv_type_k"`
	KVTypeV         string   `json:"kv_type_v"`
}

// Registry returns built-in runtime profiles. Disk-loaded registries can layer
// over this later without changing Doctor or routing call sites.
func Registry() []RuntimeProfile {
	return []RuntimeProfile{
		{
			Name:            "qwen3.8-27b",
			Family:          "qwen3.8",
			HuggingFaceRepo: "Qwen/Qwen3.8-27B",
			MatchAliases: []string{
				"Qwen3.8-27B-Q4_K_M",
				"Qwen3.8-27B-Uncensored-Q4_K_M",
			},
			Backend:       "llama.cpp",
			Quant:         "Q4_K_M",
			Adapter:       "qwen38",
			ToolProtocol:  "native-openai",
			ChatTemplate:  "embedded-gguf-jinja",
			RequiresJinja: true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:     0.7,
				TopP:            0.8,
				TopK:            20,
				MinP:            0.0,
				PresencePenalty: 1.5,
				RepeatPenalty:   1.0,
			},
			// The model supports much more natively, but 35K is the verified
			// quality/VRAM point for this RTX 3090 and leaves room for F16 KV.
			RecommendedCtx: 35000,
			KVTypeK:        "f16",
			KVTypeV:        "f16",
		},
		{
			Name:            "qwen3.6-27b-unsloth-ud-q4-k-xl",
			Family:          "qwen3.6",
			HuggingFaceRepo: "unsloth/Qwen3.6-27B-GGUF",
			MatchAliases:    []string{"Qwen3.6-27B-UD-Q4_K_XL"},
			AliasOnly:       true,
			Backend:         "llama.cpp",
			Quant:           "UD-Q4_K_XL",
			Adapter:         "qwen36",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.2,
				TopP:          0.95,
				TopK:          20,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 35000,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "qwen3.6-27b-huihui-abliterated-mtp",
			Family:          "qwen3.6",
			HuggingFaceRepo: "huihui-ai/Huihui-Qwen3.6-27B-abliterated-MTP-GGUF",
			MatchAliases:    []string{"Huihui-Qwen3.6-27B-abliterated-ggml-model"},
			AliasOnly:       true,
			Backend:         "llama.cpp",
			Quant:           "Q4_K",
			Adapter:         "qwen36",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.2,
				TopP:          0.95,
				TopK:          20,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 35000,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "qwen3.6-27b-hauhaucs-aggressive",
			Family:          "qwen3.6",
			HuggingFaceRepo: "HauhauCS/Qwen3.6-27B-Uncensored-HauhauCS-Aggressive",
			MatchAliases:    []string{"Qwen3.6-27B-Uncensored-HauhauCS-Aggressive"},
			AliasOnly:       true,
			Backend:         "llama.cpp",
			Quant:           "Q4_K_P",
			Adapter:         "qwen36",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.2,
				TopP:          0.95,
				TopK:          20,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 40000,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "qwen3.6-35b-a3b-heretic-native-mtp",
			Family:          "qwen3.6",
			HuggingFaceRepo: "llmfan46/Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved-GGUF",
			MatchAliases:    []string{"Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved"},
			AliasOnly:       true,
			Backend:         "llama.cpp",
			Quant:           "Q4_K_M",
			Adapter:         "qwen36",
			ToolProtocol:    "repair-text",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.2,
				TopP:          0.95,
				TopK:          20,
				MinP:          0.0,
				RepeatPenalty: 1.05,
			},
			RecommendedCtx: 16384,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "qwen3.6-27b",
			Family:          "qwen3.6",
			HuggingFaceRepo: "unsloth/Qwen3.6-27B-GGUF",
			MatchAliases:    []string{"qwen3.6-27b-fable-fus-711-unheretic-nm-dau-neo-max-neo"},
			Backend:         "llama.cpp",
			Quant:           "Q4/Q5-class",
			Adapter:         "qwen36",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.2,
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
			Name:            "qwen3.5-qwythos-9b-1m",
			Family:          "qwen3.5",
			HuggingFaceRepo: "empero-ai/Qwythos-9B-Claude-Mythos-5-1M-GGUF",
			MatchAliases:    []string{"Qwythos-9B-Claude-Mythos-5-1M"},
			AliasOnly:       true,
			Backend:         "llama.cpp",
			Quant:           "Q4/Q5-class",
			Adapter:         "qwen35",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
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
			RecommendedCtx: 65536,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "qwen3.5-4b",
			Family:          "qwen3.5",
			HuggingFaceRepo: "Qwen/Qwen3.5-4B",
			Backend:         "llama.cpp",
			Quant:           "Q4/Q5-class",
			Adapter:         "qwen35",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:     0.7,
				TopP:            0.8,
				TopK:            20,
				MinP:            0.0,
				PresencePenalty: 1.5,
				RepeatPenalty:   1.0,
			},
			RecommendedCtx: 65536,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "gemma4-12b",
			Family:          "gemma4",
			HuggingFaceRepo: "google/gemma-4-12B-it",
			MatchAliases:    []string{"gemma-4-12B-it"},
			Backend:         "llama.cpp",
			Quant:           "Q4/Q5-class",
			Adapter:         "gemma4",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      false,
			},
			Defaults: Defaults{
				Temperature:   1.0,
				TopP:          0.95,
				TopK:          64,
				MinP:          0.0,
				RepeatPenalty: 1.0,
			},
			RecommendedCtx: 65536,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "gemma4-e4b",
			Family:          "gemma4",
			HuggingFaceRepo: "google/gemma-4-E4B-it",
			MatchAliases:    []string{"gemma-4-E4B-it"},
			Backend:         "llama.cpp",
			Quant:           "Q4/Q8-class",
			Adapter:         "gemma4",
			ToolProtocol:    "native-openai",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: true,
				MTP:      false,
			},
			Defaults: Defaults{
				Temperature:   1.0,
				TopP:          0.95,
				TopK:          64,
				MinP:          0.0,
				RepeatPenalty: 1.0,
			},
			RecommendedCtx: 65536,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "gemma4-26b-a4b-qat-hauhaucs-balanced",
			Family:          "gemma4",
			HuggingFaceRepo: "HauhauCS/Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-MTP",
			MatchAliases: []string{
				"Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced",
				"HauhauCS/Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-MTP",
			},
			AliasOnly:     true,
			Backend:       "llama.cpp",
			Quant:         "Q4_K_M",
			Adapter:       "gemma4",
			ToolProtocol:  "mixed",
			ChatTemplate:  "embedded-gguf-jinja",
			RequiresJinja: true,
			Supports: Supports{
				Tools:    true,
				Thinking: false,
				MTP:      true,
			},
			Defaults: Defaults{
				Temperature:   0.6,
				TopP:          0.9,
				TopK:          64,
				MinP:          0.05,
				RepeatPenalty: 1.1,
			},
			RecommendedCtx: 40000,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:            "gemma4-26b-a4b-qat",
			Family:          "gemma4",
			HuggingFaceRepo: "google/gemma-4-26B-A4B-it",
			Backend:         "llama.cpp",
			Quant:           "QAT Q4-class",
			Adapter:         "gemma4",
			ToolProtocol:    "mixed",
			ChatTemplate:    "embedded-gguf-jinja",
			RequiresJinja:   true,
			Supports: Supports{
				Tools:    true,
				Thinking: false,
				MTP:      false,
			},
			Defaults: Defaults{
				Temperature:   1.0,
				TopP:          0.95,
				TopK:          64,
				MinP:          0.05,
				RepeatPenalty: 1.0,
			},
			RecommendedCtx: 49152,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
		{
			Name:          "gemma4-31b",
			Family:        "gemma4",
			Backend:       "llama.cpp",
			Quant:         "Q4_K_S",
			Adapter:       "gemma4",
			ToolProtocol:  "repair-text",
			ChatTemplate:  "embedded-gguf-jinja",
			RequiresJinja: true,
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
			RecommendedCtx: 32768,
			KVTypeK:        "q8_0",
			KVTypeV:        "q8_0",
		},
	}
}

// Match returns the best known runtime profile for the configured model.
func Match(profile settings.Profile) (RuntimeProfile, bool) {
	// The model id is authoritative. Model Matrix intentionally reuses one
	// profile's generation defaults while swapping model ids, so matching the
	// profile name first can apply (for example) Qwen metadata to a Gemma model.
	if modelHaystack := strings.ToLower(strings.TrimSpace(profile.ModelID)); modelHaystack != "" {
		if rp, ok := matchRuntimeHaystack(modelHaystack); ok {
			return rp, true
		}
	}
	return matchRuntimeHaystack(strings.ToLower(profile.Name + " " + profile.ModelID))
}

func matchRuntimeHaystack(haystack string) (RuntimeProfile, bool) {
	normalizedHaystack := normalizeRuntimeNeedle(haystack)
	for _, rp := range Registry() {
		if runtimeProfileExactMatch(rp, haystack, normalizedHaystack) {
			return rp, true
		}
	}
	for _, rp := range Registry() {
		if rp.AliasOnly {
			continue
		}
		if strings.Contains(haystack, "qat") && !strings.Contains(strings.ToLower(rp.Name), "qat") {
			continue
		}
		if strings.Contains(haystack, strings.ToLower(rp.Name)) || strings.Contains(normalizedHaystack, normalizeRuntimeNeedle(rp.Name)) {
			return rp, true
		}
	}
	if strings.Contains(haystack, "qat") {
		for _, rp := range Registry() {
			if rp.AliasOnly {
				continue
			}
			if !strings.Contains(strings.ToLower(rp.Name), "qat") {
				continue
			}
			sizeNeedle := sizeNeedleFromName(rp.Name)
			if sizeNeedle == "" || !strings.Contains(haystack, sizeNeedle) {
				continue
			}
			for _, needle := range matchNeedles(rp.Family) {
				if strings.Contains(haystack, needle) {
					return rp, true
				}
			}
		}
	}
	for _, rp := range Registry() {
		if rp.AliasOnly {
			continue
		}
		sizeNeedle := sizeNeedleFromName(rp.Name)
		if sizeNeedle == "" || !strings.Contains(haystack, sizeNeedle) {
			continue
		}
		for _, needle := range matchNeedles(rp.Family) {
			if strings.Contains(haystack, needle) {
				return rp, true
			}
		}
	}
	for _, rp := range Registry() {
		if rp.AliasOnly {
			continue
		}
		for _, needle := range matchNeedles(rp.Family) {
			if strings.Contains(haystack, needle) {
				return rp, true
			}
		}
	}
	return RuntimeProfile{}, false
}

func runtimeProfileExactMatch(rp RuntimeProfile, haystack, normalizedHaystack string) bool {
	needles := append([]string{rp.Name}, rp.MatchAliases...)
	// Alias-only profiles describe a specific fine-tune or quant. Keep the repo as
	// provenance, but require one of its explicit aliases so a multi-quant HF repo
	// (for example Unsloth's Qwen3.6 GGUF collection) does not select a quant-specific
	// template merely from the repository id.
	if rp.HuggingFaceRepo != "" && !rp.AliasOnly {
		needles = append(needles, rp.HuggingFaceRepo)
	}
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if strings.Contains(haystack, strings.ToLower(needle)) || strings.Contains(normalizedHaystack, normalizeRuntimeNeedle(needle)) {
			return true
		}
	}
	return false
}

func normalizeRuntimeNeedle(text string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func sizeNeedleFromName(name string) string {
	for _, part := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}) {
		if len(part) > 1 && strings.HasSuffix(part, "b") {
			return part
		}
	}
	return ""
}

func matchNeedles(family string) []string {
	switch family {
	case "qwen3.8":
		return []string{"qwen3.8", "qwen-3.8", "qwen_3.8"}
	case "qwen3.6":
		return []string{"qwen3.6", "qwen-3.6", "qwen_3.6"}
	case "qwen3.5":
		return []string{"qwen3.5", "qwen-3.5", "qwen_3.5"}
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
