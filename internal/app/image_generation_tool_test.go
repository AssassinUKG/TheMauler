package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInferenceBridgeImageClientAsyncSSEFlow(t *testing.T) {
	var submitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/capabilities":
			_ = json.NewEncoder(w).Encode(imageCapabilities{Ready: true, AvailableNow: true, DefaultModel: "qwen-image", DefaultQuality: "quality", DefaultSize: "1024x1024"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/generations":
			if r.Header.Get("Prefer") != "respond-async" || r.Header.Get("Idempotency-Key") == "" || r.Header.Get("X-Correlation-ID") == "" {
				t.Fatalf("missing async safety headers: %#v", r.Header)
			}
			submitted.Store(true)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"img-1","status":"queued","events_url":"/v1/images/events?job_id=img-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: job.progress\ndata: {\"id\":\"img-1\",\"status\":\"running\",\"progress\":0.5,\"current_step\":25,\"total_steps\":50}\n\n"))
			_, _ = w.Write([]byte("event: job.completed\ndata: {\"id\":\"img-1\",\"status\":\"completed\",\"progress\":1,\"result\":{\"url\":\"/v1/images/generations/img-1/content\",\"model\":\"qwen-image\",\"quantization\":\"Q6_K\",\"width\":1024,\"height\":1024,\"steps\":50,\"seed\":7,\"chat_restore_status\":\"restored\"}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &inferenceBridgeImageClient{baseURL: server.URL + "/v1", httpClient: server.Client()}
	capability, err := client.capabilities(context.Background())
	if err != nil || !capability.AvailableNow {
		t.Fatalf("capability = %#v, err = %v", capability, err)
	}
	job, err := client.submit(context.Background(), imageGenerationArgs{Prompt: "a lighthouse"}, capability, "mauler:run-1", "stable-key")
	if err != nil {
		t.Fatal(err)
	}
	var updates int
	completed, err := client.wait(context.Background(), job, func(imageJob) { updates++ })
	if err != nil {
		t.Fatal(err)
	}
	if !submitted.Load() || updates < 2 || completed.Result == nil || completed.Result.ChatRestoreStatus != "restored" {
		t.Fatalf("completed = %#v, updates = %d", completed, updates)
	}
	if got := client.resolve(completed.Result.URL); got != server.URL+"/v1/images/generations/img-1/content" {
		t.Fatalf("resolved URL = %q", got)
	}
}

func TestInferenceBridgeImageClientFallsBackToPolling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/events":
			http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		case "/v1/images/generations/img-2":
			_, _ = w.Write([]byte(`{"id":"img-2","status":"completed","result":{"url":"/v1/images/generations/img-2/content","chat_restore_status":"restored"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &inferenceBridgeImageClient{baseURL: server.URL + "/v1", httpClient: server.Client()}
	completed, err := client.wait(context.Background(), imageJob{ID: "img-2", EventsURL: "/v1/images/events?job_id=img-2"}, func(imageJob) {})
	if err != nil || completed.Status != "completed" {
		t.Fatalf("completed = %#v, err = %v", completed, err)
	}
}

func TestImageToolResultNeverContainsBase64(t *testing.T) {
	tool := (&generateImageTool{}).Description()
	if !strings.Contains(strings.ToLower(tool), "never base64") {
		t.Fatalf("tool description must keep image bytes out of model context: %q", tool)
	}
}
