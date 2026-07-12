package app

import "strings"

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
	if hasAny(lower,
		"do not edit", "don't edit", "no edits", "read-only", "readonly",
		"just inspect", "just read", "just review", "only inspect", "only read",
		"map this repo", "map the repo", "explain", "summarize", "summarise",
		"research", "recon", "enumerate", "find out", "look up",
	) {
		return true
	}
	return false
}

func promptImpliesConcreteDeliverable(prompt string) bool {
	lower := strings.ToLower(prompt)
	return hasAny(lower,
		"add ", "build ", "create ", "implement ", "fix ", "patch ", "update ",
		"write ", "generate ", "refactor ", "wire ", "land ", "ship ",
		"test ", "make ", "convert ", "migrate ",
		"file", "report", "writeup", "write-up", "readme", "doc", "docs",
		"artifact", "patch", "pr", "pull request",
	)
}
