package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type readToolResultTool struct{ app *App }

type readToolResultArgs struct {
	ResultID string `json:"result_id"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

func (t *readToolResultTool) Name() string { return "read_tool_result" }

func (t *readToolResultTool) Description() string {
	return "Read a slice of a large tool result that TheMauler offloaded from context. Use the result_id shown in the tool-result preview, with optional offset and limit."
}

func (t *readToolResultTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "result_id": {"type": "string", "description": "The offloaded result handle shown in a prior tool result preview."},
    "offset": {"type": "integer", "description": "Character offset to start reading from. Default 0."},
    "limit": {"type": "integer", "description": "Maximum characters to return. Default 4000, max 20000."}
  },
  "required": ["result_id"],
  "additionalProperties": false
}`)
}

func (t *readToolResultTool) Destructive() bool { return false }

func (t *readToolResultTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args readToolResultArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("read_tool_result: bad params: %w", err)
	}
	args.ResultID = strings.TrimSpace(args.ResultID)
	if args.ResultID == "" {
		return "", fmt.Errorf("read_tool_result: result_id is required")
	}
	out, ok := t.app.loadToolResultSlice(args.ResultID, args.Offset, args.Limit)
	if !ok {
		return fmt.Sprintf("No offloaded tool result found for result_id=%q. Use the exact result_id from the preview.", args.ResultID), nil
	}
	return out, nil
}
