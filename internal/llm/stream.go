package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// --- wire types for OpenAI-compatible SSE chunks ---

type sseChunk struct {
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage"`
	Timings *sseTimings `json:"timings"`
}

type sseChoice struct {
	Index        int      `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason"`
}

type sseDelta struct {
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content"` // Qwen3 / DeepSeek-R1 thinking
	Reasoning        string        `json:"reasoning"`         // llama.cpp / InferenceBridge reasoning alias
	ToolCalls        []sseToolCall `json:"tool_calls"`
}

type sseToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function sseFunctionDelta `json:"function"`
}

type sseFunctionDelta struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type sseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type sseTimings struct {
	CacheN             int     `json:"cache_n"`
	PromptN            int     `json:"prompt_n"`
	PromptMS           float64 `json:"prompt_ms"`
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedN         int     `json:"predicted_n"`
	PredictedMS        float64 `json:"predicted_ms"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
}

// accumTC accumulates fragmented tool-call arguments across chunks.
type accumTC struct {
	id   string
	name string
	args strings.Builder
}

// ParseSSE reads an OpenAI-compatible SSE response body and sends Deltas to ch.
// ch is closed by the caller's goroutine after this function returns.
func ParseSSE(ctx context.Context, r io.Reader, ch chan<- Delta) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)

	accum := make(map[int]*accumTC)
	var truncated bool

	for scanner.Scan() {
		if ctx.Err() != nil {
			ch <- Delta{Error: ctx.Err(), Done: true}
			return
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Malformed SSE chunk — skip it. The stream still closes correctly
			// via the Delta{Done:true} emitted after the scanner loop ends.
			_, _ = fmt.Fprintf(os.Stderr, "[stream] malformed SSE chunk: %v\n", err)
			continue
		}

		// Current llama.cpp sends finish_reason first, followed by a
		// choices:[] usage/timings chunk, then [DONE]. Do not terminate on the
		// finish_reason or the authoritative token counts and backend throughput
		// telemetry are lost.
		if chunk.Usage != nil || chunk.Timings != nil {
			ch <- Delta{Usage: usageFromSSEChunk(chunk)}
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Stream thinking tokens. Different OpenAI-compatible backends use
		// either reasoning_content or reasoning for this field.
		if thinking := choice.Delta.ReasoningContent; thinking != "" {
			ch <- Delta{Thinking: thinking}
		} else if choice.Delta.Reasoning != "" {
			ch <- Delta{Thinking: choice.Delta.Reasoning}
		}

		// Stream text content immediately
		if choice.Delta.Content != "" {
			ch <- Delta{Content: choice.Delta.Content}
		}

		// Accumulate tool-call argument fragments
		for _, tc := range choice.Delta.ToolCalls {
			a, ok := accum[tc.Index]
			if !ok {
				a = &accumTC{}
				accum[tc.Index] = a
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			a.args.WriteString(normalizeSSEToolArguments(tc.Function.Arguments))
		}

		if choice.FinishReason == "length" {
			truncated = true
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		ch <- Delta{Error: err, Done: true}
		return
	}

	// Some backends stream tool-call fragments and terminate on [DONE] or EOF
	// without a finish_reason="tool_calls" chunk. Flush whatever accumulated.
	if len(accum) > 0 {
		delta := flushAccumulatedToolCalls(accum)
		delta.Truncated = delta.Truncated || truncated
		ch <- delta
		return
	}

	// EOF remains a valid terminal signal for older compatible servers that do
	// not emit [DONE].
	ch <- Delta{Done: true, Truncated: truncated}
}

func usageFromSSEChunk(chunk sseChunk) *Usage {
	usage := &Usage{}
	if chunk.Usage != nil {
		usage.PromptTokens = chunk.Usage.PromptTokens
		usage.CompletionTokens = chunk.Usage.CompletionTokens
		usage.TotalTokens = chunk.Usage.TotalTokens
		usage.CachedPromptTokens = chunk.Usage.PromptDetails.CachedTokens
	}
	if chunk.Timings != nil {
		if usage.PromptTokens == 0 {
			usage.PromptTokens = chunk.Timings.CacheN + chunk.Timings.PromptN
		}
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = chunk.Timings.PredictedN
		}
		if usage.CachedPromptTokens == 0 {
			usage.CachedPromptTokens = chunk.Timings.CacheN
		}
		usage.PromptTokensPerSecond = chunk.Timings.PromptPerSecond
		usage.CompletionTokensPerSecond = chunk.Timings.PredictedPerSecond
		usage.PromptMilliseconds = chunk.Timings.PromptMS
		usage.CompletionMilliseconds = chunk.Timings.PredictedMS
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

// flushAccumulatedToolCalls converts the accumulated per-index tool-call
// fragments into a terminal Delta. If any call's arguments are not valid JSON
// (the model was cut off mid-argument by max_tokens) it reports Truncated so the
// agent's truncation-recovery path fires instead of passing a broken call to the
// executor.
func flushAccumulatedToolCalls(accum map[int]*accumTC) Delta {
	indexes := make([]int, 0, len(accum))
	for idx := range accum {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	calls := make([]ToolCallDef, 0, len(accum))
	for _, idx := range indexes {
		a := accum[idx]
		if rawArgs := a.args.String(); !json.Valid([]byte(rawArgs)) {
			return Delta{Done: true, Truncated: true}
		}
		// Local backends (Qwen/llama.cpp/InferenceBridge) often emit tool calls with
		// an empty id. Synthesize a stable per-index id so the assistant tool_call and
		// its tool-result message always share a non-empty tool_call_id — strict
		// OpenAI-compatible servers reject unmatched ids, and history compaction relies
		// on the pairing.
		id := a.id
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		calls = append(calls, ToolCallDef{
			ID:   id,
			Type: "function",
			Function: FunctionCall{
				Name:      a.name,
				Arguments: json.RawMessage(a.args.String()),
			},
		})
	}
	return Delta{ToolCalls: calls, Done: true}
}

func normalizeSSEToolArguments(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
