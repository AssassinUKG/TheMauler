package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestOpenAICompatibleUsesUIManagedProviderSecret(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := settings.SaveProviderAPIKey("openrouter", "sk-or-test"); err != nil {
		t.Fatal(err)
	}
	client := NewOpenAICompatible(settings.Profile{
		Provider:  "openrouter",
		Backend:   "openai-compatible",
		BaseURL:   "https://openrouter.ai/api/v1",
		ModelID:   "vendor/frontier-model",
		APIKeyEnv: "OPENROUTER_API_KEY",
	})
	compat, ok := client.(*OpenAICompat)
	if !ok {
		t.Fatalf("client type = %T", client)
	}
	if compat.apiKey != "sk-or-test" {
		t.Fatalf("api key was not resolved from the provider secret store")
	}
}

func TestOpenAICompatPingFallsBackToAuthenticatedGetWhenHeadIsRejected(t *testing.T) {
	var headCalls, getCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-or-test" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodHead:
			headCalls.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			getCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("openai-compatible", server.URL, "frontier-model", 32768, "sk-or-test", false)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if headCalls.Load() != 1 || getCalls.Load() != 1 {
		t.Fatalf("HEAD calls = %d, GET calls = %d", headCalls.Load(), getCalls.Load())
	}
}

func TestOpenAICompatModelMetadataUsesSafeProviderLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"moonshotai/kimi-k3",
			"context_length":1048576,
			"max_completion_tokens":32768,
			"supported_parameters":["max_tokens","tools"],
			"top_provider":{"context_length":262144,"max_completion_tokens":16384}
		},{
			"id":"local/model"
		}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat("openai-compatible", server.URL, "", 0, "", false)
	models, err := client.ModelMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	if got := models[0]; got.ID != "moonshotai/kimi-k3" || got.ContextLength != 262_144 || got.MaxCompletionTokens != 16_384 {
		t.Fatalf("metadata = %#v", got)
	}
	if len(models[0].SupportedParameters) != 2 {
		t.Fatalf("supported parameters = %#v", models[0].SupportedParameters)
	}
	if models[1].ContextLength != 0 || models[1].MaxCompletionTokens != 0 {
		t.Fatalf("missing limits should remain unknown: %#v", models[1])
	}

	ids, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "moonshotai/kimi-k3" || ids[1] != "local/model" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestOpenAICompatBuildBodyIncludesProfileRequestSettings(t *testing.T) {
	client := newOpenAICompat("llamacpp", "http://example.test/v1", "qwen-local", 32768, "", true)
	seed := int64(42)
	bodyBytes, err := client.buildBody(llm.Request{
		Messages:         []llm.Message{llm.NewTextMessage(llm.RoleUser, "hello")},
		MaxTokens:        9000,
		Temperature:      0.6,
		TopP:             0.95,
		TopK:             20,
		MinP:             0.05,
		PresencePenalty:  1.5,
		RepeatPenalty:    1.1,
		Seed:             seed,
		EnableThinking:   true,
		PreserveThinking: true,
		Tools: []llm.ToolDef{{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:       "web_search",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	assertJSONNumber(t, got, "max_tokens", 9000)
	assertJSONNumber(t, got, "temperature", 0.6)
	assertJSONNumber(t, got, "top_p", 0.95)
	assertJSONNumber(t, got, "top_k", 20)
	assertJSONNumber(t, got, "min_p", 0.05)
	assertJSONNumber(t, got, "presence_penalty", 1.5)
	assertJSONNumber(t, got, "repeat_penalty", 1.1)
	assertJSONNumber(t, got, "seed", 42)
	if got["model"] != "qwen-local" {
		t.Fatalf("model = %v, want qwen-local", got["model"])
	}
	if _, ok := got["context_size"]; ok {
		t.Fatalf("chat payload must not include context_size; model loading owns context: %#v", got)
	}
	if _, ok := got["tools"].([]interface{}); !ok {
		t.Fatalf("tools missing from payload: %#v", got["tools"])
	}
	streamOptions, ok := got["stream_options"].(map[string]interface{})
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage missing: %#v", got["stream_options"])
	}
	if got["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false", got["parallel_tool_calls"])
	}
	if got["parse_tool_calls"] != true {
		t.Fatalf("parse_tool_calls = %#v, want true for llama.cpp", got["parse_tool_calls"])
	}
	kwargs, ok := got["chat_template_kwargs"].(map[string]interface{})
	if !ok {
		t.Fatalf("chat_template_kwargs missing: %#v", got)
	}
	if kwargs["enable_thinking"] != true || kwargs["preserve_thinking"] != true {
		t.Fatalf("thinking kwargs wrong: %#v", kwargs)
	}
}

func TestOpenAICompatBuildBodyCarriesPreservedReasoningOnlyForThinkingBackend(t *testing.T) {
	message := llm.NewTextMessage(llm.RoleAssistant, "I will inspect the file.")
	message.ReasoningContent = "The file is the narrowest evidence path."
	req := llm.Request{
		Messages:        []llm.Message{llm.NewTextMessage(llm.RoleUser, "continue"), message},
		ReasoningEffort: "xhigh",
	}

	local := newOpenAICompat("llamacpp", "http://example.test/v1", "Qwen3.8-27B", 35000, "", true)
	bodyBytes, err := local.buildBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	messages := got["messages"].([]interface{})
	assistant := messages[1].(map[string]interface{})
	if assistant["reasoning_content"] != message.ReasoningContent || got["reasoning_effort"] != "xhigh" {
		t.Fatalf("preserved Qwen reasoning missing: body=%#v", got)
	}

	cloud := newOpenAICompat("openai-compatible", "http://example.test/v1", "frontier", 35000, "", false)
	bodyBytes, err = cloud.buildBody(req)
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]interface{}{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	messages = got["messages"].([]interface{})
	assistant = messages[1].(map[string]interface{})
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("generic provider should not receive local preserved-thinking fields: %#v", assistant)
	}
}

func TestOpenAICompatBuildBodyRequestsUsageWithoutTools(t *testing.T) {
	client := newOpenAICompat("lmstudio", "http://example.test/v1", "qwen-local", 32768, "", false)
	bodyBytes, err := client.buildBody(llm.Request{
		Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	streamOptions, ok := got["stream_options"].(map[string]interface{})
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage missing: %#v", got["stream_options"])
	}
	if _, ok := got["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls should only be sent with tools: %#v", got)
	}
	if _, ok := got["parse_tool_calls"]; ok {
		t.Fatalf("parse_tool_calls should only be sent for llama.cpp tool requests: %#v", got)
	}
}

func TestOpenAICompatBuildBodySerializesToolCallArgumentsAsString(t *testing.T) {
	client := newOpenAICompat("llamacpp", "http://example.test/v1", "qwen-local", 32768, "", true)
	bodyBytes, err := client.buildBody(llm.Request{
		Messages: []llm.Message{{
			Role:    llm.RoleAssistant,
			Content: "",
			ToolCalls: []llm.ToolCallDef{{
				ID:   "call1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "glob",
					Arguments: json.RawMessage(`{"pattern":"**/*.go"}`),
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	messages := got["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	calls := msg["tool_calls"].([]interface{})
	call := calls[0].(map[string]interface{})
	fn := call["function"].(map[string]interface{})
	if _, ok := fn["arguments"].(string); !ok {
		t.Fatalf("function.arguments must be a JSON string, got %#v", fn["arguments"])
	}
}

func TestOpenAICompatBuildBodyIncludesJSONSchemaResponseFormat(t *testing.T) {
	client := newOpenAICompat("llamacpp", "http://example.test/v1", "gemma-local", 32768, "", true)
	bodyBytes, err := client.buildBody(llm.Request{
		Messages:   []llm.Message{llm.NewTextMessage(llm.RoleUser, "call read_file")},
		ToolChoice: "required",
		Tools: []llm.ToolDef{{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:       "read_file",
				Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
			},
		}},
		JSONSchema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	format, ok := got["response_format"].(map[string]interface{})
	if !ok || format["type"] != "json_schema" {
		t.Fatalf("response_format json_schema missing: %#v", got["response_format"])
	}
	schema, ok := format["json_schema"].(map[string]interface{})
	if !ok || schema["schema"] == nil {
		t.Fatalf("json_schema.schema missing: %#v", format)
	}
}

func TestLMStudioLoadModelSendsContextLengthToNativeLoadEndpoint(t *testing.T) {
	var path string
	var auth string
	var body map[string]interface{}
	var loadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{"models":[{"key":"unsloth/qwen3.6-27b","loaded_instances":[]}]}`))
		case "/api/v1/models/load":
			loadCalls++
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"type":"llm","status":"loaded","load_config":{"context_length":32768}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "unsloth/qwen3.6-27b", 32768, "token-123", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}

	if path != "/api/v1/models/load" {
		t.Fatalf("path = %q, want /api/v1/models/load", path)
	}
	if loadCalls != 1 {
		t.Fatalf("loadCalls = %d, want 1", loadCalls)
	}
	if auth != "Bearer token-123" {
		t.Fatalf("auth = %q, want bearer token", auth)
	}
	if body["model"] != "unsloth/qwen3.6-27b" {
		t.Fatalf("model = %v", body["model"])
	}
	assertJSONNumber(t, body, "context_length", 32768)
	if body["echo_load_config"] != true {
		t.Fatalf("echo_load_config = %v, want true", body["echo_load_config"])
	}
}

func TestLlamaCppLoadModelSendsContextSizeToNativeLoadEndpoint(t *testing.T) {
	var path string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"loaded","load_config":{"context_length":32768}}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "Qwen3.6-27B-Q4_K_M.gguf", 32768, "", true)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/models/load" {
		t.Fatalf("path = %q, want /v1/models/load", path)
	}
	if body["model"] != "Qwen3.6-27B-Q4_K_M.gguf" {
		t.Fatalf("model = %v", body["model"])
	}
	assertJSONNumber(t, body, "context_size", 32768)
	if body["kv_cache_precision"] != "f16" || body["kv_cache_type_k"] != "f16" || body["kv_cache_type_v"] != "f16" {
		t.Fatalf("default KV cache config = %#v, want FP16 K/V", body)
	}
	if body["echo_load_config"] != true {
		t.Fatalf("echo_load_config = %v, want true", body["echo_load_config"])
	}
}

func TestLlamaCppLoadModelSendsCustomKVCacheTypes(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"loaded"}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "model.gguf", 32768, "", true)
	client.kvCachePrecision = "custom"
	client.kvCacheTypeK = "q8_0"
	client.kvCacheTypeV = "f16"

	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body["kv_cache_precision"] != "custom" || body["kv_cache_type_k"] != "q8_0" || body["kv_cache_type_v"] != "f16" {
		t.Fatalf("custom KV cache config was not forwarded: %#v", body)
	}
}

func TestNewLlamacppResolvesKVCacheProfileSettings(t *testing.T) {
	client, ok := NewLlamacpp(settings.Profile{
		ModelID:          "model.gguf",
		CtxTokens:        32768,
		KVCachePrecision: "q8",
		KVCacheTypeK:     "f16",
		KVCacheTypeV:     "q4_0",
	}).(*OpenAICompat)
	if !ok {
		t.Fatal("NewLlamacpp did not return the shared OpenAI-compatible client")
	}
	if client.kvCachePrecision != "q8_0" || client.kvCacheTypeK != "q8_0" || client.kvCacheTypeV != "q8_0" {
		t.Fatalf("resolved KV cache config = (%q, %q, %q), want Q8_0 K/V",
			client.kvCachePrecision, client.kvCacheTypeK, client.kvCacheTypeV)
	}
}

func TestLlamaCppForceLoadModelSendsForceReload(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"loaded","load_config":{"context_length":16384}}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "Qwen3.6-35B.gguf", 16384, "", true)
	if err := client.ForceLoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertJSONNumber(t, body, "context_size", 16384)
	if body["force_reload"] != true {
		t.Fatalf("force_reload = %v, want true", body["force_reload"])
	}
}

func TestLlamaCppLoadModelIncludesLaunchAffectingTemplateKwargs(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"loaded"}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "qwen", 32768, "", true)
	client.loadKwargsJSON = `{"enable_thinking":true,"preserve_thinking":true}`
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body["chat_template_kwargs_json"] != `{"enable_thinking":true,"preserve_thinking":true}` {
		t.Fatalf("chat_template_kwargs_json = %#v", body["chat_template_kwargs_json"])
	}
}

func TestLlamaCppLoadModelDoesNotSendDraftModelWhenSpecTypeDisabled(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"loaded"}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "gemma", 32768, "", true)
	client.specDraftModel = `C:\missing-draft.gguf`
	client.specDraftNMax = 3

	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["spec_type"]; ok {
		t.Fatalf("spec_type should not be sent when disabled: %#v", body)
	}
	if _, ok := body["draft_model_path"]; ok {
		t.Fatalf("draft_model_path should not be sent when spec_type is disabled: %#v", body)
	}
	if _, ok := body["spec_draft_n_max"]; ok {
		t.Fatalf("spec_draft_n_max should not be sent when spec_type is disabled: %#v", body)
	}
}

func TestLMStudioLoadModelSkipsWhenAlreadyLoadedWithContext(t *testing.T) {
	loadCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "qwen/qwen3.6-27b",
					"selected_variant": "qwen/qwen3.6-27b@ud-q4_k_xl",
					"loaded_instances": [{
						"id": "qwen/qwen3.6-27b:2",
						"config": {"context_length": 32768}
					}]
				}]
			}`))
		case "/api/v1/models/load":
			loadCalls++
			_, _ = w.Write([]byte(`{"status":"loaded"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "qwen/qwen3.6-27b", 32768, "", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loadCalls != 0 {
		t.Fatalf("loadCalls = %d, want 0", loadCalls)
	}
}

func TestLMStudioLoadModelUnloadsPreviousModelBeforeLoadingNew(t *testing.T) {
	var unloadCalls []string
	var loadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			// A different model ("old-model") is currently loaded.
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "old-model",
					"loaded_instances": [{"id": "old-model:1"}]
				}]
			}`))
		case "/api/v1/models/unload":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			unloadCalls = append(unloadCalls, body["model"])
			_, _ = w.Write([]byte(`{"status":"unloaded"}`))
		case "/api/v1/models/load":
			loadCalls++
			_, _ = w.Write([]byte(`{"status":"loaded"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "new-model", 32768, "", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(unloadCalls) != 1 || unloadCalls[0] != "old-model" {
		t.Fatalf("unloadCalls = %v, want [old-model]", unloadCalls)
	}
	if loadCalls != 1 {
		t.Fatalf("loadCalls = %d, want 1", loadCalls)
	}
}

func TestLMStudioLoadModelDoesNotUnloadTargetModel(t *testing.T) {
	var unloadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			// Target model is already loaded at the right context length.
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "qwen/qwen3.6-27b",
					"loaded_instances": [{"id": "qwen/qwen3.6-27b:1", "config": {"context_length": 32768}}]
				}]
			}`))
		case "/api/v1/models/unload":
			unloadCalls++
			_, _ = w.Write([]byte(`{"status":"unloaded"}`))
		case "/api/v1/models/load":
			_, _ = w.Write([]byte(`{"status":"loaded"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "qwen/qwen3.6-27b", 32768, "", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if unloadCalls != 0 {
		t.Fatalf("target model should not be unloaded, unloadCalls = %d", unloadCalls)
	}
}

func TestLMStudioLoadModelSkipsAlreadyLoadedTargetWithMissingContext(t *testing.T) {
	var loadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "qwen/qwen3.6-27b",
					"selected_variant": "qwen/qwen3.6-27b@ud-q4_k_xl",
					"loaded_instances": [{"id": "qwen/qwen3.6-27b:1", "config": {}}]
				}]
			}`))
		case "/api/v1/models/load":
			loadCalls++
			_, _ = w.Write([]byte(`{"status":"loaded"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "qwen/qwen3.6-27b", 32768, "", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loadCalls != 0 {
		t.Fatalf("already-loaded target should not trigger duplicate load, loadCalls = %d", loadCalls)
	}
}

func TestLMStudioLoadModelUnloadsTargetWithTooSmallContextBeforeReload(t *testing.T) {
	var unloadCalls []string
	var loadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "qwen/qwen3.6-27b",
					"loaded_instances": [{"id": "qwen/qwen3.6-27b:1", "config": {"context_length": 4096}}]
				}]
			}`))
		case "/api/v1/models/unload":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			unloadCalls = append(unloadCalls, body["model"])
			_, _ = w.Write([]byte(`{"status":"unloaded"}`))
		case "/api/v1/models/load":
			loadCalls++
			_, _ = w.Write([]byte(`{"status":"loaded"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "qwen/qwen3.6-27b", 32768, "", false)
	if err := client.LoadModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(unloadCalls) != 1 || unloadCalls[0] != "qwen/qwen3.6-27b" {
		t.Fatalf("unloadCalls = %v, want target unload before reload", unloadCalls)
	}
	if loadCalls != 1 {
		t.Fatalf("loadCalls = %d, want 1", loadCalls)
	}
}

func TestModelsAppliesTimeoutWhenCallerHasNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL, "qwen", 32768, "", true)
	start := time.Now()
	_, err := client.Models(context.Background())
	if err == nil {
		t.Fatal("Models returned nil error for hung server")
	}
	if time.Since(start) > 12*time.Second {
		t.Fatalf("Models did not respect internal timeout")
	}
}

func TestModelsRespectsShorterCallerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL, "qwen", 32768, "", true)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.Models(ctx)
	if err == nil {
		t.Fatal("Models returned nil error for caller timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Models ignored shorter caller timeout")
	}
}

func TestChatCancellationPostsInferenceCancelForLlamaCpp(t *testing.T) {
	var cancelCalls atomic.Int32
	streamStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"},\"finish_reason\":\"\"}]}\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			close(streamStarted)
			<-r.Context().Done()
		case "/v1/inference/cancel":
			cancelCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cancelled":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "qwen", 32768, "", true)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := client.Chat(ctx, llm.Request{
		Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "hello")},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-streamStarted
	cancel()
	for range ch {
	}
	deadline := time.Now().Add(5 * time.Second)
	for cancelCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if cancelCalls.Load() == 0 {
		t.Fatal("expected /v1/inference/cancel to be called after chat cancellation")
	}
}

func TestActualContextLengthReturnsLoadedContextForLMStudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/models" {
			_, _ = w.Write([]byte(`{
				"models": [{
					"key": "unsloth/qwen3.6-27b",
					"loaded_instances": [{"id": "unsloth/qwen3.6-27b:1", "config": {"context_length": 24576}}]
				}]
			}`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "unsloth/qwen3.6-27b", 32768, "", false)
	got := client.ActualContextLength(context.Background())
	if got != 24576 {
		t.Fatalf("ActualContextLength = %d, want 24576", got)
	}
}

func TestActualContextLengthReadsLlamaCppProps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":153088}}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "some-model", 32768, "", true)
	if got := client.ActualContextLength(context.Background()); got != 153088 {
		t.Fatalf("ActualContextLength for llamacpp = %d, want 153088", got)
	}
}

func TestActualContextLengthReadsInferenceBridgeStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/stats" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"requested_model": "Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
			"active_model": "Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
			"matches_active_model": true,
			"state": "Loaded",
			"progress": {"stage": "loaded", "done": true},
			"stats": {
				"model": "Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
				"context_size": 54016
			}
		}`))
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf", 45000, "", true)
	if got := client.ActualContextLength(context.Background()); got != 54016 {
		t.Fatalf("ActualContextLength for InferenceBridge stats = %d, want 54016", got)
	}
}

func TestActualContextLengthIgnoresInferenceBridgeStatsForDifferentModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/stats":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"active_model": "other-model.gguf",
				"matches_active_model": false,
				"state": "Loaded",
				"progress": {"stage": "loaded", "done": true},
				"stats": {"model": "other-model.gguf", "context_size": 54016}
			}`))
		case "/props":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":32768}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newOpenAICompat("llamacpp", server.URL+"/v1", "wanted-model.gguf", 32768, "", true)
	if got := client.ActualContextLength(context.Background()); got != 0 {
		t.Fatalf("ActualContextLength for mismatched InferenceBridge stats = %d, want 0", got)
	}
}

func TestActualContextLengthReturnsZeroWhenModelNotLoaded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models": [{"key": "unsloth/qwen3.6-27b", "loaded_instances": []}]}`))
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "unsloth/qwen3.6-27b", 32768, "", false)
	if got := client.ActualContextLength(context.Background()); got != 0 {
		t.Fatalf("ActualContextLength for unloaded model = %d, want 0", got)
	}
}

func TestActualContextLengthReturnsZeroOnAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "unsloth/qwen3.6-27b", 32768, "", false)
	if got := client.ActualContextLength(context.Background()); got != 0 {
		t.Fatalf("ActualContextLength on API error = %d, want 0", got)
	}
}

