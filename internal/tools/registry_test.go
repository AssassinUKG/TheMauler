package tools

import (
	"context"
	"encoding/json"
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
