package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

type httpProbeTool struct{ app *App }

func (t *httpProbeTool) Name() string { return "http_probe" }

func (t *httpProbeTool) Destructive() bool { return false }

func (t *httpProbeTool) Description() string {
	return "Run a bounded HTTP probe pipeline through the configured shell backend and return a compact summary plus a raw artifact path saved in the workspace root. Use instead of many repeated curl calls when checking base paths, redirects, headers, server/version, common access errors, cookies/auth headers, or small path lists. Inspect the saved artifact with grep/read if the raw response matters, instead of rerunning the same live probe."
}

func (t *httpProbeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "url": {"type": "string", "description": "Base URL to probe, e.g. http://boxname.htb/"},
    "paths": {"type": "array", "items": {"type": "string"}, "description": "Optional paths to probe relative to url. Defaults to /, /admin/, /robots.txt."},
    "max_paths": {"type": "integer", "minimum": 1, "maximum": 12, "description": "Maximum paths to probe. Default 8."},
    "timeout": {"type": "integer", "minimum": 2, "maximum": 30, "description": "Per-request curl max-time seconds. Default 10."},
    "headers": {"type": "array", "items": {"type": "string"}, "description": "Optional literal headers, e.g. Authorization: Basic ..."}
  },
  "required": ["url"]
}`)
}

type httpProbeArgs struct {
	URL      string   `json:"url"`
	Paths    []string `json:"paths"`
	MaxPaths int      `json:"max_paths"`
	Timeout  int      `json:"timeout"`
	Headers  []string `json:"headers"`
}

func (t *httpProbeTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args httpProbeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("http_probe: bad params: %w", err)
	}
	base := strings.TrimSpace(args.URL)
	if base == "" {
		return "", fmt.Errorf("http_probe: url is required")
	}
	if err := t.app.enforceEngagementScope(ctx, t.Name(), base); err != nil {
		return "", err
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return "", fmt.Errorf("http_probe: invalid url: %w", err)
	}
	paths := normaliseProbePaths(args.Paths, args.MaxPaths)
	timeout := args.Timeout
	if timeout <= 0 || timeout > 30 {
		timeout = 10
	}
	artifactPath, shellArtifactPath, err := httpProbeArtifactPath(base, t.app.cfg.Tools)
	if err != nil {
		return "", err
	}
	command := buildHTTPProbeCommand(base, paths, args.Headers, timeout, shellArtifactPath, artifactPath)
	cmdArgs, _ := marshalToolArgsNoHTMLEscape(map[string]any{
		"command": command,
		"timeout": min(300, max(30, timeout*len(paths)+15)),
		"verbose": false,
	})
	out, runErr := t.app.registry.Run(ctx, llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: cmdArgs}})
	artifactOK, artifactErr := ensureHTTPProbeArtifact(artifactPath, out)
	summary := summarizeHTTPProbeOutput(out, artifactPath)
	if artifactErr != nil {
		summary = strings.TrimSpace(summary) + "\nArtifact unavailable: " + artifactErr.Error()
	}
	var artifacts []string
	if artifactOK {
		artifacts = []string{artifactPath}
		t.app.emit("mauler:workspace_changed", filepath.Dir(artifactPath))
	}
	t.app.recordLedger(ledger.Event{
		Kind:      "pipeline",
		Source:    "tool",
		Tool:      t.Name(),
		Status:    firstNonEmpty(statusFromErr(runErr), "done"),
		Message:   base,
		Detail:    strings.Join(paths, ", "),
		Output:    summary,
		Artifacts: artifacts,
		Metadata: map[string]string{
			"paths":   fmt.Sprintf("%d", len(paths)),
			"timeout": fmt.Sprintf("%d", timeout),
		},
	})
	if runErr != nil {
		return summary, runErr
	}
	return summary, nil
}

func ensureHTTPProbeArtifact(artifactPath, capturedOutput string) (bool, error) {
	if strings.TrimSpace(artifactPath) == "" {
		return false, fmt.Errorf("empty artifact path")
	}
	if info, err := os.Stat(artifactPath); err == nil && info.Size() > 0 {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o750); err != nil {
		return false, err
	}
	body := strings.TrimRight(capturedOutput, "\r\n")
	if body == "" {
		body = "[http_probe produced no captured shell output]"
	}
	if err := os.WriteFile(artifactPath, []byte(body+"\n"), 0o640); err != nil {
		return false, err
	}
	return true, nil
}

func normaliseProbePaths(paths []string, maxPaths int) []string {
	if len(paths) == 0 {
		paths = []string{"/", "/admin/", "/robots.txt"}
	}
	if maxPaths <= 0 || maxPaths > 12 {
		maxPaths = 8
	}
	seen := map[string]bool{}
	out := make([]string, 0, min(len(paths), maxPaths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxPaths {
			break
		}
	}
	if len(out) == 0 {
		return []string{"/"}
	}
	return out
}

func httpProbeArtifactPath(base string, cfg settings.ToolsConfig) (string, string, error) {
	u, _ := url.Parse(base)
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		host = "target"
	}
	safe := regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(host, "_")
	dir := "."
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", err
	}
	hostPath, err := filepath.Abs(filepath.Join(dir, fmt.Sprintf("http_probe_%s_%s.txt", safe, time.Now().Format("20060102_150405"))))
	if err != nil {
		return "", "", err
	}
	displayPath := filepath.ToSlash(hostPath)
	shellPath := displayPath
	if strings.EqualFold(strings.TrimSpace(cfg.ShellBackend), "wsl") {
		shellPath = tools.WindowsPathToWSL(displayPath)
		if strings.TrimSpace(shellPath) == "" {
			return "", "", fmt.Errorf("http_probe: could not convert artifact path %q to WSL path", displayPath)
		}
	}
	return displayPath, shellPath, nil
}

func buildHTTPProbeCommand(base string, paths, headers []string, timeout int, shellArtifactPath, displayArtifactPath string) string {
	if strings.TrimSpace(shellArtifactPath) == "" {
		shellArtifactPath = displayArtifactPath
	}
	var sb strings.Builder
	sb.WriteString("set -o pipefail 2>/dev/null || true; ")
	sb.WriteString("out=" + terminalShellQuote(shellArtifactPath) + "; mkdir -p \"$(dirname \"$out\")\"; : > \"$out\"; ")
	for _, p := range paths {
		target := joinProbeURL(base, p)
		sb.WriteString("printf '%s\\n' " + terminalShellQuote("=== "+p+" "+target+" ===") + " | tee -a \"$out\" >/dev/null; ")
		sb.WriteString("curl -skS -i -L --path-as-is --max-time ")
		sb.WriteString(fmt.Sprintf("%d ", timeout))
		for _, header := range headers {
			header = strings.TrimSpace(header)
			if header == "" {
				continue
			}
			sb.WriteString("-H " + terminalShellQuote(header) + " ")
		}
		sb.WriteString(terminalShellQuote(target))
		sb.WriteString(" 2>&1 | tee -a \"$out\"; printf '\\n' | tee -a \"$out\" >/dev/null; ")
	}
	sb.WriteString("printf '%s\\n' " + terminalShellQuote("__MAULER_HTTP_PROBE_ARTIFACT__="+displayArtifactPath))
	return sb.String()
}

func joinProbeURL(base, p string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if p == "/" {
		u.Path = "/"
		u.RawQuery = ""
		return u.String()
	}
	u.Path = path.Join("/", strings.TrimPrefix(p, "/"))
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawQuery = ""
	return u.String()
}

func summarizeHTTPProbeOutput(out, artifactPath string) string {
	type item struct {
		path     string
		status   string
		server   string
		location string
		ctype    string
		error    string
	}
	var items []item
	var current *item
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "=== ") {
			fields := strings.Fields(trimmed)
			items = append(items, item{})
			current = &items[len(items)-1]
			if len(fields) >= 2 {
				current.path = fields[1]
			}
			continue
		}
		if current == nil {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(trimmed, "HTTP/"):
			current.status = trimmed
		case strings.HasPrefix(lower, "server:"):
			current.server = strings.TrimSpace(trimmed[len("server:"):])
		case strings.HasPrefix(lower, "location:"):
			current.location = strings.TrimSpace(trimmed[len("location:"):])
		case strings.HasPrefix(lower, "content-type:"):
			current.ctype = strings.TrimSpace(trimmed[len("content-type:"):])
		case strings.Contains(lower, "could not resolve") || strings.Contains(lower, "failed to connect") || strings.Contains(lower, "timed out"):
			current.error = trimmed
		}
	}
	var sb strings.Builder
	sb.WriteString("HTTP probe summary\n")
	for _, item := range items {
		if item.path == "" {
			continue
		}
		sb.WriteString("- " + item.path + ": ")
		parts := []string{}
		if item.status != "" {
			parts = append(parts, item.status)
		}
		if item.server != "" {
			parts = append(parts, "server="+item.server)
		}
		if item.location != "" {
			parts = append(parts, "location="+item.location)
		}
		if item.ctype != "" {
			parts = append(parts, "type="+item.ctype)
		}
		if item.error != "" {
			parts = append(parts, "error="+item.error)
		}
		if len(parts) == 0 {
			parts = append(parts, "no headers captured")
		}
		sb.WriteString(strings.Join(parts, "; ") + "\n")
	}
	sb.WriteString("Artifact: " + artifactPath)
	return strings.TrimSpace(sb.String())
}

func statusFromErr(err error) string {
	if err != nil {
		return "error"
	}
	return "done"
}
