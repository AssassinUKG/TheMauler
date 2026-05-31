package backends

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mauler/internal/llm"
)

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
	if body["echo_load_config"] != true {
		t.Fatalf("echo_load_config = %v, want true", body["echo_load_config"])
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
