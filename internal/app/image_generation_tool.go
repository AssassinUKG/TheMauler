package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

const generateImageToolName = "generate_image"

type imageCapabilities struct {
	Enabled                   bool     `json:"enabled"`
	Ready                     bool     `json:"ready"`
	AvailableNow              bool     `json:"available_now"`
	AutomaticModelSwapEnabled bool     `json:"automatic_model_swap_enabled"`
	BlockedReason             string   `json:"blocked_reason"`
	DefaultModel              string   `json:"default_model"`
	DefaultQuality            string   `json:"default_quality"`
	DefaultSize               string   `json:"default_size"`
	SupportedSizes            []string `json:"supported_sizes"`
	Reasons                   []string `json:"reasons"`
}

type imageJobResult struct {
	URL               string `json:"url"`
	Model             string `json:"model"`
	ModelName         string `json:"model_name"`
	Quantization      string `json:"quantization"`
	Profile           string `json:"profile"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Steps             int    `json:"steps"`
	Seed              int64  `json:"seed"`
	ChatRestoreStatus string `json:"chat_restore_status"`
}

type imageJob struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	QueuePosition     int             `json:"queue_position"`
	JobsAhead         int             `json:"jobs_ahead"`
	Stage             string          `json:"stage"`
	Message           string          `json:"message"`
	Progress          float64         `json:"progress"`
	CurrentStep       int             `json:"current_step"`
	TotalSteps        int             `json:"total_steps"`
	ElapsedSeconds    float64         `json:"elapsed_seconds"`
	ETASeconds        *float64        `json:"eta_seconds"`
	Done              bool            `json:"done"`
	Error             string          `json:"error"`
	StatusURL         string          `json:"status_url"`
	EventsURL         string          `json:"events_url"`
	CancelURL         string          `json:"cancel_url"`
	ChatRestoreStatus string          `json:"chat_restore_status"`
	Result            *imageJobResult `json:"result"`
}

type imageGenerationArgs struct {
	Prompt            string  `json:"prompt"`
	NegativePrompt    string  `json:"negative_prompt,omitempty"`
	Size              string  `json:"size,omitempty"`
	Quality           string  `json:"quality,omitempty"`
	Steps             int     `json:"steps,omitempty"`
	Seed              *int64  `json:"seed,omitempty"`
	ReferenceImageURL string  `json:"reference_image_url,omitempty"`
	ReferenceStrength float64 `json:"reference_strength,omitempty"`
	VisualType        string  `json:"visual_type,omitempty"`
}

type inferenceBridgeImageClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type toolExecutionContextKey struct{}

func withToolExecutionContext(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, toolExecutionContextKey{}, strings.TrimSpace(callID))
}

func toolExecutionID(ctx context.Context) string {
	value, _ := ctx.Value(toolExecutionContextKey{}).(string)
	return strings.TrimSpace(value)
}

type generateImageTool struct{ app *App }

func (t *generateImageTool) Name() string      { return generateImageToolName }
func (t *generateImageTool) Destructive() bool { return false }
func (t *generateImageTool) Description() string {
	return "Generate one image with InferenceBridge's managed image runtime. Use for artistic or photographic visuals; use deterministic diagram or document tools when exact text, labels, or numbers matter. The result is a protected image URL, never base64 image data."
}
func (t *generateImageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string"},
    "negative_prompt": {"type": "string"},
    "size": {"type": "string", "enum": ["1024x1024", "1328x1328", "1664x928", "928x1664", "1472x1104", "1104x1472", "1584x1056", "1056x1584"]},
    "quality": {"type": "string", "enum": ["preview", "quality", "max_quality"]},
    "steps": {"type": "integer", "enum": [30, 40, 50, 60]},
    "seed": {"type": "integer", "minimum": 0, "maximum": 4294967295},
    "reference_image_url": {"type": "string"},
    "reference_strength": {"type": "number", "minimum": 0.05, "maximum": 1.0},
    "visual_type": {"type": "string"}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`)
}

