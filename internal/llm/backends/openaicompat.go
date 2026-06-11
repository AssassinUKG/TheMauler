// Package backends implements llm.Client for each supported inference server.
package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

// OpenAICompat implements llm.Client against any OpenAI-compatible /v1 API.
// It is the shared base used by both the llama.cpp and LM Studio backends.
type OpenAICompat struct {
	clientName     string
	baseURL        string
	modelID        string
	contextTokens  int
	apiKey         string
	thinkingKwargs bool // send chat_template_kwargs (llama.cpp only)
	loadKwargsJSON string
	specType       string
	specDraftNMax  int
	specDraftModel string
	httpClient     *http.Client
}

// newOpenAICompat is the internal constructor.
func newOpenAICompat(name, baseURL, modelID string, contextTokens int, apiKey string, thinkingKwargs bool) *OpenAICompat {
	return &OpenAICompat{
		clientName:     name,
		baseURL:        strings.TrimRight(baseURL, "/"),
		modelID:        modelID,
		contextTokens:  contextTokens,
		apiKey:         apiKey,
		thinkingKwargs: thinkingKwargs,
		httpClient:     &http.Client{Timeout: 0}, // no timeout — streaming responses can be long
	}
}

// NewOpenAICompatible creates a generic OpenAI-compatible client for local
// servers such as SGLang, vLLM, or custom bridges that do not expose native
// model-management APIs.
func NewOpenAICompatible(p settings.Profile) llm.Client {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8000/v1"
	}
	apiKey := ""
	if p.APIKeyEnv != "" {
		apiKey = os.Getenv(p.APIKeyEnv)
	}
	return newOpenAICompat("openai-compatible", baseURL, p.ModelID, p.CtxTokens, apiKey, false)
}

func (c *OpenAICompat) Name() string { return c.clientName }

