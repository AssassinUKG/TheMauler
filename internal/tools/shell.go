package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Shell runs a command in the configured platform shell.
type Shell struct {
	TimeoutSecs int
}

func (t *Shell) Name() string      { return "shell" }
func (t *Shell) Destructive() bool { return true }

func (t *Shell) Description() string {
	backend := detectShellBackend("")
	distro := activeWSLDistro()
	if backend == "wsl" && distro != "" {
		backend += " (" + distro + ")"
		if user := activeWSLUser(); user != "" {
			backend += " as " + user
		}
	}
	return fmt.Sprintf("Run a shell command in the current working directory using the %s backend. "+
		"On Windows auto uses PowerShell; on Linux/WSL auto uses bash. If the backend is WSL, commands run inside the configured WSL distro. Use platform-native paths for the active backend. "+
		"By default each call runs in a fresh isolated shell (reliable, cannot hang); set session=true only when state must persist (interactive/reverse shell, persistent cd/env). "+
		"Write commands as plain text with literal operators (&, >, <, |, \"); never HTML-escape them (do not write &amp;, &gt;, &lt;, &quot;).", backend)
}

func (t *Shell) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The command to run in the active shell"},
    "timeout": {"type": "integer", "description": "Timeout in seconds (default 120, max 300). Use 120-300 for long-running scans/enumeration."},
    "session": {"type": "boolean", "description": "Default false: run in a fresh isolated shell — reliable, deterministic, cannot hang the session. Set true ONLY when state must persist across commands: an interactive shell, a reverse/bind shell, or a cd/export/variable that later commands depend on. Most enumeration (nmap, curl, gobuster, ffuf) should leave this false."}
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
	hadEntities := htmlEntityRE.MatchString(command)
	prepared, err := PrepareShellCommand(command)
	if err != nil {
		return "", fmt.Errorf("shell: %w", err)
	}
	command = prepared
	timeoutSecs := defaultTimeout
	if timeoutSecs <= 0 {
		timeoutSecs = 120 // match the schema-advertised default so omitted timeouts don't kill long scans
	}
	if requestedTimeout > 0 && requestedTimeout <= 300 {
		timeoutSecs = requestedTimeout
	}

	backend := detectShellBackend(forcedBackend)
	distro := ""
	user := ""
	if backend == "wsl" {
		distro = activeWSLDistro()
		user = activeWSLUser()
		if runtime.GOOS == "windows" {
			if unwrapped, ok := unwrapNestedWSLCommand(command); ok {
				command = unwrapped
			}
			command = forceNonInteractiveSudo(command)
			command = wrapWSLCommandForCancellation(command, shellRunID())
		}
	}
	wd, _ := os.Getwd()
	cmd, err := shellCommand(ctx, backend, distro, command, wd)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()
	cmd = cloneCommandWithContext(ctx, cmd)
	cmd.WaitDelay = 2 * time.Second
	if backend == "wsl" && runtime.GOOS == "windows" {
		runID := extractWSLRunID(command)
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				cancelWSLCommandGroup(distro, user, runID)
			case <-done:
			}
		}()
		defer close(done)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond)

	var sb strings.Builder
	if stdout.Len() > 0 {
		sb.WriteString(decodeCommandOutput(stdout.Bytes()))
	}
	if stderr.Len() > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(decodeCommandOutput(stderr.Bytes()))
	}

	if ctx.Err() == context.DeadlineExceeded {
		result := strings.TrimRight(sb.String(), "\n\r ")
		if result != "" {
			result += "\n"
		}
		result += fmt.Sprintf("[%s timed out after %ds, %s]", backend, timeoutSecs, elapsed)
		result += "\nRecovery: this command hit Mauler's shell timeout, not a task failure. Retry with a larger timeout (120-300 seconds for nmap/gobuster/ffuf/hydra), narrow the scan, or write long-running output to a file and continue from partial results."
		return result, fmt.Errorf("shell: timed out after %ds", timeoutSecs)
	}
	if ctx.Err() == context.Canceled {
		result := strings.TrimRight(sb.String(), "\n\r ")
		if result != "" {
			result += "\n"
		}
		result += fmt.Sprintf("[%s cancelled after %s]", backend, elapsed)
		result += "\nRecovery: the user interrupted this command. Stop tool use now, summarize current progress, and wait for the next instruction."
		return result, fmt.Errorf("shell: cancelled")
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
		if hadEntities {
			result += "\nhint: your command HTML-escaped shell operators (&amp;, &gt;, &lt;). Write them literally — &, >, <, | — and do NOT add more escaping. TheMauler already unescapes, so escalating the escaping will not help."
		}
		if hint := shellFailureHint(backend, command); hint != "" {
			result += "\n" + hint
		}
	}

	if exitCode != 0 {
		return result, fmt.Errorf("exit code %d", exitCode)
	}
	return result, nil
}

