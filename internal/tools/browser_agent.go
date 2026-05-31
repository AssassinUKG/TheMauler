package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BrowserAgent runs a high-level browser task via the browser-use Python library.
// It delegates to scripts/browser_agent.py which drives an AI-powered Playwright session.
type BrowserAgent struct {
	TimeoutSecs int
}

func (t *BrowserAgent) Name() string      { return "browser_agent" }
func (t *BrowserAgent) Destructive() bool { return false }

func (t *BrowserAgent) Description() string {
	return "Execute a complex multi-step browser task using an autonomous AI agent (browser-use + Playwright). " +
		"Pass a natural language task; the agent navigates pages, clicks, fills forms, and extracts data by itself. " +
		"Use this for tasks that span multiple pages or require visual reasoning. " +
		"For precise single-action control use browser_open / browser_click / browser_type instead. " +
		"Requires: pip install browser-use langchain-openai && playwright install chromium."
}

func (t *BrowserAgent) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "Natural language description of the browser task to perform"
    },
    "timeout_secs": {
      "type": "integer",
      "description": "Max seconds to allow (default 300, max 600)"
    }
  },
  "required": ["task"],
  "additionalProperties": false
}`)
}

func (t *BrowserAgent) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Task        string `json:"task"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_agent: bad params: %w", err)
	}
	task := strings.TrimSpace(p.Task)
	if task == "" {
		return "", fmt.Errorf("browser_agent: task is required")
	}

	timeoutSecs := 300
	if t.TimeoutSecs > 0 {
		timeoutSecs = t.TimeoutSecs
	}
	if p.TimeoutSecs > 0 && p.TimeoutSecs <= 600 {
		timeoutSecs = p.TimeoutSecs
	}

	scriptPath, err := findBrowserAgentScript()
	if err != nil {
		return "", fmt.Errorf("browser_agent: %w\nRun: pip install browser-use langchain-openai && playwright install chromium", err)
	}

	pyExe := findPythonExe()

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, pyExe, scriptPath, task)
	cmd.Env = buildBrowserEnv()
	applyHiddenWindow(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	result := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())

	if runCtx.Err() == context.DeadlineExceeded {
		out := result
		if out == "" {
			out = "[no output before timeout]"
		}
		return out, fmt.Errorf("browser_agent: timed out after %ds", timeoutSecs)
	}

	if err != nil {
		msg := fmt.Sprintf("browser_agent: %v", err)
		if errOut != "" {
			msg += "\n" + errOut
		}
		return result, fmt.Errorf("%s", msg)
	}

	if result == "" {
		result = "Task completed."
	}
	return result, nil
}

func findBrowserAgentScript() (string, error) {
	// 1. relative to CWD (most common when running with wails dev)
	wd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(wd, "scripts", "browser_agent.py"),
	}

	// 2. relative to executable (production build)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "scripts", "browser_agent.py"),
		)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("scripts/browser_agent.py not found (looked in %s)", strings.Join(candidates, ", "))
}

func findPythonExe() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "python"
}

// buildBrowserEnv merges the current process environment with browser-use defaults,
// letting BROWSER_USE_* vars already in the environment take precedence.
func buildBrowserEnv() []string {
	env := os.Environ()
	defaults := map[string]string{
		"BROWSER_USE_API_BASE": "http://localhost:1234/v1",
		"BROWSER_USE_API_KEY":  "lm-studio",
		"BROWSER_USE_MODEL":    "qwen3.6-27b",
		"BROWSER_USE_HEADLESS": "true",
	}
	// Only inject defaults for keys not already present in the environment.
	set := make(map[string]bool, len(env))
	for _, kv := range env {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			set[kv[:idx]] = true
		}
	}
	for k, v := range defaults {
		if !set[k] {
			env = append(env, k+"="+v)
		}
	}
	return env
}
