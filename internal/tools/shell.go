package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mauler/internal/settings"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Shell runs a command in the configured platform shell.
type Shell struct {
	TimeoutSecs int
}

func (t *Shell) Name() string      { return "shell" }
func (t *Shell) Destructive() bool { return true }

func (t *Shell) Description() string {
	backend := detectShellBackend("")
	return fmt.Sprintf("Run a shell command in the current working directory using the %s backend. "+
		"On Windows auto uses PowerShell; on Linux/WSL auto uses bash. Use platform-native paths for the active backend.", backend)
}

func (t *Shell) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The command to run in the active shell"},
    "timeout": {"type": "integer", "description": "Timeout in seconds (default 30, max 300)"}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
}

type shellParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func (t *Shell) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p shellParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("shell: bad params: %w", err)
	}
	return runShell(ctx, p.Command, p.Timeout, t.TimeoutSecs, "")
}

// Bash is a compatibility alias for older prompts/models that call bash.
type Bash struct {
	TimeoutSecs int
}

func (t *Bash) Name() string      { return "bash" }
func (t *Bash) Destructive() bool { return true }

func (t *Bash) Description() string {
	return "Compatibility alias for shell. Runs through the configured shell backend rather than assuming /bin/bash."
}

func (t *Bash) Schema() json.RawMessage { return (&Shell{}).Schema() }

func (t *Bash) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p shellParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("bash: bad params: %w", err)
	}
	return runShell(ctx, p.Command, p.Timeout, t.TimeoutSecs, "")
}

func runShell(ctx context.Context, command string, requestedTimeout, defaultTimeout int, forcedBackend string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("shell: command is required")
	}
	timeoutSecs := defaultTimeout
	if timeoutSecs <= 0 {
		timeoutSecs = 30
	}
	if requestedTimeout > 0 && requestedTimeout <= 300 {
		timeoutSecs = requestedTimeout
	}

	backend := detectShellBackend(forcedBackend)
	wd, _ := os.Getwd()
	cmd, err := shellCommand(ctx, backend, command, wd)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()
	cmd = cloneCommandWithContext(ctx, cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond)

	var sb strings.Builder
	if stdout.Len() > 0 {
		sb.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(stderr.String())
	}

	if ctx.Err() == context.DeadlineExceeded {
		return sb.String(), fmt.Errorf("shell: timed out after %ds", timeoutSecs)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return sb.String(), fmt.Errorf("shell: %w", err)
		}
	}

	result := strings.TrimRight(sb.String(), "\n\r ")
	if result != "" {
		result += "\n"
	}
	result += fmt.Sprintf("[%s exit %d, %s]", backend, exitCode, elapsed)
	if exitCode != 0 {
		if hint := shellFailureHint(backend, command); hint != "" {
			result += "\n" + hint
		}
	}

	if exitCode != 0 {
		return result, fmt.Errorf("exit code %d", exitCode)
	}
	return result, nil
}

func shellFailureHint(backend, command string) string {
	if backend != "powershell" && backend != "pwsh" {
		return ""
	}
	if looksLikePowerShellCurlAlias(command) {
		return "hint: active shell backend is PowerShell. In Windows PowerShell, curl may be an alias for Invoke-WebRequest, so curl flags like -s can fail. Use curl.exe for real curl, or use Invoke-WebRequest -Uri ... -UseBasicParsing."
	}
	if !looksLikeBashSyntax(command) {
		return ""
	}
	return "hint: active shell backend is PowerShell. This command looks like bash syntax. Use PowerShell syntax (Get-ChildItem, Select-String, $null) or switch Settings > Tools > Shell backend to wsl/bash."
}

func looksLikePowerShellCurlAlias(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))
	if strings.HasPrefix(lower, "curl.exe ") {
		return false
	}
	if !strings.HasPrefix(lower, "curl ") && !strings.Contains(lower, "| curl ") && !strings.Contains(lower, "; curl ") {
		return false
	}
	return strings.Contains(lower, " -s") ||
		strings.Contains(lower, " --") ||
		strings.Contains(lower, " -h") ||
		strings.Contains(lower, " -x")
}

func looksLikeBashSyntax(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "/dev/null") ||
		strings.Contains(lower, " 2>") ||
		strings.Contains(lower, " || ") ||
		strings.Contains(lower, " && ") && strings.Contains(lower, "ls ")
}

func detectShellBackend(forced string) string {
	if forced != "" && forced != "auto" {
		return forced
	}
	cfg, _ := settings.Load()
	if cfg != nil && cfg.Tools.ShellBackend != "" && cfg.Tools.ShellBackend != "auto" {
		return cfg.Tools.ShellBackend
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	if isWSL() {
		return "bash"
	}
	return "bash"
}

func shellCommand(ctx context.Context, backend, command, wd string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch backend {
	case "powershell", "pwsh":
		exe := "powershell.exe"
		if backend == "pwsh" {
			exe = "pwsh"
		}
		cmd = exec.CommandContext(ctx, exe, "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
		cmd.Dir = NormalizeHostPath(wd)
		return cmd, nil
	case "cmd":
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
		cmd.Dir = NormalizeHostPath(wd)
		return cmd, nil
	case "wsl":
		if runtime.GOOS != "windows" {
			cmd = exec.CommandContext(ctx, "/bin/bash", "-lc", command)
			cmd.Dir = NormalizeHostPath(wd)
			return cmd, nil
		}
		wslDir := WindowsPathToWSL(wd)
		wrapped := command
		if wslDir != "" {
			wrapped = "cd " + shellQuote(wslDir) + " && " + command
		}
		return exec.CommandContext(ctx, "wsl.exe", "bash", "-lc", wrapped), nil
	case "bash", "":
		cmd = exec.CommandContext(ctx, "/bin/bash", "-lc", command)
		cmd.Dir = NormalizeHostPath(wd)
		return cmd, nil
	default:
		return nil, fmt.Errorf("unsupported shell backend %q", backend)
	}
}

func cloneCommandWithContext(ctx context.Context, cmd *exec.Cmd) *exec.Cmd {
	next := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	next.Dir = cmd.Dir
	next.Env = cmd.Env
	applyHiddenWindow(next)
	return next
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