func PrepareShellCommand(command string) (string, error) {
	command = NormalizeShellCommandText(command)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	if err := rejectProtectedShellMutation(command); err != nil {
		return "", err
	}
	return command, nil
}

func NormalizeShellCommandText(command string) string {
	return cleanShellCommand(command)
}

var htmlEntityRE = regexp.MustCompile(`&(amp|gt|lt|quot|#x?[0-9]+);`)

func cleanShellCommand(command string) string {
	command = strings.TrimSpace(command)
	// Unescape to a fixpoint. Local models routinely HTML-escape shell operators
	// (& > < ") and the escaping compounds across retries (&amp;amp;amp;...1). A fixed
	// 3-pass cap left deep escaping intact, which errored and made the model escalate the
	// escaping further — a doom loop. html.UnescapeString strictly shortens and converges,
	// so this terminates; the cap is only a defensive bound.
	for i := 0; i < 24; i++ {
		next := html.UnescapeString(command)
		if next == command {
			break
		}
		command = next
	}
	return strings.TrimSpace(command)
}

func decodeCommandOutput(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if looksUTF16LE(data) {
		return decodeUTF16(data, false)
	}
	if looksUTF16BE(data) {
		return decodeUTF16(data, true)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "?")
}

func looksUTF16LE(data []byte) bool {
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		return true
	}
	pairs := len(data) / 2
	if pairs < 4 {
		return false
	}
	zeroOdd := 0
	printableEven := 0
	for i := 0; i+1 < len(data); i += 2 {
		if data[i+1] == 0 {
			zeroOdd++
		}
		if data[i] >= 0x09 && data[i] <= 0x7e {
			printableEven++
		}
	}
	return zeroOdd*100/pairs >= 45 && printableEven*100/pairs >= 45
}

func looksUTF16BE(data []byte) bool {
	if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
		return true
	}
	pairs := len(data) / 2
	if pairs < 4 {
		return false
	}
	zeroEven := 0
	printableOdd := 0
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == 0 {
			zeroEven++
		}
		if data[i+1] >= 0x09 && data[i+1] <= 0x7e {
			printableOdd++
		}
	}
	return zeroEven*100/pairs >= 45 && printableOdd*100/pairs >= 45
}

func decodeUTF16(data []byte, bigEndian bool) string {
	if len(data) >= 2 {
		if (!bigEndian && data[0] == 0xff && data[1] == 0xfe) || (bigEndian && data[0] == 0xfe && data[1] == 0xff) {
			data = data[2:]
		}
	}
	u16 := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			u16 = append(u16, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			u16 = append(u16, uint16(data[i])|uint16(data[i+1])<<8)
		}
	}
	return string(utf16.Decode(u16))
}

func shellRunID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func wrapWSLCommandForCancellation(command, runID string) string {
	if runID == "" {
		runID = shellRunID()
	}
	return fmt.Sprintf("MAULER_RUN_ID=%s; bash -lc %s", shellQuote(runID), shellQuote(command))
}

func extractWSLRunID(command string) string {
	const marker = "MAULER_RUN_ID='"
	start := strings.Index(command, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(command[start:], "'")
	if end < 0 {
		return ""
	}
	return command[start : start+end]
}

func cancelWSLCommandGroup(distro, user, runID string) {
	if runtime.GOOS != "windows" || runID == "" {
		return
	}
	pidfile := "/tmp/mauler-shell-" + runID + ".pid"
	script := fmt.Sprintf("pid=$(cat %s 2>/dev/null || true); if [ -n \"$pid\" ]; then kill -TERM -- -\"$pid\" 2>/dev/null || true; sleep 0.5; kill -KILL -- -\"$pid\" 2>/dev/null || true; fi; rm -f %s",
		shellQuote(pidfile), shellQuote(pidfile))
	args := []string{}
	if distro = strings.TrimSpace(distro); distro != "" {
		args = append(args, "-d", distro)
	}
	if user = strings.TrimSpace(user); user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, "--", "bash", "-lc", script)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	applyHiddenWindow(cmd)
	_ = cmd.Run()
}