// LoadModel asks backends that support model management to load the configured
// model with profile load-time settings.
func (c *OpenAICompat) LoadModel(ctx context.Context) error {
	if c.modelID == "" || c.contextTokens <= 0 {
		return nil
	}
	if c.clientName == "llamacpp" {
		return c.loadLlamaCppModel(ctx)
	}
	if c.clientName != "lmstudio" {
		return nil
	}
	nativeBase := strings.TrimSuffix(c.baseURL, "/v1")

	// Unload any other loaded models first so we never run two large models
	// simultaneously — LM Studio will happily load both if not told otherwise.
	models, err := c.listLMStudioModels(ctx, nativeBase)
	if err == nil && c.hasUsableLMStudioTarget(models) {
		return nil
	}
	if err == nil {
		_ = c.unloadLMStudioModels(ctx, nativeBase, models)
	}
	body := map[string]interface{}{
		"model":            c.modelID,
		"context_length":   c.contextTokens,
		"echo_load_config": true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/v1/models/load", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("load model HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func (c *OpenAICompat) loadLlamaCppModel(ctx context.Context) error {
	body := map[string]interface{}{
		"model":            c.modelID,
		"context_size":     c.contextTokens,
		"echo_load_config": true,
	}
	if c.loadKwargsJSON != "" {
		body["chat_template_kwargs_json"] = c.loadKwargsJSON
	}
	if c.specType != "" {
		body["spec_type"] = c.specType
	}
	if c.specDraftNMax > 0 {
		body["spec_draft_n_max"] = c.specDraftNMax
	}
	if strings.TrimSpace(c.specDraftModel) != "" {
		body["draft_model_path"] = c.specDraftModel
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/models/load", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("llamacpp load model HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type lmStudioModelInfo struct {
	Key             string
	SelectedVariant string
	Variants        []string
	LoadedInstances []lmStudioInstanceInfo
}

type lmStudioInstanceInfo struct {
	ID            string
	ContextLength int
}

func (c *OpenAICompat) listLMStudioModels(ctx context.Context, nativeBase string) ([]lmStudioModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list models HTTP %d", resp.StatusCode)
	}
	var result struct {
		Models []struct {
			Key             string   `json:"key"`
			SelectedVariant string   `json:"selected_variant"`
			Variants        []string `json:"variants"`
			LoadedInstances []struct {
				ID     string `json:"id"`
				Config struct {
					ContextLength int `json:"context_length"`
				} `json:"config"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	models := make([]lmStudioModelInfo, 0, len(result.Models))
	for _, model := range result.Models {
		info := lmStudioModelInfo{
			Key:             model.Key,
			SelectedVariant: model.SelectedVariant,
			Variants:        model.Variants,
		}
		for _, instance := range model.LoadedInstances {
			info.LoadedInstances = append(info.LoadedInstances, lmStudioInstanceInfo{
				ID:            instance.ID,
				ContextLength: instance.Config.ContextLength,
			})
		}
		models = append(models, info)
	}
	return models, nil
}

func (c *OpenAICompat) hasUsableLMStudioTarget(models []lmStudioModelInfo) bool {
	for _, model := range models {
		if !c.matchesLMStudioModel(model.Key, model.SelectedVariant, model.Variants) {
			continue
		}
		for _, instance := range model.LoadedInstances {
			if c.usableContext(instance.ContextLength) {
				return true
			}
		}
	}
	return false
}

func (c *OpenAICompat) usableContext(ctxLen int) bool {
	return c.contextTokens <= 0 || ctxLen == 0 || ctxLen >= c.contextTokens
}

func (c *OpenAICompat) unloadLMStudioModels(ctx context.Context, nativeBase string, models []lmStudioModelInfo) error {
	for _, model := range models {
		if len(model.LoadedInstances) == 0 {
			continue
		}
		_ = c.unloadLMStudioModel(ctx, nativeBase, model.Key)
	}
	return nil
}

// unloadOtherLMStudioModels unloads every loaded model that is NOT the target
// model, preventing multiple large models from coexisting in VRAM.
func (c *OpenAICompat) unloadOtherLMStudioModels(ctx context.Context, nativeBase string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/v1/models", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("list models HTTP %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Key             string `json:"key"`
			SelectedVariant string `json:"selected_variant"`
			LoadedInstances []struct {
				ID string `json:"id"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	want := normalizeModelID(c.modelID)
	for _, model := range result.Models {
		if len(model.LoadedInstances) == 0 {
			continue
		}
		if normalizeModelID(model.Key) == want {
			continue
		}
		// This model is loaded but is NOT our target — unload it.
		_ = c.unloadLMStudioModel(ctx, nativeBase, model.Key)
	}
	return nil
}

func (c *OpenAICompat) unloadLMStudioModel(ctx context.Context, nativeBase, modelKey string) error {
	body, _ := json.Marshal(map[string]string{"model": modelKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nativeBase+"/api/v1/models/unload", bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *OpenAICompat) isLMStudioModelLoaded(ctx context.Context, nativeBase string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeBase+"/api/v1/models", nil)
	if err != nil {
		return false, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("list models HTTP %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Key             string   `json:"key"`
			SelectedVariant string   `json:"selected_variant"`
			Variants        []string `json:"variants"`
			LoadedInstances []struct {
				ID     string `json:"id"`
				Config struct {
					ContextLength int `json:"context_length"`
				} `json:"config"`
			} `json:"loaded_instances"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}
	for _, model := range result.Models {
		if !c.matchesLMStudioModel(model.Key, model.SelectedVariant, model.Variants) {
			continue
		}
		for _, instance := range model.LoadedInstances {
			if c.contextTokens > 0 && instance.Config.ContextLength != c.contextTokens {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

// ActualContextLength queries the LM Studio native API and returns the context
// length that the target model is currently loaded with. Returns 0 for non-LM Studio
// backends, or if the model is not loaded, or if the query fails.
func (c *OpenAICompat) ActualContextLength(ctx context.Context) int {
	if c.clientName == "llamacpp" {
		actual, _ := c.actualLlamaContext(ctx)
		return actual
	}
	if c.clientName != "lmstudio" {
		return 0
	}
	nativeBase := strings.TrimSuffix(c.baseURL, "/v1")
	models, err := c.listLMStudioModels(ctx, nativeBase)
	if err != nil {
		return 0
	}
	for _, model := range models {
		if !c.matchesLMStudioModel(model.Key, model.SelectedVariant, model.Variants) {
			continue
		}
		for _, instance := range model.LoadedInstances {
			if instance.ContextLength > 0 {
				return instance.ContextLength
			}
		}
	}
	return 0
}

func (c *OpenAICompat) actualLlamaContext(ctx context.Context) (int, error) {
	base := strings.TrimSuffix(c.baseURL, "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/props", nil)
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("props HTTP %d", resp.StatusCode)
	}
	var props struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&props); err != nil {
		return 0, err
	}
	if props.DefaultGenerationSettings.NCtx > 0 {
		return props.DefaultGenerationSettings.NCtx, nil
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/health", nil)
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var health struct {
		KVCache struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"kv_cache"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return 0, err
	}
	return health.KVCache.TotalTokens, nil
}

func (c *OpenAICompat) matchesLMStudioModel(key, selectedVariant string, variants []string) bool {
	want := normalizeModelID(c.modelID)
	candidates := []string{key, selectedVariant}
	candidates = append(candidates, variants...)
	for _, candidate := range candidates {
		got := normalizeModelID(candidate)
		if got == want || strings.HasPrefix(got, want+":") || strings.HasPrefix(got, want+"@") {
			return true
		}
	}
	return false
}

func normalizeModelID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.TrimSuffix(id, "/")
	if i := strings.Index(id, "@"); i >= 0 {
		id = id[:i]
	}
	return id
}

// Ping checks that the backend is reachable within 3 s.
// Uses HEAD /models to avoid triggering a visible "List Models" log entry
// in LM Studio while still verifying the API is responding.
func (c *OpenAICompat) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", c.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Fall back to GET if HEAD is not supported
		req2, err2 := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
		if err2 != nil {
			return err
		}
		c.setHeaders(req2)
		resp, err = c.httpClient.Do(req2)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("backend returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// Models returns the list of model IDs reported by the backend.
func (c *OpenAICompat) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	ids := make([]string, len(result.Data))
	for i, m := range result.Data {
		ids[i] = m.ID
	}
	return ids, nil
}

// Chat sends a streaming chat completion request.
func (c *OpenAICompat) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	bodyBytes, err := c.buildBody(req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan llm.Delta, 128)
	go func() {
		defer close(ch)
		done := make(chan struct{})
		go c.watchStreamingCancel(ctx, resp.Body, done)
		defer close(done)
		defer resp.Body.Close()
		llm.ParseSSE(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (c *OpenAICompat) watchStreamingCancel(ctx context.Context, body io.Closer, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		_ = body.Close()
		c.cancelActiveInference()
	case <-done:
	}
}

func (c *OpenAICompat) cancelActiveInference() {
	if c.clientName != "llamacpp" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/inference/cancel", nil)
	if err != nil {
		return
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// --- request building ---

type chatReqBody struct {
	Model           string        `json:"model,omitempty"`
	Messages        []apiMessage  `json:"messages"`
	Stream          bool          `json:"stream"`
	MaxTokens       int           `json:"max_tokens,omitempty"`
	Temperature     float64       `json:"temperature,omitempty"`
	TopP            float64       `json:"top_p,omitempty"`
	TopK            int           `json:"top_k,omitempty"`
	MinP            float64       `json:"min_p,omitempty"`
	PresencePenalty float64       `json:"presence_penalty,omitempty"`
	Seed            *int64        `json:"seed,omitempty"`
	Tools           []llm.ToolDef `json:"tools,omitempty"`
	// tool_choice: "auto" | "none" | "required" — omitted when empty
	ToolChoice        string         `json:"tool_choice,omitempty"`
	StreamOptions     *streamOptions `json:"stream_options,omitempty"`
	ParallelToolCalls *bool          `json:"parallel_tool_calls,omitempty"`
	ParseToolCalls    *bool          `json:"parse_tool_calls,omitempty"`
	// llama.cpp thinking-mode control
	ChatTemplateKwargs *chatTemplateKwargs `json:"chat_template_kwargs,omitempty"`
	// Structured output
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatTemplateKwargs struct {
	EnableThinking   *bool  `json:"enable_thinking,omitempty"`
	PreserveThinking *bool  `json:"preserve_thinking,omitempty"`
	SpecType         string `json:"spec_type,omitempty"`
	SpecDraftNMax    int    `json:"spec_draft_n_max,omitempty"`
}

type responseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *jsonSchemaSpec `json:"json_schema,omitempty"`
}

type jsonSchemaSpec struct {
	Schema json.RawMessage `json:"schema"`
}

type apiMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // string or []ContentBlock
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  interface{} `json:"tool_calls,omitempty"`
	Name       string      `json:"name,omitempty"`
}

type apiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function apiFunctionCall `json:"function"`
}

type apiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (c *OpenAICompat) buildBody(req llm.Request) ([]byte, error) {
	msgs := buildMessages(req)

	body := chatReqBody{
		Model:           c.modelID,
		Messages:        msgs,
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		PresencePenalty: req.PresencePenalty,
	}

	if req.TopK > 0 {
		body.TopK = req.TopK
	}
	if req.MinP > 0 {
		body.MinP = req.MinP
	}
	if req.Seed >= 0 {
		s := req.Seed
		body.Seed = &s
	}
	if len(req.Tools) > 0 {
		body.Tools = req.Tools
		parallel := false
		body.ParallelToolCalls = &parallel
		if req.ToolChoice != "" {
			body.ToolChoice = req.ToolChoice
		}
		if c.thinkingKwargs {
			parse := true
			body.ParseToolCalls = &parse
		}
	}

	// thinking kwargs + MTP — llama.cpp only
	if c.thinkingKwargs {
		enable := req.EnableThinking
		preserve := req.PreserveThinking
		body.ChatTemplateKwargs = &chatTemplateKwargs{
			EnableThinking:   &enable,
			PreserveThinking: &preserve,
			SpecType:         req.SpecType,
			SpecDraftNMax:    req.SpecDraftNMax,
		}
	}

	// grammar-constrained structured output
	if req.JSONSchema != nil {
		body.ResponseFormat = &responseFormat{
			Type:       "json_schema",
			JSONSchema: &jsonSchemaSpec{Schema: req.JSONSchema},
		}
	}

	return json.Marshal(body)
}

func buildMessages(req llm.Request) []apiMessage {
	var msgs []apiMessage

	if req.System != "" {
		msgs = append(msgs, apiMessage{Role: llm.RoleSystem, Content: req.System})
	}

	for _, m := range req.Messages {
		am := apiMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		if len(m.ToolCalls) > 0 {
			am.ToolCalls = apiToolCalls(m.ToolCalls)
		}
		msgs = append(msgs, am)
	}
	return msgs
}

func apiToolCalls(calls []llm.ToolCallDef) []apiToolCall {
	out := make([]apiToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, apiToolCall{
			ID:   call.ID,
			Type: call.Type,
			Function: apiFunctionCall{
				Name:      call.Function.Name,
				Arguments: string(call.Function.Arguments),
			},
		})
	}
	return out
}

func (c *OpenAICompat) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