func (t *generateImageTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.app == nil {
		return "", fmt.Errorf("generate_image: app is unavailable")
	}
	var args imageGenerationArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("generate_image: bad params: %w", err)
	}
	args.Prompt = strings.TrimSpace(args.Prompt)
	if args.Prompt == "" {
		return "", fmt.Errorf("generate_image: prompt is required")
	}

	client, err := t.app.inferenceBridgeImageClient()
	if err != nil {
		return "", err
	}
	capability, err := client.capabilities(ctx)
	if err != nil {
		return "", fmt.Errorf("generate_image: InferenceBridge capability check failed: %w", err)
	}
	if !capability.AvailableNow {
		reason := firstNonEmpty(capability.BlockedReason, strings.Join(capability.Reasons, "; "), "image generation is not currently available")
		return "", fmt.Errorf("generate_image: %s", reason)
	}

	runID := ""
	if claimant, claimantErr := engagementClaimantFromContext(ctx); claimantErr == nil {
		runID = claimant.ID
	}
	callID := toolExecutionID(ctx)
	if callID == "" {
		callID = randomImageRequestID()
	}
	correlationID := "mauler:" + firstNonEmpty(runID, callID)
	idempotencyKey := imageIdempotencyKey(runID, callID, raw)

	job, err := client.submit(ctx, args, capability, correlationID, idempotencyKey)
	if err != nil {
		return "", fmt.Errorf("generate_image: submit failed: %w", err)
	}
	emit := func(update imageJob) {
		payload := map[string]any{
			"tool_call_id": callID,
			"job_id":       update.ID,
			"status":       update.Status,
			"stage":        update.Stage,
			"message":      update.Message,
			"progress":     update.Progress,
			"current_step": update.CurrentStep,
			"total_steps":  update.TotalSteps,
			"jobs_ahead":   update.JobsAhead,
			"eta_seconds":  update.ETASeconds,
		}
		t.app.emitRunContext(ctx, "mauler:image_progress", payload)
	}
	emit(job)

	completed, err := client.wait(ctx, job, emit)
	if err != nil {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = client.cancel(cancelCtx, job.ID)
			cancel()
			return "", fmt.Errorf("generate_image: cancelled; InferenceBridge was asked to cancel job %s", job.ID)
		}
		return "", fmt.Errorf("generate_image: %w", err)
	}
	emit(completed)
	if completed.Result == nil {
		return "", fmt.Errorf("generate_image: job %s completed without an image result", completed.ID)
	}
	result := completed.Result
	protectedURL := client.resolve(result.URL)
	compact := map[string]any{
		"status":              "completed",
		"job_id":              completed.ID,
		"image_url":           protectedURL,
		"model":               result.Model,
		"quantization":        result.Quantization,
		"size":                fmt.Sprintf("%dx%d", result.Width, result.Height),
		"steps":               result.Steps,
		"seed":                result.Seed,
		"chat_restore_status": firstNonEmpty(result.ChatRestoreStatus, completed.ChatRestoreStatus),
		"note":                "The image URL is protected by InferenceBridge; do not request or insert base64 image data into model context.",
	}
	encoded, _ := json.Marshal(compact)
	return string(encoded), nil
}

func (a *App) inferenceBridgeImageClient() (*inferenceBridgeImageClient, error) {
	a.mu.Lock()
	profiles := a.profiles
	var provider settings.Provider
	found := false
	if profiles != nil {
		provider, found = profiles.Providers["inference-bridge"]
		if !found {
			for _, candidate := range profiles.Providers {
				if strings.Contains(strings.ToLower(candidate.Name+" "+candidate.BaseURL), "inference") || strings.Contains(candidate.BaseURL, ":8800") {
					provider = candidate
					found = true
					break
				}
			}
		}
	}
	a.mu.Unlock()
	if !found || strings.TrimSpace(provider.BaseURL) == "" {
		return nil, fmt.Errorf("generate_image: the InferenceBridge provider is not configured")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("generate_image: invalid InferenceBridge URL: %w", err)
	}
	return &inferenceBridgeImageClient{
		baseURL:    baseURL,
		apiKey:     settings.ResolveProviderAPIKey(firstNonEmpty(provider.Name, "inference-bridge"), provider.APIKeyEnv),
		httpClient: &http.Client{},
	}, nil
}

