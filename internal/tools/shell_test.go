package tools

import (
	"runtime"
	"strings"
	"testing"
)

func TestDetectShellBackendAuto(t *testing.T) {
	got := detectShellBackend("auto")
	if runtime.GOOS == "windows" {
		if got != "powershell" {
			t.Fatalf("auto backend on windows = %q, want powershell", got)
		}
		return
	}
	if got != "bash" {
		t.Fatalf("auto backend on non-windows = %q, want bash", got)
	}
}

func TestDetectShellBackendForced(t *testing.T) {
	if got := detectShellBackend("wsl"); got != "wsl" {
		t.Fatalf("forced backend = %q, want wsl", got)
	}
}

func TestShellFailureHintForBashSyntaxInPowerShell(t *testing.T) {
	cmd := `ls *.md 2>/dev/null || echo "none"`
	if hint := shellFailureHint("powershell", cmd); hint == "" {
		t.Fatalf("expected PowerShell/bash syntax hint")
	}
	if hint := shellFailureHint("bash", cmd); hint != "" {
		t.Fatalf("bash backend should not warn about bash syntax: %s", hint)
	}
}

func TestShellFailureHintForCurlAliasInPowerShell(t *testing.T) {
	cmd := `curl -s http://worldtimeapi.org/api/ip`
	hint := shellFailureHint("powershell", cmd)
	if !strings.Contains(hint, "curl.exe") || !strings.Contains(hint, "Invoke-WebRequest") {
		t.Fatalf("expected curl alias hint, got %q", hint)
	}
	if hint := shellFailureHint("powershell", `curl.exe -s http://example.com`); hint != "" {
		t.Fatalf("curl.exe should not trigger alias hint: %s", hint)
	}
}
