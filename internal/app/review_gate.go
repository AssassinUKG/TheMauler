package app

import (
	"regexp"
	"strings"
)

var apiHTTPMethodWordRE = regexp.MustCompile(`(?i)\b(?:get|post|put|patch|delete|head|options)\b`)
var answerOutputVerbRE = regexp.MustCompile(`(?i)\b(?:create|make|generate|produce|return|give me|show me)\s+(?:(?:a|an|the)\s+)?(?:(?:concise|short|brief|detailed|markdown|simple|clear|complete|full)\s+)?(?:table|summary|report|plan|list|breakdown)\b`)
var planOutputVerbRE = regexp.MustCompile(`(?i)\b(?:create|make|produce|give me|show me)\s+(?:(?:a|an|the)\s+)?(?:(?:concise|short|brief|detailed|clear|complete|full)\s+)?plan\b`)
var artifactMutationVerbRE = regexp.MustCompile(`(?i)\b(?:write|save|store|place|put|drop|leave|download|copy)\b`)
var commandOutputNounRE = regexp.MustCompile(`(?i)\b(?:(?:curl|wget|powershell|pwsh|bash|shell|terminal|cli)\s+)?(?:command|command line|one[- ]liner|request)\b`)
var commandOutputIntentRE = regexp.MustCompile(`(?i)\b(?:give|show|provide|compose|format|generate|create|write|return)\b`)
var commandExecutionStartRE = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:(?:can|could|would|will)\s+you\s+)?(?:run|execute|test|try|send|submit|fire|invoke|perform|issue|validate|verify)\b`)
var commandExecutionObjectRE = regexp.MustCompile(`(?i)\b(?:then|and|also|please)?\s*(?:run|execute|test|try|send|submit|fire|invoke|perform|issue|validate|verify)\s+(?:it|this|that|the\s+(?:command|request|payload)|curl|wget|powershell|pwsh|bash)\b`)
var commandFileDestinationRE = regexp.MustCompile(`(?i)\b(?:save|store|place|put|write)\b[^\r\n]{0,80}\b(?:file|workspace|repo(?:sitory)?|directory|folder|path|disk)\b`)

// VerifyVerdict is the shared result shape for review-loop gates.
type VerifyVerdict struct {
	Gate         string   `json:"gate"`
	Status       string   `json:"status"`
	Blocking     bool     `json:"blocking"`
	Summary      string   `json:"summary"`
	Improvements []string `json:"improvements"`
	Evidence     string   `json:"evidence"`
}

// runIsGateable reports whether the review loop should run for this task.
// Read-only research/recon/planning tasks are excluded even when automation is on.
func runIsGateable(run TaskRun, mode AgentMode) bool {
	modeName := strings.ToLower(strings.TrimSpace(mode.Name))
	if modeName == "" {
		modeName = strings.ToLower(strings.TrimSpace(run.Mode))
	}
	if !reviewGateableMode(modeName) {
		return false
	}
	if runHasFileMutation(run) {
		return true
	}
	if promptLooksReadOnly(run.Prompt) {
		return false
	}
	return promptImpliesConcreteDeliverable(run.Prompt)
}

func reviewGateableMode(modeName string) bool {
	switch strings.ToLower(strings.TrimSpace(modeName)) {
	case "auto", "builder", "fixer", "ops":
		return true
	default:
		return false
	}
}

func promptLooksReadOnly(prompt string) bool {
	lower := strings.ToLower(prompt)
	if explicitlyForbidsToolUse(prompt) {
		return true
	}
	if promptClearlyRequestsCommandOutput(lower) {
		return true
	}
	if promptClearlyRequestsAnswerOutput(lower) && !promptAlsoRequestsWorkspaceMutation(lower) {
		return true
	}
	if hasAny(lower,
		"do not edit", "don't edit", "no edits", "no changes", "read-only", "readonly", "without editing",
	) {
		return true
	}
	explicitMutation := promptExplicitlyRequestsMutation(lower)
	if hasAny(lower, "do not change", "don't change", "without changing") && !explicitMutation {
		return true
	}
	if explicitMutation {
		return false
	}
	if hasAny(lower,
		"just inspect", "just read", "just review", "only inspect", "only read", "inspect ", "read ",
		"map this repo", "map the repo", "explain", "summarize", "summarise",
		"research", "recon", "enumerate", "find out", "look up", "analyse", "analyze",
		"how many", "count ", "list ", "can you access", "can you read", "can you tell me", "tell me ", "what is", "what are",
	) {
		return true
	}
	return false
}

// promptClearlyRequestsCommandOutput separates composing a command in Chat from
// authorising its execution. Security nouns such as PoC, exploit, payload, URL,
// and callback describe the requested text; they do not turn "give me the curl
// command" into permission to send the request. An explicit execution verb or
// workspace destination keeps the normal agent/tool route.
func promptClearlyRequestsCommandOutput(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	if lower == "" || !commandOutputNounRE.MatchString(lower) || !commandOutputIntentRE.MatchString(lower) {
		return false
	}
	if promptAlsoRequestsWorkspaceMutation(lower) || promptRequestsArtifactMutation(lower) || commandFileDestinationRE.MatchString(lower) {
		return false
	}
	return !commandExecutionStartRE.MatchString(lower) && !commandExecutionObjectRE.MatchString(lower)
}

func promptExplicitlyRequestsMutation(lower string) bool {
	if promptAlsoRequestsWorkspaceMutation(lower) || promptRequestsArtifactMutation(lower) {
		return true
	}
	if promptClearlyRequestsAnswerOutput(lower) && !promptAlsoRequestsWorkspaceMutation(lower) {
		// “Create a table/summary/report” normally describes the requested Chat
		// response, not permission to edit the workspace. A separate explicit
		// file/code/docs action below still wins for mixed requests.
		return false
	}
	if promptLooksLikeAPIInventory(lower) && !hasAny(lower,
		"then delete", "and delete", "please delete", "delete the", "delete this", "delete that",
		"then patch", "and patch", "please patch", "patch the", "patch this",
		"remove ", "edit ", "update ", "change ", "implement ", "create ", "add ",
	) {
		// HTTP methods are data in an API inventory question, not edit/delete
		// instructions. Strip them before applying the workspace-mutation verbs.
		lower = apiHTTPMethodWordRE.ReplaceAllString(lower, "")
	}
	return hasAny(lower,
		"add ", "create ", "implement ", "fix ", "patch ", "update ",
		"edit ", "replace ", "refactor ", "wire ", "land ", "ship ",
		"convert ", "migrate ", "append ", "delete ", "remove ",
		"write a file", "write the file", "write to ", "generate a file",
		"generate the file", "build the app", "build the project",
	)
}

func promptRequestsArtifactMutation(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	if !artifactMutationVerbRE.MatchString(lower) || !hasAny(lower,
		"poc", "proof of concept", "script", "checker", "source file", "source code",
		"code file", "patch file", "artifact", "payload file", "exploit file",
	) {
		return false
	}
	destination := hasAny(lower,
		"directory", "folder", "workspace", "repository", "repo", "path", "to disk",
		"on disk", "locally", "save as", "write as", "into ",
	)
	chatOnly := hasAny(lower,
		"in chat", "in the chat", "answer with", "reply with", "paste the code",
		"show me the code", "show the code", "do not save", "don't save", "dont save",
	)
	return destination || !chatOnly
}

func promptClearlyRequestsAnswerOutput(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	return answerOutputVerbRE.MatchString(lower) || hasAny(lower,
		"tell me", "let me know", "show me", "give me", "answer with", "answer in chat",
		"respond with", "reply with", "return only", "report back", "in the chat", "in chat",
		"how many", "count the", "counts for", "calculate", "breakdown", "break down",
		"list the", "list all", "summarize", "summarise", "explain",
		"create the table", "create the summary", "create the report",
		"give me a plan", "show me a plan",
	)
}

func promptClearlyRequestsPlanOutput(lower string) bool {
	return planOutputVerbRE.MatchString(strings.TrimSpace(lower))
}

func promptAlsoRequestsWorkspaceMutation(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	return hasAny(lower,
		"then fix", "and fix", "also fix", "please fix",
		"then repair", "and repair", "also repair", "please repair",
		"then implement", "and implement", "also implement", "please implement",
		"then update", "and update", "also update",
		"then edit", "and edit", "also edit", "please edit",
		"then add", "and add", "also add",
		"then create", "and create", "also create",
		"then delete", "and delete", "also delete", "please delete",
		"then remove", "and remove", "also remove", "please remove",
		"save to a file", "save to the file", "save this to", "save it to", "save as a file",
		"save it in", "save this in", "save the poc in", "save the script in",
		"write to a file", "write to the file", "write this to", "write it to", "write into",
		"create a file", "create the file", "generate a file", "generate the file",
		"leave it in the directory", "leave it in this directory", "leave it in the folder", "leave it in this folder",
		"put it in the directory", "put it in this directory", "put it in the folder", "put it in this folder",
		"drop it in the directory", "drop it in this directory", "drop it in the folder", "drop it in this folder",
		"store it in the directory", "store it in this directory", "store it in the folder", "store it in this folder",
		"update the file", "update files", "update the docs", "update docs", "update documentation",
		"update the readme", "update readme", "edit the file", "edit files", "edit the docs",
		"append to", "commit the", "apply the change", "apply these changes",
	)
}

func promptLooksLikeAPIInventory(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	return hasAny(lower, "endpoint", "api", "openapi", "swagger") &&
		hasAny(lower, "how many", "count", "coverage", "list", "which") &&
		apiHTTPMethodWordRE.MatchString(lower)
}

func promptImpliesConcreteDeliverable(prompt string) bool {
	lower := strings.ToLower(prompt)
	if promptExplicitlyRequestsMutation(lower) {
		return true
	}
	return hasAny(lower,
		"apply the change", "apply these changes", "working implementation",
		"code change", "pull request", "deliverable file", "save the report",
	)
}