func TestActualContextLengthReturnsZeroWhenContextLengthMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Instance is loaded but config has no context_length field.
		_, _ = w.Write([]byte(`{
			"models": [{
				"key": "unsloth/qwen3.6-27b",
				"loaded_instances": [{"id": "unsloth/qwen3.6-27b:1", "config": {}}]
			}]
		}`))
	}))
	defer server.Close()

	client := newOpenAICompat("lmstudio", server.URL+"/v1", "unsloth/qwen3.6-27b", 32768, "", false)
	if got := client.ActualContextLength(context.Background()); got != 0 {
		t.Fatalf("ActualContextLength with missing context_length = %d, want 0", got)
	}
}

func assertJSONNumber(t *testing.T, m map[string]interface{}, key string, want float64) {
	t.Helper()
	got, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s missing or not number: %#v", key, m[key])
	}
	if got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}

func TestOpenAICompatBuildBodyCarriesGrammar(t *testing.T) {
	client := newOpenAICompat("llamacpp", "http://example.test/v1", "gemma-local", 32768, "", true)
	// with grammar set
	bodyBytes, err := client.buildBody(llm.Request{
		Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "hi")},
		Grammar:  `root ::= "x"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &got); err != nil {
		t.Fatal(err)
	}
	if got["grammar"] != `root ::= "x"` {
		t.Fatalf("grammar not carried: %v", got["grammar"])
	}
	// omitted when empty
	bodyBytes, _ = client.buildBody(llm.Request{Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "hi")}})
	got = map[string]interface{}{}
	_ = json.Unmarshal(bodyBytes, &got)
	if _, ok := got["grammar"]; ok {
		t.Fatalf("grammar should be omitted when empty, got %v", got["grammar"])
	}
}

func TestBuildMessagesNormalizesLateControllerPrompts(t *testing.T) {
	messages := buildMessages(llm.Request{Messages: []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "primary system"),
		llm.NewTextMessage(llm.RoleUser, "task"),
		llm.NewTextMessage(llm.RoleAssistant, "first answer"),
		llm.NewTextMessage(llm.RoleSystem, "controller repair one"),
		llm.NewTextMessage(llm.RoleAssistant, "second answer"),
		llm.NewTextMessage(llm.RoleSystem, "controller repair two"),
	}}, false)

	wantRoles := []string{
		llm.RoleSystem,
		llm.RoleUser,
		llm.RoleAssistant,
		llm.RoleUser,
		llm.RoleAssistant,
		llm.RoleUser,
	}
	if len(messages) != len(wantRoles) {
		t.Fatalf("messages = %#v, want %d alternating turns", messages, len(wantRoles))
	}
	for i, want := range wantRoles {
		if messages[i].Role != want {
			t.Fatalf("message %d role = %q, want %q; messages=%#v", i, messages[i].Role, want, messages)
		}
	}
}

func TestBuildMessagesMergesConsecutiveTextOnlyAssistantTurns(t *testing.T) {
	messages := buildMessages(llm.Request{Messages: []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		llm.NewTextMessage(llm.RoleAssistant, "first answer"),
		llm.NewTextMessage(llm.RoleAssistant, "second answer"),
	}}, false)

	if len(messages) != 2 || messages[1].Role != llm.RoleAssistant {
		t.Fatalf("messages = %#v, want one merged assistant turn", messages)
	}
	if messages[1].Content != "first answer\nsecond answer" {
		t.Fatalf("merged content = %#v", messages[1].Content)
	}
}

func TestBuildMessagesPreservesToolCallResultPair(t *testing.T) {
	messages := buildMessages(llm.Request{Messages: []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "read the file"),
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCallDef{{
				ID:   "call-1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read",
					Arguments: json.RawMessage(`{"path":"a.txt"}`),
				},
			}},
		},
		{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read", Content: "contents"},
		llm.NewTextMessage(llm.RoleAssistant, "done"),
	}}, false)

	if len(messages) != 4 || messages[1].ToolCalls == nil || messages[2].Role != llm.RoleTool ||
		messages[2].ToolCallID != "call-1" || messages[3].Role != llm.RoleAssistant {
		t.Fatalf("tool transcript was changed: %#v", messages)
	}
}
