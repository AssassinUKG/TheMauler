package llm

import (
	"sort"
	"strings"
)

// ToolCallGrammar builds a GBNF grammar that constrains a model's output to a
// single well-formed tool call: a JSON object {"name": <one of the enabled tool
// names>, "arguments": <JSON object>}. llama.cpp masks the sampler to this
// grammar at every token, so a weak quant cannot emit the malformed tool-call
// text we saw from gemma (<|channel>, call:x|..., truncated half-calls). It does
// NOT constrain per-tool argument schemas yet (that's a later, fiddlier pass);
// it guarantees a valid envelope, a real tool name, and syntactically valid JSON
// arguments — which is the bulk of the reliability win.
//
// Returns "" when there are no tools (nothing to constrain).
func ToolCallGrammar(tools []ToolDef) string {
	names := toolNames(tools)
	if len(names) == 0 {
		return ""
	}

	var b strings.Builder
	// Top-level: the tool-call envelope.
	b.WriteString("root ::= \"{\" ws \"\\\"name\\\"\" ws \":\" ws name ws \",\" ws \"\\\"arguments\\\"\" ws \":\" ws object ws \"}\"\n")

	// name: alternation of the exact enabled tool names, each a JSON string literal.
	b.WriteString("name ::= ")
	for i, n := range names {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString("\"\\\"" + gbnfEscapeLiteral(n) + "\\\"\"")
	}
	b.WriteString("\n")

	// Standard JSON value grammar (llama.cpp GBNF dialect).
	b.WriteString(jsonValueGrammar)
	return b.String()
}

// toolNames returns the sorted, de-duplicated function names from the tool defs.
// Sorted so the generated grammar is deterministic (stable cache keys/tests).
func toolNames(tools []ToolDef) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		n := strings.TrimSpace(t.Function.Name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// gbnfEscapeLiteral escapes a tool name for embedding inside a GBNF double-quoted
// literal that itself represents a JSON string. Tool names are [a-z0-9_], so this
// is defensive; backslash and double-quote are the only characters that would
// break the literal.
func gbnfEscapeLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// jsonValueGrammar is the GBNF for arbitrary JSON values, used for tool-call
// arguments. Mirrors the canonical llama.cpp json.gbnf.
const jsonValueGrammar = `value  ::= object | array | string | number | ("true" | "false" | "null") ws
object ::= "{" ws ( string ws ":" ws value ("," ws string ws ":" ws value)* )? "}" ws
array  ::= "[" ws ( value ("," ws value)* )? "]" ws
string ::= "\"" ( [^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]) )* "\"" ws
number ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? ([eE] [-+]? [0-9]+)? ws
ws     ::= [ \t\n]*
`
