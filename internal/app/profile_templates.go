package app

import (
	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

// ModelProfileTemplateResult is the code-owned profile template selected for a
// local or Hugging Face model identifier. The chat template is descriptive:
// InferenceBridge/llama.cpp must use the Jinja embedded in the GGUF rather than
// Mauler inventing model tokens in React.
type ModelProfileTemplateResult struct {
	Matched         bool             `json:"matched"`
	TemplateID      string           `json:"template_id,omitempty"`
	Family          string           `json:"family,omitempty"`
	Adapter         string           `json:"adapter,omitempty"`
	ToolProtocol    string           `json:"tool_protocol,omitempty"`
	ChatTemplate    string           `json:"chat_template,omitempty"`
	RequiresJinja   bool             `json:"requires_jinja,omitempty"`
	HuggingFaceRepo string           `json:"huggingface_repo,omitempty"`
	Profile         settings.Profile `json:"profile"`
	Notes           []string         `json:"notes"`
}

// RecommendModelProfileTemplate applies conservative family/variant defaults
// without loading the model. The Providers UI uses it as soon as a local/HF
// catalogue model is selected; Benchmark remains the live verification step.
func (a *App) RecommendModelProfileTemplate(profile settings.Profile) ModelProfileTemplateResult {
	recommended, notes := recommendProfileSettings(profile)
	result := ModelProfileTemplateResult{
		Profile: recommended,
		Notes:   notes,
	}
	rp, ok := runtimeprofile.Match(profile)
	if !ok {
		return result
	}
	result.Matched = true
	result.TemplateID = rp.Name
	result.Family = rp.Family
	result.Adapter = rp.Adapter
	result.ToolProtocol = rp.ToolProtocol
	result.ChatTemplate = rp.ChatTemplate
	result.RequiresJinja = rp.RequiresJinja
	result.HuggingFaceRepo = rp.HuggingFaceRepo
	return result
}