func unwrapNestedWSLCommand(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"wsl bash -c ", "wsl.exe bash -c "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		inner, ok := parseFirstQuotedArg(strings.TrimSpace(trimmed[len(prefix):]))
		if !ok {
			return command, false
		}
		return strings.TrimSpace(inner), true
	}
	return command, false
}

func parseFirstQuotedArg(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	quote := s[0]
	if quote != '\'' && quote != '"' {
		return "", false
	}
	var out strings.Builder
	escaped := false
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if escaped {
			out.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if ch == quote {
			return out.String(), true
		}
		out.WriteByte(ch)
	}
	return "", false
}

var sudoInvocationRE = regexp.MustCompile(`(^|[|&;({]\s*)sudo(\s+)`)

func forceNonInteractiveSudo(command string) string {
	if strings.Contains(command, "sudo -n") || strings.Contains(command, "sudo --non-interactive") {
		return command
	}
	return sudoInvocationRE.ReplaceAllStringFunc(command, func(match string) string {
		return strings.Replace(match, "sudo", "sudo -n", 1)
	})
}

// ForceNonInteractiveSudo rewrites bare `sudo` to `sudo -n` so a missing password
// fails fast instead of hanging the shell waiting for input. Exported for the shared
// terminal path, which otherwise blocks on the sudo prompt until it times out.
func ForceNonInteractiveSudo(command string) string {
	return forceNonInteractiveSudo(command)
}

func shellFailureHint(backend, command string) string {
	if backend == "wsl" {
		if strings.Contains(command, "sudo -n") {
			return "hint: sudo was run in non-interactive mode so Mauler cannot hang waiting for a password. If sudo credentials are needed, authenticate in Kali first with sudo -v, or configure a narrow NOPASSWD rule for the specific lab command."
		}
		if looksNestedWSLCommand(command) {
			return "hint: active shell backend is already WSL/Kali. Do not prefix commands with wsl, wsl.exe, or wsl sudo. Run the Linux command directly, for example: printf '%s\\n' '10.129.12.172 connected.htb' | sudo tee -a /etc/hosts"
		}
		if looksFragileSudoBashCommand(command) {
			return "hint: active shell backend is WSL/Kali. Avoid nested sudo bash -c quoting for simple file edits. Prefer: printf '%s\\n' '10.129.12.172 connected.htb' | sudo tee -a /etc/hosts"
		}
		return ""
	}
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

func looksNestedWSLCommand(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))
	return strings.HasPrefix(lower, "wsl ") || strings.HasPrefix(lower, "wsl.exe ") || strings.Contains(lower, " wsl ")
}

func looksFragileSudoBashCommand(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "sudo bash -c") || strings.Contains(lower, "sudo sh -c")
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
		(strings.Contains(lower, " && ") && strings.Contains(lower, "ls "))
}

func detectShellBackend(forced string) string {
	if forced != "" && forced != "auto" {
		return forced
	}
	if backend := configuredShellBackend(); backend != "" && backend != "auto" {
		return backend
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	if isWSL() {
		return "bash"
	}
	return "bash"
}

func activeWSLDistro() string {
	return configuredShellDistro()
}

func activeWSLUser() string {
	return configuredShellUser()
}

func shellCommand(ctx context.Context, backend, distro, command, wd string) (*exec.Cmd, error) {
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
		args := []string{}
		if distro = strings.TrimSpace(distro); distro != "" {
			args = append(args, "-d", distro)
		}
		if user := activeWSLUser(); user != "" {
			args = append(args, "--user", user)
		}
		if wslDir != "" {
			args = append(args, "--cd", wslDir)
		}
		args = append(args, "--", "bash", "-lc", command)
		return exec.CommandContext(ctx, "wsl.exe", args...), nil
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
