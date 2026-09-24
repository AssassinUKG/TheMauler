package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mauler/internal/ledger"
	"mauler/internal/llm"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type runScriptTool struct{ app *App }

type runScriptArgs struct {
	Code           string `json:"code"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxToolCalls   int    `json:"max_tool_calls"`
}

type scriptToolRequest struct {
	ID   int             `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

type scriptToolResponse struct {
	OK     bool   `json:"ok"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (t *runScriptTool) Name() string { return "run_script" }

func (t *runScriptTool) Description() string {
	return "Run a short Python orchestration script that can call Mauler tools through helper functions: read, read_many, glob, grep, write, edit, shell, web_search, fetch_url, memory, todo, and read_tool_result. Use this to collapse multi-step local workflows into one tool call."
}

func (t *runScriptTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "code": {"type": "string", "description": "Python code to run. Helper functions are already defined."},
    "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 120, "description": "Optional wall-clock timeout. Default 30, max 120."},
    "max_tool_calls": {"type": "integer", "minimum": 0, "maximum": 50, "description": "Optional inner tool-call cap. Default 20, max 50."}
  },
  "required": ["code"]
}`)
}

func (t *runScriptTool) Destructive() bool { return true }

func (t *runScriptTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args runScriptArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("run_script: bad params: %w", err)
	}
	args.Code = strings.TrimSpace(args.Code)
	if args.Code == "" {
		return "", fmt.Errorf("run_script: code is required")
	}
	timeout := boundedValue(args.TimeoutSeconds, 30, 1, 120)
	maxToolCalls := boundedValue(args.MaxToolCalls, 20, 0, 50)
	return t.app.runPythonToolScript(ctx, args.Code, timeout, maxToolCalls)
}

func (a *App) runPythonToolScript(parent context.Context, userCode string, timeoutSeconds int, maxToolCalls int) (string, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	dir, err := os.MkdirTemp("", "mauler-run-script-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	scriptPath := filepath.Join(dir, "script.py")
	if err := os.WriteFile(scriptPath, []byte(buildRunScriptPython(userCode)), 0o600); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "python", "-u", scriptPath)
	cmd.Dir = currentWorkingDir()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("run_script: start python: %w", err)
	}

	var stderrBuf bytes.Buffer
	var stdoutBuf bytes.Buffer
	lastTool := ""
	failedTool := ""
	failedError := ""
	stderrDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&stderrBuf, stderr)
		stderrDone <- copyErr
	}()

	toolCalls := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "__MAULER_TOOL__") {
			stdoutBuf.WriteString(line)
			stdoutBuf.WriteByte('\n')
			continue
		}
		var req scriptToolRequest
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "__MAULER_TOOL__")), &req); err != nil {
			_ = writeScriptToolResponse(stdin, scriptToolResponse{OK: false, Error: "bad tool request: " + err.Error()})
			continue
		}
		toolCalls++
		lastTool = req.Tool
		if maxToolCalls > 0 && toolCalls > maxToolCalls {
			failedTool = req.Tool
			failedError = fmt.Sprintf("run_script inner tool-call cap exceeded (%d)", maxToolCalls)
			_ = writeScriptToolResponse(stdin, scriptToolResponse{OK: false, Error: fmt.Sprintf("run_script inner tool-call cap exceeded (%d)", maxToolCalls)})
			continue
		}
		start := time.Now()
		call := llm.ToolCallDef{Function: llm.FunctionCall{Name: req.Tool, Arguments: req.Args}}
		run := runControlFromContext(ctx)
		controlled, controlBlock := false, ""
		if run != nil {
			controlled, controlBlock = a.prepareControlledTool(run, call)
		}
		var result string
		var runErr error
		if controlBlock != "" {
			result = "control plane blocked run_script inner tool call: " + controlBlock
			runErr = fmt.Errorf("%s", result)
		} else {
			result, runErr = a.registry.Run(ctx, call)
		}
		durMs := time.Since(start).Milliseconds()
		status := "done"
		resp := scriptToolResponse{OK: true, Result: result}
		if runErr != nil {
			status = "error"
			failedTool = req.Tool
			failedError = runErr.Error()
			resp = scriptToolResponse{OK: false, Result: result, Error: runErr.Error()}
		}
		if run != nil {
			run.addTool(req.Tool, trimRunText(string(req.Args)), trimRunText(result), status, durMs)
			a.recordControlledToolOutcome(run, controlled, call, runErr)
		}
		a.recordLedger(ledger.Event{
			Kind:       "run_script_tool_call",
			Source:     "run_script",
			Tool:       req.Tool,
			Status:     status,
			Input:      string(req.Args),
			Output:     truncateRunes(result, 4000),
			Error:      resp.Error,
			DurationMs: durMs,
		})
		if err := writeScriptToolResponse(stdin, resp); err != nil {
			break
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	stderrErr := <-stderrDone
	out := strings.TrimSpace(stdoutBuf.String())
	errText := strings.TrimSpace(stderrBuf.String())
	if ctx.Err() != nil {
		return formatRunScriptResult("timeout", toolCalls, lastTool, failedTool, fmt.Sprintf("timeout after %d seconds", timeoutSeconds), out, errText, "", maxToolCalls, timeoutSeconds), fmt.Errorf("run_script: timeout after %d seconds", timeoutSeconds)
	}
	if scanErr != nil {
		return formatRunScriptResult("error", toolCalls, lastTool, failedTool, scanErr.Error(), out, errText, "", maxToolCalls, timeoutSeconds), fmt.Errorf("run_script: read stdout: %w", scanErr)
	}
	if stderrErr != nil {
		return formatRunScriptResult("error", toolCalls, lastTool, failedTool, stderrErr.Error(), out, errText, "", maxToolCalls, timeoutSeconds), fmt.Errorf("run_script: read stderr: %w", stderrErr)
	}
	state := "done"
	if waitErr != nil {
		state = "error"
		if failedError == "" {
			failedError = waitErr.Error()
		}
		return formatRunScriptResult(state, toolCalls, lastTool, failedTool, failedError, out, errText, waitErr.Error(), maxToolCalls, timeoutSeconds), waitErr
	}
	return formatRunScriptResult(state, toolCalls, lastTool, failedTool, failedError, out, errText, "", maxToolCalls, timeoutSeconds), nil
}

