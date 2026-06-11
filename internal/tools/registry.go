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
	if err := validateRequiredArguments(t.Schema(), args); err != nil {
		return "", err
	}
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
		&Shell{TimeoutSecs: 120},
		&Bash{TimeoutSecs: 120},
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
	Required   []string                      `json:"required"`
}

type jsonPropertySchema struct {
	Type       any                           `json:"type"`
	Properties map[string]jsonPropertySchema `json:"properties"`
	Items      *jsonPropertySchema           `json:"items"`
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
	changed := coerceObjectValues(object, params.Properties)
	if !changed {
		return raw
	}
	out, err := json.Marshal(object)
	if err != nil {
		return raw
	}
	return out
}

func coerceObjectValues(object map[string]any, properties map[string]jsonPropertySchema) bool {
	changed := false
	for name, prop := range properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		if coerceValueForProperty(object, name, value, prop) {
			changed = true
		}
	}
	return changed
}

func coerceValueForProperty(parent map[string]any, name string, value any, prop jsonPropertySchema) bool {
	switch typed := value.(type) {
	case string:
		coerced, ok := coerceStringValue(typed, prop)
		if ok {
			parent[name] = coerced
			return true
		}
	case map[string]any:
		return coerceObjectValues(typed, prop.Properties)
	case []any:
		return coerceArrayValues(typed, prop)
	}
	return false
}

func coerceArrayValues(values []any, prop jsonPropertySchema) bool {
	if prop.Items == nil {
		return false
	}
	changed := false
	for i, value := range values {
		switch typed := value.(type) {
		case string:
			coerced, ok := coerceStringValue(typed, *prop.Items)
			if ok {
				values[i] = coerced
				changed = true
			}
		case map[string]any:
			if coerceObjectValues(typed, prop.Items.Properties) {
				changed = true
			}
		}
	}
	return changed
}

func coerceStringValue(value string, prop jsonPropertySchema) (any, bool) {
	text := strings.TrimSpace(value)
	for _, typ := range schemaTypes(prop.Type) {
		switch typ {
		case "integer":
			if parsed, ok := parseInteger(text); ok {
				return parsed, true
			}
		case "number":
			if parsed, err := strconv.ParseFloat(text, 64); err == nil {
				return parsed, true
			}
		case "boolean":
			if parsed, err := strconv.ParseBool(strings.ToLower(text)); err == nil {
				return parsed, true
			}
		}
	}
	return nil, false
}

func validateRequiredArguments(schema, raw json.RawMessage) error {
	var params jsonObjectSchema
	if len(schema) == 0 || json.Unmarshal(schema, &params) != nil || len(params.Required) == 0 {
		return nil
	}
	var object map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &object) != nil {
		return fmt.Errorf("tool arguments must be a JSON object with required fields: %s", strings.Join(params.Required, ", "))
	}
	missing := make([]string, 0)
	for _, name := range params.Required {
		if isEmptyArgValue(object[name]) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tool argument(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func isEmptyArgValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
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
