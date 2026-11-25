package app

import (
	"regexp"
	"strings"
)

// These predicates consume only operator-owned routing text, never attachment
// bodies. Output location is separate from permission to mutate a named file.
var namedArtifactPathRE = regexp.MustCompile("(?i)[[:alnum:]_./\\\\:-]+\\.(?:txt|md|json|jsonl|yaml|yml|toml|csv|xml|html|css|js|ts|tsx|jsx|py|rs|go|sh|ps1|sql|ini|cfg|log)\\b")
var wholeArtifactPathRE = regexp.MustCompile("(?i)^" + namedArtifactPathRE.String()[4:] + "$")
var quotedRoutingDataRE = regexp.MustCompile("\"(?:\\\\.|[^\"\\\\])*\"|'[^'\\r\\n]*'|\\x60[^\\x60\\r\\n]*\\x60")
var namedArtifactActionRE = regexp.MustCompile("(?i)(?:^|[.!?;]\\s+|\\b(?:then|and|also)\\s+)(?:(?:please|can you|could you|would you|will you)\\s+)?(?:create|write|save|store|append|update|edit|patch|delete|remove|rename|move|copy)\\b")
var compositionObjectRE = regexp.MustCompile("(?i)^\\s*(?:(?:a|an|the|my)\\s+)?(?:(?:short|brief|concise|friendly|formal|long|quick)\\s+)*(?:message|email|reply|note|paragraph|poem|speech|announcement|draft)\\b")
var compositionVerbRE = regexp.MustCompile("(?i)\\b(?:write|draft|compose|rewrite|reword|polish)\\s+")
var externalCompositionActionRE = regexp.MustCompile("(?i)(?:^|[.!?;]\\s+|\\b(?:then|and|also)\\s+)(?:(?:please|can you|could you)\\s+)?(?:send|post|publish|submit|execute|run|upload)\\s+(?:it|this|that|the|a|an|my)\\b")
var numericDirectAnswerRE = regexp.MustCompile("^[+-]?[0-9]+(?:\\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$")
var exactReplyTokenRE = regexp.MustCompile("^[A-Za-z0-9_-]{1,160}$")

func promptHasNamedFileMutation(text string) bool {
	instructions := quotedRoutingDataRE.ReplaceAllStringFunc(text, func(quoted string) string {
		body := quoted[1 : len(quoted)-1]
		if wholeArtifactPathRE.MatchString(body) {
			return body
		}
		return " "
	})
	for _, location := range namedArtifactActionRE.FindAllStringIndex(instructions, -1) {
		object := strings.TrimSpace(instructions[location[1]:])
		if compositionObjectRE.MatchString(object) {
			continue
		}
		if len(object) > 220 {
			object = object[:220]
		}
		if end := strings.Index(strings.ToLower(object), " containing "); end >= 0 {
			object = object[:end]
		}
		if namedArtifactPathRE.MatchString(object) {
			return true
		}
	}
	return false
}

func promptHasStandaloneNoToolInstruction(text string) bool {
	clauses := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return strings.ContainsRune(".;!,?\n", r)
	})
	for _, clause := range clauses {
		clause = strings.Join(strings.Fields(clause), " ")
		clause = strings.TrimPrefix(clause, "please ")
		switch clause {
		case "no tools", "without tools", "no files or tools", "no tools or files",
			"no files and no tools", "advice only", "discussion only", "chat only",
			"do not inspect or access any files", "do not access any files":
			return true
		}
	}
	return false
}

func shortExactReplyTarget(text string) (string, bool) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"reply exactly ", "respond exactly ", "answer exactly "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		target := strings.TrimSpace(text[len(prefix):])
		target = strings.TrimSuffix(target, ".")
		suffix := " and nothing else"
		if strings.HasSuffix(strings.ToLower(target), suffix) {
			target = strings.TrimSpace(target[:len(target)-len(suffix)])
		}
		return target, exactReplyTokenRE.MatchString(target)
	}
	return "", false
}

func promptRequestsTextComposition(text string) bool {
	if _, ok := shortExactReplyTarget(text); ok {
		return true
	}
	if needsLiveSystemInfoTool(text) || needsLiveExternalInfoTool(text) || needsWindowsHostInspectionTool(text) {
		return false
	}
	lower := strings.ToLower(text)
	if promptHasNamedFileMutation(text) || promptAlsoRequestsWorkspaceMutation(lower) || externalCompositionActionRE.MatchString(lower) {
		return false
	}
	for _, location := range compositionVerbRE.FindAllStringIndex(lower, -1) {
		if compositionObjectRE.MatchString(lower[location[1]:]) {
			return true
		}
	}
	return hasAny(lower, "wanted a message for", "want a message for")
}

func promptIsSimpleGreetingRequest(text string) bool {
	normal := strings.NewReplacer(".", " ", "!", " ", "?", " ", ",", " ").Replace(strings.ToLower(text))
	normal = strings.Join(strings.Fields(normal), " ")
	for _, opening := range []string{"hello", "hi", "hey", "hiya", "hello there", "hi there"} {
		if normal == opening {
			return true
		}
		if strings.HasPrefix(normal, opening+" ") {
			suffix := strings.TrimSpace(strings.TrimPrefix(normal, opening+" "))
			switch suffix {
			case "please greet me briefly", "greet me briefly", "please greet me", "say hi", "keep it brief":
				return true
			}
		}
	}
	return false
}

func directAnswerAcceptedForPrompt(mode, prompt, answer string) bool {
	if target, ok := shortExactReplyTarget(prompt); ok && normalizeConversationMode(mode) != conversationModeAgent {
		return strings.TrimSpace(stripVisibleThinkTags(answer)) == target
	}
	return directAnswerAcceptedForMode(mode, answer)
}

func promptHasRepairIntent(text string) bool {
	for _, word := range []string{"fix", "repair", "restore", "broken", "failing", "error", "crash"} {
		if hasWholeWord(text, word) {
			return true
		}
	}
	return hasAny(text, "doesn't work", "does not work")
}

func hasAnyWholePhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if hasWholeWord(text, phrase) {
			return true
		}
	}
	return false
}

func acceptsShortObservedAnswer(run TaskRun, text string) bool {
	if len(run.Tools) != 0 || run.Contract != nil && (len(run.Contract.Deliverables) != 0 || len(run.Contract.RequiredEvidence) != 0) {
		return false
	}
	prompt := taskRoutingPrompt(run)
	if toolChoiceFor(prompt, 0, 0) != "none" {
		return false
	}
	text = strings.TrimSpace(stripVisibleThinkTags(text))
	if target, ok := shortExactReplyTarget(prompt); ok {
		return text == target
	}
	return numericDirectAnswerRE.MatchString(text)
}
