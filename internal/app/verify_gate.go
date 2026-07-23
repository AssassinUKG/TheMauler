package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

type verifyCommand struct {
	Gate    string
	Command string
}

// runVerifyGate detects whole-task verification commands, runs them through the
// configured shell path, and returns one verdict per command.
func (a *App) runVerifyGate(ctx context.Context, run *TaskRun, cfg *settings.Settings) []VerifyVerdict {
	if run == nil || cfg == nil {
		return nil
	}
	if !runIsGateable(*run, AgentMode{Name: run.Mode}) {
		return nil
	}
	commands := detectVerifyCommands(*cfg)
	if len(commands) == 0 {
		// The sealed task contract only requires a project command verdict when
		// one was available at intake. Simple text/config mutations still have
		// the file-hash postcondition and must not be made permanently
		// unfinishable by a verifier the contract never promised.
		if run.Control != nil && !controlNeedsProjectVerification(*run) {
			return nil
		}
		return []VerifyVerdict{{
			Gate:         "verify",
			Status:       "inconclusive",
			Blocking:     true,
			Summary:      "No deterministic project verifier was detected.",
			Improvements: []string{"Set agents.review_loop.verify_commands to the project's build/test command before relying on completion approval."},
			Evidence:     "Auto-detection found no supported Go, Node, or Python verification contract.",
		}}
	}
	timeout := cfg.Agents.ReviewLoop.VerifyTimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	tools.SetConfigSnapshot(cfg.Tools)
	shell := &tools.Shell{TimeoutSecs: timeout}
	verdicts := make([]VerifyVerdict, 0, len(commands))
	for _, command := range commands {
		raw, _ := json.Marshal(map[string]any{
			"command": command.Command,
			"timeout": timeout,
			"verbose": true,
		})
		out, err := shell.Run(ctx, raw)
		verdicts = append(verdicts, verifyVerdictFromCommand(command, out, err))
	}
	return verdicts
}

func detectVerifyCommands(cfg settings.Settings) []verifyCommand {
	if len(cfg.Agents.ReviewLoop.VerifyCommands) > 0 {
		out := make([]verifyCommand, 0, len(cfg.Agents.ReviewLoop.VerifyCommands))
		for _, command := range cfg.Agents.ReviewLoop.VerifyCommands {
			command = strings.TrimSpace(command)
			if command == "" {
				continue
			}
			out = append(out, verifyCommand{Gate: gateForVerifyCommand(command), Command: command})
		}
		return out
	}
	if fileExists("go.mod") {
		return []verifyCommand{
			{Gate: "build", Command: "go build ./..."},
			{Gate: "lint", Command: "go vet ./..."},
			{Gate: "test", Command: "go test ./..."},
		}
	}
	if scripts := npmVerifyScripts("package.json"); len(scripts) > 0 {
		return scripts
	}
	if fileExists("pyproject.toml") || fileExists("setup.py") {
		out := []verifyCommand{{Gate: "build", Command: "python -m compileall ."}}
		if pythonProjectMentionsPytest() {
			out = append(out, verifyCommand{Gate: "test", Command: "pytest -q"})
		}
		return out
	}
	return nil
}

func npmVerifyScripts(path string) []verifyCommand {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	var out []verifyCommand
	if strings.TrimSpace(pkg.Scripts["build"]) != "" {
		out = append(out, verifyCommand{Gate: "build", Command: "npm run build"})
	}
	if strings.TrimSpace(pkg.Scripts["test"]) != "" {
		out = append(out, verifyCommand{Gate: "test", Command: "npm test"})
	}
	return out
}

func pythonProjectMentionsPytest() bool {
	if dirExists("tests") || fileExists("pytest.ini") {
		return true
	}
	for _, name := range []string{"pyproject.toml", "setup.py"} {
		data, err := os.ReadFile(name)
		if err == nil && strings.Contains(strings.ToLower(string(data)), "pytest") {
			return true
		}
	}
	return false
}

func verifyVerdictFromCommand(command verifyCommand, output string, err error) VerifyVerdict {
	output = strings.TrimSpace(output)
	verdict := VerifyVerdict{
		Gate:     command.Gate,
		Status:   "pass",
		Blocking: false,
		Summary:  fmt.Sprintf("%s passed: %s", command.Gate, command.Command),
		Evidence: trimEvidence(output),
	}
	if err == nil {
		return verdict
	}
	verdict.Status = "fail"
	if strings.Contains(strings.ToLower(err.Error()), "timed out") || strings.Contains(strings.ToLower(output), "timed out") {
		verdict.Status = "error"
	}
	verdict.Blocking = verifyGateBlocks(command.Gate)
	verdict.Summary = fmt.Sprintf("%s failed: %s", command.Gate, command.Command)
	verdict.Improvements = []string{fmt.Sprintf("Fix the failure from `%s` before declaring the task complete.", command.Command)}
	return verdict
}

func verifyGateBlocks(gate string) bool {
	switch strings.ToLower(strings.TrimSpace(gate)) {
	case "build", "test":
		return true
	case "lint":
		return false
	default:
		return true
	}
}

func gateForVerifyCommand(command string) string {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "test"), strings.Contains(lower, "pytest"):
		return "test"
	case strings.Contains(lower, "vet"), strings.Contains(lower, "lint"), strings.Contains(lower, "eslint"):
		return "lint"
	case strings.Contains(lower, "build"), strings.Contains(lower, "compile"):
		return "build"
	default:
		return "verify"
	}
}

func trimEvidence(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= 4000 {
		return output
	}
	return output[:1000] + "\n...\n" + output[len(output)-3000:]
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && info.IsDir()
}