func formatRunScriptResult(state string, toolCalls int, lastTool, failedTool, failedError, stdoutText, stderrText, exitText string, maxToolCalls int, timeoutSeconds int) string {
	if strings.TrimSpace(state) == "" {
		state = "done"
	}
	if strings.TrimSpace(lastTool) == "" {
		lastTool = "-"
	}
	if strings.TrimSpace(failedTool) == "" {
		failedTool = "-"
	}
	if strings.TrimSpace(failedError) == "" {
		failedError = "-"
	}
	nextTool := "proceed"
	switch state {
	case "timeout":
		nextTool = "narrow script, increase timeout, or use background shell/job"
	case "error":
		nextTool = "inspect failed_tool/error, fix script or call the failed tool directly"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[run_script_result state=%s]\n", state)
	sb.WriteString("contract:\n")
	fmt.Fprintf(&sb, "  state: %s\n", state)
	fmt.Fprintf(&sb, "  inner_tool_calls: %d\n", toolCalls)
	fmt.Fprintf(&sb, "  max_tool_calls: %d\n", maxToolCalls)
	fmt.Fprintf(&sb, "  timeout_seconds: %d\n", timeoutSeconds)
	fmt.Fprintf(&sb, "  last_tool: %s\n", lastTool)
	fmt.Fprintf(&sb, "  failed_tool: %s\n", failedTool)
	fmt.Fprintf(&sb, "  error: %s\n", oneLineContractValue(failedError))
	fmt.Fprintf(&sb, "  result_id: -\n")
	fmt.Fprintf(&sb, "  next_tool: %s\n", nextTool)
	sb.WriteString("  do_not_repeat: do not rerun the same script if the contract already identifies the failed tool/error; inspect/fix the specific step\n")
	sb.WriteString(fmt.Sprintf("run_script finished: inner_tool_calls=%d\n", toolCalls))
	if stdoutText = strings.TrimSpace(stdoutText); stdoutText != "" {
		sb.WriteString("\n[stdout]\n")
		sb.WriteString(truncateRunes(stdoutText, 12000))
	}
	if stderrText = strings.TrimSpace(stderrText); stderrText != "" {
		sb.WriteString("\n[stderr]\n")
		sb.WriteString(truncateRunes(stderrText, 8000))
	}
	if strings.TrimSpace(exitText) != "" {
		sb.WriteString("\n[exit]\n")
		sb.WriteString(exitText)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func oneLineContractValue(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return "-"
	}
	return truncateRunes(value, 240)
}

func writeScriptToolResponse(w io.Writer, resp scriptToolResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

func buildRunScriptPython(userCode string) string {
	encoded, _ := json.Marshal(userCode)
	return fmt.Sprintf(`import json, sys

_next_id = 0

def mauler_tool(tool, **kwargs):
    global _next_id
    _next_id += 1
    print("__MAULER_TOOL__" + json.dumps({"id": _next_id, "tool": tool, "args": kwargs}, separators=(",", ":")), flush=True)
    line = sys.stdin.readline()
    if not line:
        raise RuntimeError("Mauler tool bridge closed")
    resp = json.loads(line)
    if not resp.get("ok"):
        err = resp.get("error") or "tool failed"
        result = resp.get("result")
        if result:
            err += "\n" + result
        raise RuntimeError(err)
    return resp.get("result", "")

def read(path, start_line=0, end_line=0):
    args = {"path": path}
    if start_line:
        args["start_line"] = start_line
    if end_line:
        args["end_line"] = end_line
    return mauler_tool("read", **args)

def read_many(paths):
    return mauler_tool("read", paths=paths)

def glob(pattern):
    return mauler_tool("glob", pattern=pattern)

def grep(pattern, path="", glob="", case_sensitive=True):
    args = {"pattern": pattern, "case_sensitive": case_sensitive}
    if path:
        args["path"] = path
    if glob:
        args["glob"] = glob
    return mauler_tool("grep", **args)

def write(path, content, append=False):
    return mauler_tool("write", path=path, content=content, append=append)

def edit(path, old_string, new_string):
    return mauler_tool("edit", path=path, old=old_string, new=new_string)

def shell(command, timeout=120, background=False):
    return mauler_tool("shell", command=command, timeout=timeout, background=background)

def web_search(query, max_results=5):
    return mauler_tool("web_search", query=query, max_results=max_results)

def fetch_url(url, max_chars=20000):
    return mauler_tool("fetch_url", url=url, max_chars=max_chars)

def memory(action, query="", content=""):
    return mauler_tool("memory", action=action, query=query, content=content)

def todo(action, **kwargs):
    return mauler_tool("todo_" + action, **kwargs)

def progress(action="read", content="", section="", path=""):
    args = {"action": action}
    if content:
        args["content"] = content
    if section:
        args["section"] = section
    if path:
        args["path"] = path
    return mauler_tool("progress", **args)

def read_tool_result(result_id, offset=0, limit=8000):
    return mauler_tool("read_tool_result", result_id=result_id, offset=offset, limit=limit)

_USER_CODE = %s
exec(compile(_USER_CODE, "<mauler-run-script>", "exec"), globals(), globals())
`, string(encoded))
}

func currentWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return "."
	}
	return wd
}
