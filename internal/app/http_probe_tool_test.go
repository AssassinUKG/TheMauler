package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
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
		"out='/mnt/c/work/http_probe_connected.htb_20260706_120000.txt'",
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
	if strings.Contains(cmd, "out=''") {
		t.Fatalf("command used empty output path:\n%s", cmd)
	}
	if !strings.Contains(cmd, "out='C:/work/http_probe_connected.txt'") {
		t.Fatalf("command did not fall back to display path:\n%s", cmd)
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