func (a *App) imageGenerationAvailable(ctx context.Context) bool {
	a.imageCapabilityMu.Lock()
	if time.Since(a.imageCapabilityAt) < 15*time.Second {
		available := a.imageCapability.AvailableNow
		a.imageCapabilityMu.Unlock()
		return available
	}
	a.imageCapabilityMu.Unlock()

	client, err := a.inferenceBridgeImageClient()
	if err != nil {
		return false
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	capability, err := client.capabilities(queryCtx)
	if err != nil {
		return false
	}
	a.imageCapabilityMu.Lock()
	a.imageCapability = capability
	a.imageCapabilityAt = time.Now()
	a.imageCapabilityMu.Unlock()
	return capability.AvailableNow
}

func (c *inferenceBridgeImageClient) capabilities(ctx context.Context) (imageCapabilities, error) {
	var capability imageCapabilities
	err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/images/capabilities", nil, nil, &capability)
	return capability, err
}

func (c *inferenceBridgeImageClient) submit(ctx context.Context, args imageGenerationArgs, capability imageCapabilities, correlationID, idempotencyKey string) (imageJob, error) {
	payload := map[string]any{
		"model":           firstNonEmpty(capability.DefaultModel, "qwen-image-2512-q6"),
		"prompt":          args.Prompt,
		"size":            firstNonEmpty(args.Size, capability.DefaultSize, "1024x1024"),
		"quality":         firstNonEmpty(args.Quality, capability.DefaultQuality, "quality"),
		"n":               1,
		"response_format": "url",
		"metadata": map[string]string{
			"client":         "mauler",
			"correlation_id": correlationID,
		},
	}
	if value := strings.TrimSpace(args.NegativePrompt); value != "" {
		payload["negative_prompt"] = value
	}
	if args.Steps > 0 {
		payload["steps"] = args.Steps
	}
	if args.Seed != nil {
		payload["seed"] = *args.Seed
	}
	if value := strings.TrimSpace(args.ReferenceImageURL); value != "" {
		payload["reference_image_url"] = value
	}
	if args.ReferenceStrength > 0 {
		payload["reference_strength"] = args.ReferenceStrength
	}
	if value := strings.TrimSpace(args.VisualType); value != "" {
		payload["visual_type"] = value
	}
	headers := map[string]string{
		"Prefer":           "respond-async",
		"Idempotency-Key":  idempotencyKey,
		"X-Correlation-ID": correlationID,
		"X-Client-Name":    "mauler",
	}
	var job imageJob
	err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/images/generations", payload, headers, &job)
	return job, err
}

func (c *inferenceBridgeImageClient) wait(ctx context.Context, initial imageJob, onUpdate func(imageJob)) (imageJob, error) {
	job, streamErr := c.waitSSE(ctx, initial, onUpdate)
	if streamErr == nil || ctx.Err() != nil {
		return job, streamErr
	}
	return c.waitPolling(ctx, initial.ID, onUpdate)
}

func (c *inferenceBridgeImageClient) waitSSE(ctx context.Context, initial imageJob, onUpdate func(imageJob)) (imageJob, error) {
	eventsURL := initial.EventsURL
	if eventsURL == "" {
		eventsURL = "/v1/images/events?job_id=" + url.QueryEscape(initial.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(eventsURL), nil)
	if err != nil {
		return initial, err
	}
	c.addHeaders(req)
	req.Header.Set("Accept", "text/event-stream")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return initial, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return initial, responseError(response)
	}

	current := initial
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				var update imageJob
				if json.Unmarshal([]byte(data.String()), &update) == nil && update.ID == initial.ID {
					current = update
					onUpdate(current)
					if imageJobTerminal(current.Status) {
						return terminalImageJob(current)
					}
				}
				data.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return current, err
	}
	return current, fmt.Errorf("image event stream ended before job %s completed", initial.ID)
}

func (c *inferenceBridgeImageClient) waitPolling(ctx context.Context, jobID string, onUpdate func(imageJob)) (imageJob, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		job, err := c.getJob(ctx, jobID)
		if err == nil {
			onUpdate(job)
			if imageJobTerminal(job.Status) {
				return terminalImageJob(job)
			}
		}
		select {
		case <-ctx.Done():
			return imageJob{ID: jobID}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *inferenceBridgeImageClient) getJob(ctx context.Context, jobID string) (imageJob, error) {
	var job imageJob
	err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/images/generations/"+url.PathEscape(jobID), nil, nil, &job)
	return job, err
}

func (c *inferenceBridgeImageClient) cancel(ctx context.Context, jobID string) (imageJob, error) {
	var job imageJob
	err := c.doJSON(ctx, http.MethodDelete, c.baseURL+"/images/generations/"+url.PathEscape(jobID), nil, nil, &job)
	return job, err
}

func (c *inferenceBridgeImageClient) doJSON(ctx context.Context, method, endpoint string, body any, headers map[string]string, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	c.addHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(output)
}

func (c *inferenceBridgeImageClient) addHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TheMauler/InferenceBridgeImageClient")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func (c *inferenceBridgeImageClient) resolve(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	base, baseErr := url.Parse(c.baseURL)
	relative, relativeErr := url.Parse(reference)
	if baseErr != nil || relativeErr != nil {
		return reference
	}
	return base.ResolveReference(relative).String()
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	var envelope struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		return fmt.Errorf("InferenceBridge returned %s: %v", response.Status, envelope.Error)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("InferenceBridge returned %s: %s", response.Status, message)
}

func imageJobTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalImageJob(job imageJob) (imageJob, error) {
	switch strings.ToLower(strings.TrimSpace(job.Status)) {
	case "completed":
		return job, nil
	case "cancelled":
		return job, fmt.Errorf("image job %s was cancelled", job.ID)
	default:
		return job, fmt.Errorf("image job %s failed: %s", job.ID, firstNonEmpty(job.Error, job.Message, "unknown error"))
	}
}

func imageIdempotencyKey(runID, callID string, raw json.RawMessage) string {
	hash := sha256.Sum256([]byte(runID + "\x00" + callID + "\x00" + string(raw)))
	return "mauler-" + hex.EncodeToString(hash[:16])
}

func randomImageRequestID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("image-%d", time.Now().UnixNano())
	}
	return "image-" + hex.EncodeToString(bytes)
}

func containsToolDef(defs []llm.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func filterOutToolDef(defs []llm.ToolDef, name string) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(defs))
	for _, def := range defs {
		if def.Function.Name != name {
			out = append(out, def)
		}
	}
	return out
}
