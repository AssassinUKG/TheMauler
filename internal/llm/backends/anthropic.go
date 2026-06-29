package backends

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"net/http"
	"os"
	"strings"
)

const anthropicDefaultBaseURL = "https://api.anthropic.com/v1"

type Anthropic struct {
	apiKey     string
	modelID    string
	baseURL    string
	httpClient *http.Client
}

func NewAnthropic(p settings.Profile) llm.Client {
	key := os.Getenv(p.APIKeyEnv)
	if key == "" && p.APIKeyEnv == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}
	return &Anthropic{
		apiKey:     key,
		modelID:    p.ModelID,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Ping(ctx context.Context) error {
	if a.apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return nil
}

func (a *Anthropic) Models(ctx context.Context) ([]string, error) {
	if strings.TrimSpace(a.modelID) == "" {
		return nil, nil
	}
	return []string{a.modelID}, nil
}

func (a *Anthropic) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	if strings.TrimSpace(a.apiKey) == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	body, err := buildAnthropicBody(a.modelID, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", a.apiKey)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	ch := make(chan llm.Delta, 128)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		parseAnthropicSSE(ctx, resp.Body, ch)
	}()
	return ch, nil
}

type anthropicReq struct {
	Model       string               `json:"model"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []anthropicMessage   `json:"messages"`
	System      string               `json:"system,omitempty"`
	Stream      bool                 `json:"stream"`
	Temperature *float64             `json:"temperature,omitempty"`
	TopP        *float64             `json:"top_p,omitempty"`
	Tools       []anthropicTool      `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

func buildAnthropicBody(model string, req llm.Request) ([]byte, error) {
	system, messages := convertAnthropicMessages(req.Messages)
	if strings.TrimSpace(req.System) != "" {
		if system != "" {
			system += "\n\n"
		}
		system += strings.TrimSpace(req.System)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := anthropicReq{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  messages,
		System:    system,
		Stream:    true,
	}
	if req.Temperature != 0 {
		body.Temperature = &req.Temperature
	}
	if req.TopP != 0 {
		body.TopP = &req.TopP
	}
	for _, tool := range req.Tools {
		body.Tools = append(body.Tools, anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}
	switch req.ToolChoice {
	case "required":
		body.ToolChoice = &anthropicToolChoice{Type: "any"}
	case "none":
		body.ToolChoice = &anthropicToolChoice{Type: "none"}
	case "auto":
		body.ToolChoice = &anthropicToolChoice{Type: "auto"}
	}
	return json.Marshal(body)
}

func convertAnthropicMessages(messages []llm.Message) (string, []anthropicMessage) {
	var system []string
	var out []anthropicMessage
	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleSystem:
			if text := strings.TrimSpace(messageTextForAnthropic(msg)); text != "" {
				system = append(system, text)
			}
		case llm.RoleAssistant:
			parts := []anthropicContentPart{}
			if text := messageTextForAnthropic(msg); text != "" {
				parts = append(parts, anthropicContentPart{Type: "text", Text: text})
			}
			for _, call := range msg.ToolCalls {
				input := call.Function.Arguments
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				parts = append(parts, anthropicContentPart{
					Type:  "tool_use",
					ID:    call.ID,
					Name:  call.Function.Name,
					Input: input,
				})
			}
			if len(parts) > 0 {
				out = append(out, anthropicMessage{Role: "assistant", Content: parts})
			}
		case llm.RoleTool:
			out = append(out, anthropicMessage{
				Role: "user",
				Content: []anthropicContentPart{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   messageTextForAnthropic(msg),
				}},
			})
		default:
			if text := messageTextForAnthropic(msg); text != "" {
				out = append(out, anthropicMessage{Role: "user", Content: []anthropicContentPart{{Type: "text", Text: text}}})
			}
		}
	}
	return strings.Join(system, "\n\n"), out
}

func messageTextForAnthropic(msg llm.Message) string {
	switch c := msg.Content.(type) {
	case string:
		return c
	case []llm.ContentBlock:
		var sb strings.Builder
		for _, block := range c {
			if block.Text != "" {
				sb.WriteString(block.Text)
			}
			if block.ImageURL != nil && block.ImageURL.URL != "" {
				sb.WriteString("\n[image attachment omitted: Anthropic backend bridge currently sends text/tool content only]")
			}
		}
		return sb.String()
	default:
		data, _ := json.Marshal(c)
		return string(data)
	}
}

type anthropicStreamState struct {
	blocks map[int]*anthropicToolBlock
}

type anthropicToolBlock struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func parseAnthropicSSE(ctx context.Context, r io.Reader, ch chan<- llm.Delta) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	state := anthropicStreamState{blocks: map[int]*anthropicToolBlock{}}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		for _, delta := range state.parseEvent([]byte(data)) {
			ch <- delta
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- llm.Delta{Error: err}
	}
}

func (s *anthropicStreamState) parseEvent(data []byte) []llm.Delta {
	var event struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
			Text  string          `json:"text"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return []llm.Delta{{Error: err}}
	}
	switch event.Type {
	case "content_block_start":
		if event.ContentBlock.Type == "tool_use" {
			block := &anthropicToolBlock{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name}
			input := strings.TrimSpace(string(event.ContentBlock.Input))
			if input != "" && input != "null" && input != "{}" {
				block.Arguments.Write(event.ContentBlock.Input)
			}
			s.blocks[event.Index] = block
		}
	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
			return []llm.Delta{{Content: event.Delta.Text}}
		case "input_json_delta":
			block := s.blocks[event.Index]
			if block == nil {
				block = &anthropicToolBlock{}
				s.blocks[event.Index] = block
			}
			block.Arguments.WriteString(event.Delta.PartialJSON)
		}
	case "content_block_stop":
		if block := s.blocks[event.Index]; block != nil && block.Name != "" {
			args := strings.TrimSpace(block.Arguments.String())
			if args == "" {
				args = "{}"
			}
			delete(s.blocks, event.Index)
			return []llm.Delta{{ToolCalls: []llm.ToolCallDef{{
				ID:   block.ID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      block.Name,
					Arguments: json.RawMessage(args),
				},
			}}}}
		}
	case "message_delta":
		delta := llm.Delta{}
		if event.Delta.StopReason == "max_tokens" {
			delta.Truncated = true
		}
		if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 {
			delta.Usage = &llm.Usage{
				PromptTokens:     event.Usage.InputTokens,
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
			}
		}
		if delta.Truncated || delta.Usage != nil {
			return []llm.Delta{delta}
		}
	}
	return nil
}
