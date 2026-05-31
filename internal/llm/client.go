package llm

import (
	"context"
	"encoding/json"
)

// Role constants.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ContentBlock is one piece of multimodal message content.
type ContentBlock struct {
	Type     string    `json:"type"`               // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds a base64-encoded image for the vision API.
type ImageURL struct {
	URL    string `json:"url"`    // "data:image/png;base64,..."
	Detail string `json:"detail"` // "auto" | "low" | "high"
}

// Message is a single conversation turn.
type Message struct {
	Role       string        `json:"role"`
	Content    interface{}   `json:"content"`                 // string or []ContentBlock
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCallDef `json:"tool_calls,omitempty"`
	Name       string        `json:"name,omitempty"`
}

// NewTextMessage constructs a simple text-only message.
func NewTextMessage(role, text string) Message {
	return Message{Role: role, Content: text}
}

// ToolCallDef is an outbound tool call from the model.
type ToolCallDef struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and its serialised JSON arguments.
type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDef declares a tool the model may call.
type ToolDef struct {
	Type     string          `json:"type"` // always "function"
	Function ToolFunctionDef `json:"function"`
}

// ToolFunctionDef is the function declaration inside a ToolDef.
type ToolFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
}

// Delta is one streaming chunk from the model.
type Delta struct {
	Content   string        // text token(s)
	Thinking  string        // reasoning_content / thinking tokens (Qwen3, DeepSeek-R1, etc.)
	ToolCalls []ToolCallDef // populated on finish_reason == "tool_calls"
	Done      bool
	Truncated bool  // true when finish_reason == "length" (hit max_tokens)
	Error     error
	Usage     *Usage
}

// Usage holds token counts when the backend reports them.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Request is the full input to Client.Chat.
type Request struct {
	Messages        []Message
	Tools           []ToolDef
	System          string          // prepended as a system message
	MaxTokens       int
	Temperature     float64
	TopP            float64
	TopK            int
	MinP            float64
	PresencePenalty float64
	Seed            int64
	// ToolChoice controls whether the model may call tools this turn.
	// ""         → omit from request (backend default, equivalent to "auto")
	// "auto"     → model decides (default when tools are present)
	// "none"     → model must reply in plain text; tool definitions are still
	//              sent so the backend can cache the prefix (better KV-cache
	//              hit rate than removing tools entirely, per OpenAI guidance)
	// "required" → model must call at least one tool
	ToolChoice string
	// Thinking-mode controls (llama.cpp only)
	EnableThinking   bool
	PreserveThinking bool
	// MTP speculative decoding (llama.cpp b9180+).
	// SpecType: "" (disabled) | "draft-mtp"
	// SpecDraftNMax: draft tokens per step, 0 = server default
	SpecType      string
	SpecDraftNMax int
	// Grammar-constrained output (llama.cpp / LM Studio structured output)
	JSONSchema json.RawMessage
}

// Client is the interface every LLM backend must satisfy.
type Client interface {
	// Chat sends a streaming request and returns a channel of Deltas.
	// The channel is closed when the response is complete or on error.
	Chat(ctx context.Context, req Request) (<-chan Delta, error)
	// Models lists models available on the backend.
	Models(ctx context.Context) ([]string, error)
	// Ping checks that the backend is reachable (2-3 s timeout).
	Ping(ctx context.Context) error
	// Name returns a human-readable identifier for this client instance.
	Name() string
}
