package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestToEnabledToolDefsFiltersDisabledTools(t *testing.T) {
	registry := New()
	defs := registry.ToEnabledToolDefs(map[string]bool{
		"read_file":  true,
		"web_search": true,
		"bash":       false,
	})

	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if !seen["read_file"] || !seen["read_pdf"] || !seen["web_search"] {
		t.Fatalf("enabled tools missing: %#v", seen)
	}
	if seen["bash"] {
		t.Fatalf("explicitly disabled tool was included: %#v", seen)
	}
}

func TestShellDisabledAlsoDisablesBashAlias(t *testing.T) {
	registry := New()
	defs := registry.ToEnabledToolDefs(map[string]bool{
		"shell": false,
		"bash":  true,
	})

	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if seen["shell"] || seen["bash"] {
		t.Fatalf("shell alias tools should both be hidden when shell is disabled: %#v", seen)
	}
}

func TestBashFallsBackToShellSettingWhenBashUnset(t *testing.T) {
	registry := New()
	defs := registry.ToEnabledToolDefs(map[string]bool{
		"shell": false,
	})

	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if seen["bash"] {
		t.Fatalf("bash should honour shell=false when bash is absent: %#v", seen)
	}
}

type typedArgsTool struct {
	got map[string]any
}

func (t *typedArgsTool) Name() string        { return "typed_args" }
func (t *typedArgsTool) Description() string { return "test tool" }
func (t *typedArgsTool) Destructive() bool   { return false }
func (t *typedArgsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"max_tool_calls":{"type":"integer"},
			"timeout_seconds":{"type":"integer"},
			"ratio":{"type":"number"},
			"enabled":{"type":"boolean"},
			"query":{"type":"string"}
		}
	}`)
}
func (t *typedArgsTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	return "ok", json.Unmarshal(raw, &t.got)
}

func TestRegistryRunCoercesStringifiedSchemaTypes(t *testing.T) {
	registry := &Registry{tools: map[string]Tool{}}
	tool := &typedArgsTool{}
	registry.Register(tool)

	_, err := registry.Run(context.Background(), llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "typed_args",
			Arguments: json.RawMessage(`{"max_tool_calls":"8","timeout_seconds":"180","ratio":"0.95","enabled":"true","query":"cars"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.got["max_tool_calls"] != float64(8) || tool.got["timeout_seconds"] != float64(180) {
		t.Fatalf("integer-like strings were not coerced: %#v", tool.got)
	}
	if tool.got["ratio"] != 0.95 || tool.got["enabled"] != true || tool.got["query"] != "cars" {
		t.Fatalf("schema coercion changed unexpected values: %#v", tool.got)
	}
}

type requiredArgsTool struct {
	ran bool
}

func (t *requiredArgsTool) Name() string        { return "required_args" }
func (t *requiredArgsTool) Description() string { return "test tool" }
func (t *requiredArgsTool) Destructive() bool   { return false }
func (t *requiredArgsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"required":["path","content"],
		"properties":{
			"path":{"type":"string"},
			"content":{"type":"string"}
		}
	}`)
}
func (t *requiredArgsTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	t.ran = true
	return "ok", nil
}

func TestRegistryRunRejectsMissingRequiredArguments(t *testing.T) {
	registry := &Registry{tools: map[string]Tool{}}
	tool := &requiredArgsTool{}
	registry.Register(tool)

	_, err := registry.Run(context.Background(), llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "required_args",
			Arguments: json.RawMessage(`{"path":"out.txt"}`),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("expected missing content error, got %v", err)
	}
	if tool.ran {
		t.Fatalf("tool ran despite missing required argument")
	}
}

type nestedArgsTool struct {
	got map[string]any
}

func (t *nestedArgsTool) Name() string        { return "nested_args" }
func (t *nestedArgsTool) Description() string { return "test tool" }
func (t *nestedArgsTool) Destructive() bool   { return false }
func (t *nestedArgsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"options":{
				"type":"object",
				"properties":{
					"recursive":{"type":"boolean"},
					"limit":{"type":"integer"}
				}
			}
		}
	}`)
}
func (t *nestedArgsTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	return "ok", json.Unmarshal(raw, &t.got)
}

func TestRegistryRunCoercesNestedStringifiedSchemaTypes(t *testing.T) {
	registry := &Registry{tools: map[string]Tool{}}
	tool := &nestedArgsTool{}
	registry.Register(tool)

	_, err := registry.Run(context.Background(), llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "nested_args",
			Arguments: json.RawMessage(`{"options":{"recursive":"true","limit":"3"}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options, ok := tool.got["options"].(map[string]any)
	if !ok {
		t.Fatalf("options was not an object: %#v", tool.got)
	}
	if options["recursive"] != true || options["limit"] != float64(3) {
		t.Fatalf("nested schema values were not coerced: %#v", options)
	}
}
