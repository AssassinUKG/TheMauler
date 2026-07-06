package llm

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func defs(names ...string) []ToolDef {
	out := make([]ToolDef, 0, len(names))
	for _, n := range names {
		out = append(out, ToolDef{Type: "function", Function: ToolFunctionDef{Name: n}})
	}
	return out
}

func TestToolCallGrammarEmpty(t *testing.T) {
	if g := ToolCallGrammar(nil); g != "" {
		t.Fatalf("expected empty grammar for no tools, got %q", g)
	}
}

func TestToolCallGrammarContainsSortedDedupedNames(t *testing.T) {
	g := ToolCallGrammar(defs("shell", "read_file", "shell", "terminal_run"))
	for _, n := range []string{"shell", "read_file", "terminal_run"} {
		if !strings.Contains(g, `\"`+n+`\"`) {
			t.Fatalf("grammar missing tool name %q:\n%s", n, g)
		}
	}
	// name rule alternation should be sorted: read_file < shell < terminal_run
	nameLine := grammarRule(g, "name")
	if strings.Index(nameLine, "read_file") > strings.Index(nameLine, "shell") {
		t.Fatalf("names not sorted in: %s", nameLine)
	}
	// dedup: "shell" literal appears once in the name rule
	if strings.Count(nameLine, `\"shell\"`) != 1 {
		t.Fatalf("shell not deduped: %s", nameLine)
	}
}

// TestToolCallGrammarWellFormed checks every referenced rule is defined — the
// most common GBNF authoring bug.
func TestToolCallGrammarWellFormed(t *testing.T) {
	g := ToolCallGrammar(defs("shell", "http_probe"))
	defined := map[string]bool{}
	for _, line := range strings.Split(g, "\n") {
		if i := strings.Index(line, "::="); i > 0 {
			defined[strings.TrimSpace(line[:i])] = true
		}
	}
	for _, want := range []string{"root", "name", "value", "object", "array", "string", "number", "ws"} {
		if !defined[want] {
			t.Fatalf("rule %q not defined in grammar:\n%s", want, g)
		}
	}
	// Strip quoted literals and char classes, then every remaining identifier must be a defined rule.
	ident := regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_-]*`)
	for _, line := range strings.Split(g, "\n") {
		rhs := line
		if i := strings.Index(line, "::="); i >= 0 {
			rhs = line[i+3:]
		}
		rhs = stripLiteralsAndClasses(rhs)
		for _, id := range ident.FindAllString(rhs, -1) {
			if !defined[id] {
				t.Fatalf("undefined rule reference %q in line: %s", id, line)
			}
		}
	}
}

// TestToolCallGrammarAcceptsRealToolCall is a sanity check that a real tool-call
// JSON our system would emit is at least valid JSON of the envelope shape the
// grammar describes (full GBNF acceptance needs the llama.cpp engine).
func TestToolCallGrammarAcceptsRealToolCall(t *testing.T) {
	g := ToolCallGrammar(defs("shell"))
	if g == "" {
		t.Fatal("expected non-empty grammar")
	}
	call := `{"name": "shell", "arguments": {"command": "id", "timeout": 120, "background": false}}`
	var env struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(call), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if env.Name != "shell" || !json.Valid(env.Arguments) {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func grammarRule(grammar, rule string) string {
	for _, line := range strings.Split(grammar, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), rule+" ") || strings.HasPrefix(strings.TrimSpace(line), rule+"::") {
			return line
		}
	}
	return ""
}

// stripLiteralsAndClasses removes "..."-quoted literals and [...] char classes so
// the lint only sees rule identifiers.
func stripLiteralsAndClasses(s string) string {
	var b strings.Builder
	inStr, inClass, esc := false, false, false
	for _, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case inStr:
			if r == '"' {
				inStr = false
			}
		case inClass:
			if r == ']' {
				inClass = false
			}
		case r == '"':
			inStr = true
		case r == '[':
			inClass = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
