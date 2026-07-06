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
		"read":       true,
		"web_search": true,
	})

	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if !seen["read"] || !seen["web_search"] {
		t.Fatalf("enabled tools missing: %#v", seen)
	}
	if seen["bash"] {
		t.Fatalf("explicitly disabled tool was included: %#v", seen)
	}
}

func TestBashToolIsNotRegistered(t *testing.T) {
	registry := New()
	if _, ok := registry.Get("bash"); ok {
		t.Fatalf("bash tool should not be registered")
	}
}

func TestMissingEnabledMapEntriesAreHidden(t *testing.T) {
	registry := New()
	defs := registry.ToEnabledToolDefs(map[string]bool{
		"shell": true,
	})

	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if !seen["shell"] || seen["read"] {
		t.Fatalf("only explicit enabled entries should be advertised: %#v", seen)
	}
}

type verboseSchemaTool struct{}

func (t *verboseSchemaTool) Name() string { return "verbose_schema" }
func (t *verboseSchemaTool) Description() string {
	return strings.Repeat("Use this detailed model-facing description carefully. ", 20)
}
func (t *verboseSchemaTool) Destructive() bool { return false }
func (t *verboseSchemaTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"title":"Verbose Schema",
		"description":"top-level schema prose",
		"type":"object",
		"required":["path"],
		"properties":{
			"path":{
				"title":"Path",
				"description":"path prose that should not be sent in model schema",
				"type":"string",
				"default":"x",
				"examples":["a","b"]
			},
			"options":{
				"description":"nested prose",
				"type":"object",
				"properties":{
					"limit":{"description":"limit prose","type":"integer"}
				}
			}
		}
	}`)
}
func (t *verboseSchemaTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

func TestToolDefsUseCompactPromptSchemas(t *testing.T) {
	registry := &Registry{tools: map[string]Tool{}}
	registry.Register(&verboseSchemaTool{})

	defs := registry.ToToolDefs()
	if len(defs) != 1 {
		t.Fatalf("defs len = %d, want 1", len(defs))
	}
	if len([]rune(defs[0].Function.Description)) > maxLLMToolDescriptionRunes+20 {
		t.Fatalf("description was not capped: %d runes", len([]rune(defs[0].Function.Description)))
	}
	raw := string(defs[0].Function.Parameters)
	for _, notWant := range []string{"description", "title", "examples", "default", "top-level schema prose", "nested prose"} {
		if strings.Contains(raw, notWant) {
			t.Fatalf("compact schema still contains %q:\n%s", notWant, raw)
		}
	}
	for _, want := range []string{`"type":"object"`, `"required":["path"]`, `"path"`, `"limit"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("compact schema lost required structure %q:\n%s", want, raw)
		}
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

func TestMetadataForDefaultTools(t *testing.T) {
	registry := New()
	shell, ok := registry.Get("shell")
	if !ok {
		t.Fatal("missing shell tool")
	}
	meta := MetadataFor(shell)
	if meta.AccessClass != "exec" || meta.RequiresShell == "" || !meta.UnrestrictedReady {
		t.Fatalf("unexpected shell metadata: %#v", meta)
	}
	read, ok := registry.Get("read")
	if !ok {
		t.Fatal("missing read tool")
	}
	meta = MetadataFor(read)
	if meta.AccessClass != "read" || meta.LatencyClass != "instant" {
		t.Fatalf("unexpected read metadata: %#v", meta)
	}
}

func TestRegistrySpecsAreSortedAndGated(t *testing.T) {
	registry := New()
	specs := registry.SpecsFor(map[string]bool{"read": true, "shell": false}, map[string]bool{
		"read":  true,
		"shell": true,
	})
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
		if spec.Name == "read" {
			if spec.Metadata.AccessClass != "read" || len(spec.Schema) == 0 {
				t.Fatalf("read spec missing metadata/schema: %#v", spec)
			}
		}
	}
	if strings.Join(names, ",") != "read" {
		t.Fatalf("unexpected gated specs: %#v", names)
	}
	spec, ok := registry.Spec("shell")
	if !ok {
		t.Fatal("missing shell spec")
	}
	if spec.Metadata.AccessClass != "exec" || !spec.Metadata.Resumable {
		t.Fatalf("unexpected shell spec metadata: %#v", spec.Metadata)
	}
}
