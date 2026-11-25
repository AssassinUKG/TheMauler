package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestHTTPProbeHelpersBuildBoundedCommand(t *testing.T) {
	paths := normaliseProbePaths([]string{"", "admin/", "/admin/", "/robots.txt", "/extra"}, 3)
	if strings.Join(paths, ",") != "/admin/,/robots.txt,/extra" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	cmd := buildHTTPProbeCommand("http://connected.htb/base", paths, []string{"X-Test: yes"}, 7, "/mnt/c/work/http_probe_connected.htb_20260706_120000.txt", "C:/work/http_probe_connected.htb_20260706_120000.txt")
	for _, want := range []string{
		"curl -skS -i -L",
		"--max-time 7",
		"X-Test: yes",
		"http://connected.htb/admin/",
		"tee -a '/mnt/c/work/http_probe_connected.htb_20260706_120000.txt'",
		"exit \"$probe_rc\"",
		"__MAULER_HTTP_PROBE_ARTIFACT__=C:/work/http_probe_connected.htb_20260706_120000.txt",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}
}

func TestHTTPProbeArtifactPathUsesWorkspaceRootAndWSLPath(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	display, shellPath, err := httpProbeArtifactPath("http://10.129.26.26/", settings.ToolsConfig{ShellBackend: "wsl"})
	if err != nil {
		t.Fatalf("artifact path failed: %v", err)
	}
	if !strings.Contains(filepath.Base(display), "http_probe_10.129.26.26_") {
		t.Fatalf("expected root-level http_probe artifact, got %q", display)
	}
	if strings.TrimSpace(shellPath) == "" {
		t.Fatalf("shell path was empty")
	}
	if strings.Contains(filepath.ToSlash(display), ".mauler_artifacts/http_probe") {
		t.Fatalf("artifact should be root-level now, got %q", display)
	}
}

func TestBuildHTTPProbeCommandNeverUsesEmptyOut(t *testing.T) {
	cmd := buildHTTPProbeCommand("http://connected.htb", []string{"/"}, nil, 5, "", "C:/work/http_probe_connected.txt")
	if !strings.Contains(cmd, "tee -a 'C:/work/http_probe_connected.txt'") {
		t.Fatalf("command did not fall back to display path:\n%s", cmd)
	}
	if strings.Contains(cmd, "$out") {
		t.Fatalf("command should use the quoted literal artifact path, not a fragile nested-shell variable:\n%s", cmd)
	}
}

func TestLiveHTTPProbeWSLCommand(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("MAULER_LIVE_ROUTER_TARGET"))
	if target == "" {
		t.Skip("set MAULER_LIVE_ROUTER_TARGET to an explicitly authorised private router IP")
	}
	ip := net.ParseIP(target)
	if ip == nil || !ip.IsPrivate() {
		t.Fatalf("MAULER_LIVE_ROUTER_TARGET must be a private IP, got %q", target)
	}
	dir := t.TempDir()
	displayPath := filepath.ToSlash(filepath.Join(dir, "http-live.txt"))
	shellPath := tools.WindowsPathToWSL(displayPath)
	command := buildHTTPProbeCommand("http://"+target, []string{"/", "/definitely-not-a-real-mauler-regression-path-7f3b"}, nil, 5, shellPath, displayPath)
	raw, err := json.Marshal(map[string]any{"command": command, "backend": "wsl", "timeout": 30, "verbose": true})
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := (&tools.Shell{TimeoutSecs: 30}).Run(context.Background(), raw)
	t.Logf("command=%s\nresult=%s", command, out)
	if runErr != nil {
		t.Fatal(runErr)
	}
	data, err := os.ReadFile(displayPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact := string(data)
	if !strings.Contains(artifact, "=== / http://"+target+"/ ===") || !strings.Contains(artifact, "HTTP/1.1 200") {
		t.Fatalf("live artifact lacks expected root response:\n%s", artifact)
	}
	if strings.Contains(artifact, "tee: '") || strings.Contains(artifact, "unexpected EOF") {
		t.Fatalf("live artifact contains shell-wrapper errors:\n%s", artifact)
	}
	summary := summarizeHTTPProbeOutput(out, displayPath)
	if !strings.Contains(summary, "- /: HTTP/1.1 200") || !strings.Contains(summary, "/definitely-not-a-real-mauler-regression-path-7f3b: HTTP/1.1 404") {
		t.Fatalf("live compact summary did not preserve both path verdicts:\n%s", summary)
	}
}

func TestEnsureHTTPProbeArtifactWritesFallbackWhenShellFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http_probe_target_20260706_120000.txt")

	ok, err := ensureHTTPProbeArtifact(path, "HTTP/1.1 200 OK\nServer: test")
	if err != nil {
		t.Fatalf("ensure artifact failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected artifact to be marked available")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("artifact was not written: %v", err)
	}
	if !strings.Contains(string(data), "HTTP/1.1 200 OK") {
		t.Fatalf("artifact did not contain captured output: %q", string(data))
	}
}

func TestEnsureHTTPProbeArtifactKeepsExistingShellArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http_probe_target_20260706_120000.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("shell file\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	ok, err := ensureHTTPProbeArtifact(path, "fallback")
	if err != nil || !ok {
		t.Fatalf("expected existing artifact to remain available, ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "shell file\n" {
		t.Fatalf("existing artifact was overwritten: %q", string(data))
	}
}

func TestSummarizeHTTPProbeOutput(t *testing.T) {
	raw := `=== / http://connected.htb/ ===
HTTP/1.1 301 Moved Permanently
Server: Apache/2.4.6
Location: http://connected.htb/admin/
Content-Type: text/html

=== /admin/ http://connected.htb/admin/ ===
HTTP/1.1 403 Forbidden
Server: Apache/2.4.6
Content-Type: text/html`

	summary := summarizeHTTPProbeOutput(raw, "http_probe_connected.htb_20260706_120000.txt")
	for _, want := range []string{
		"/: HTTP/1.1 301 Moved Permanently",
		"location=http://connected.htb/admin/",
		"/admin/: HTTP/1.1 403 Forbidden",
		"Artifact: http_probe_connected.htb_20260706_120000.txt",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestSummarizeHTTPProbeOutputWarnsOnIndistinguishablePaths(t *testing.T) {
	raw := `=== /admin http://router.test/admin ===
HTTP/1.1 200 OK
Content-Type: text/html
Content-Length: 27

<html>router shell</html>
=== /definitely-not-real http://router.test/definitely-not-real ===
HTTP/1.1 200 OK
Content-Type: text/html
Content-Length: 27

<html>router shell</html>
__MAULER_HTTP_PROBE_ARTIFACT__=C:/work/router.txt
[wsl exit 0, 125ms]`

	summary := summarizeHTTPProbeOutput(raw, "router.txt")
	for _, want := range []string{"[http_fallback_warning]", "2 distinct paths", "path existence", "body_sha256"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("fallback summary missing %q:\n%s", want, summary)
		}
	}
}
