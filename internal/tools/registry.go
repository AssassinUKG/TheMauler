// Package tools defines the Tool interface and the shared Registry.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"mauler/internal/llm"
)

// Tool is the interface every built-in tool must implement.
type Tool interface {
	// Name returns the function name sent to the model (snake_case).
	Name() string
	// Description is the human + model readable description.
	Description() string
	// Schema returns the JSON Schema object for the parameters.
	Schema() json.RawMessage
	// Run executes the tool and returns a result string or an error.
	Run(ctx context.Context, params json.RawMessage) (string, error)
	// Destructive reports whether this tool modifies files or runs code.
	Destructive() bool
}

// Registry holds all registered tools.
type Registry struct {
	tools map[string]Tool
}

// New returns a Registry pre-populated with the default tool set.
func New() *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range defaults() {
		r.Register(t)
	}
	return r
}

// Register adds a tool, overwriting any existing tool with the same name.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ToToolDefs converts the registry to the slice format the LLM API expects.
func (r *Registry) ToToolDefs() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return defs
}

// ToEnabledToolDefs converts only enabled tools to function declarations.
func (r *Registry) ToEnabledToolDefs(enabled map[string]bool) []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		name := t.Name()
		if enabled != nil {
			if on, ok := enabledState(enabled, name); ok && !on {
				continue
			}
		}
		defs = append(defs, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:        name,
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return defs
}

func enabledState(enabled map[string]bool, name string) (bool, bool) {
	if name == "bash" {
		if on, ok := enabled["shell"]; ok {
			return on, true
		}
		if on, ok := enabled["bash"]; ok {
			return on, true
		}
	}
	on, ok := enabled[name]
	return on, ok
}

// Run dispatches a tool call by name and returns the result string.
// If the tool is not found, it returns an error result string (not a Go error)
// so the model can self-correct.
func (r *Registry) Run(ctx context.Context, call llm.ToolCallDef) (string, error) {
	t, ok := r.tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
	args := coerceJSONArguments(t.Schema(), call.Function.Arguments)
	return t.Run(ctx, args)
}

// defaults returns the built-in tool instances.
func defaults() []Tool {
	return []Tool{
		&ReadFile{},
		&ReadMany{},
		&FileOutline{},
		&ReadChunks{},
		&ReadPDF{},
		&WriteFile{},
		&EditFile{},
		&Shell{TimeoutSecs: 30},
		&Bash{TimeoutSecs: 30},
		&Glob{},
		&Grep{},
		&SessionSearch{},
		&SQLiteSchema{},
		&SQLiteQuery{},
		&TodoCreate{},
		&TodoUpdate{},
		&TodoDone{},
		&TodoBlocked{},
		&TodoList{},
		&TodoClear{},
		&WebSearch{},
		&FetchURL{},
		&BrowserOpen{},
		&BrowserSnapshot{},
		&BrowserClick{},
		&BrowserType{},
		&BrowserExtract{},
		&BrowserScreenshot{},
		&BrowserClose{},
		&BrowserAgent{TimeoutSecs: 300},
		&SkillsList{},
		&SkillView{},
	}
}

type jsonObjectSchema struct {
	Properties map[string]jsonPropertySchema `json:"properties"`
}

type jsonPropertySchema struct {
	Type any `json:"type"`
}

func coerceJSONArguments(schema, raw json.RawMessage) json.RawMessage {
	var object map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &object) != nil {
		return raw
	}
	var params jsonObjectSchema
	if len(schema) == 0 || json.Unmarshal(schema, &params) != nil || len(params.Properties) == 0 {
		return raw
	}
	changed := false
	for name, prop := range params.Properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		fieldChanged := false
		for _, typ := range schemaTypes(prop.Type) {
			switch typ {
			case "integer":
				parsed, ok := parseInteger(text)
				if ok {
					object[name] = parsed
					changed = true
					fieldChanged = true
				}
			case "number":
				parsed, err := strconv.ParseFloat(text, 64)
				if err == nil {
					object[name] = parsed
					changed = true
					fieldChanged = true
				}
			case "boolean":
				parsed, err := strconv.ParseBool(strings.ToLower(text))
				if err == nil {
					object[name] = parsed
					changed = true
					fieldChanged = true
				}
			}
			if fieldChanged {
				break
			}
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(object)
	if err != nil {
		return raw
	}
	return out
}

func schemaTypes(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func parseInteger(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	if i, err := strconv.Atoi(text); err == nil {
		return i, true
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}
