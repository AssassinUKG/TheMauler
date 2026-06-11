package tools

import (
	"mauler/internal/settings"
	"runtime"
	"strings"
	"testing"
)

func TestDetectShellBackendAuto(t *testing.T) {
	got := detectShellBackend("auto")
	if cfg, _ := settings.Load(); cfg != nil && cfg.Tools.ShellBackend != "" && cfg.Tools.ShellBackend != "auto" {
		if got != cfg.Tools.ShellBackend {
			t.Fatalf("auto backend should respect configured backend = %q, got %q", cfg.Tools.ShellBackend, got)
		}
		return
	}
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

func TestWSLShellCommandUsesConfiguredDistroAndCd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("wsl.exe command shape is Windows-specific")
	}
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	cfg.Tools.ShellUser = "root"
	if err := settings.Save(&cfg); err != nil {
		t.Fatal(err)
	}
	cmd, err := shellCommand(t.Context(), "wsl", "kali-linux", "pwd", `C:\Users\richa\Desktop\TheMauler`)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cmd.Args, " ")
	for _, want := range []string{"wsl.exe", "-d", "kali-linux", "--user", "root", "--cd", "/mnt/c/Users/richa/Desktop/TheMauler", "--", "bash", "-lc", "pwd"} {
		if !strings.Contains(got, want) {
			t.Fatalf("wsl command args missing %q: %#v", want, cmd.Args)
		}
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

func TestShellFailureHintForNestedWSLWhenAlreadyInWSL(t *testing.T) {
	hint := shellFailureHint("wsl", `wsl sudo bash -c 'echo x >> /etc/hosts'`)
	if !strings.Contains(hint, "already WSL/Kali") || !strings.Contains(hint, "sudo tee") {
		t.Fatalf("expected nested WSL/sudo tee hint, got %q", hint)
	}
}

func TestUnwrapNestedWSLCommand(t *testing.T) {
	got, ok := unwrapNestedWSLCommand(`wsl bash -c "nmap -sS -sV -p- 10.129.12.172" 2>&1`)
	if !ok {
		t.Fatalf("expected nested WSL command to unwrap")
	}
	want := `nmap -sS -sV -p- 10.129.12.172`
	if got != want {
		t.Fatalf("unwrapNestedWSLCommand = %q, want %q", got, want)
	}
}

func TestWrapWSLCommandForCancellation(t *testing.T) {
	got := wrapWSLCommandForCancellation(`nmap -sS -sV -p- 10.129.12.172`, "test123")
	for _, want := range []string{
		"MAULER_RUN_ID='test123'",
		"bash -lc",
		"nmap -sS -sV -p- 10.129.12.172",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapped WSL command missing %q: %s", want, got)
		}
	}
	for _, avoid := range []string{"wait \"$child\"", "exit \"$status\"", "/tmp/mauler-shell-test123.pid"} {
		if strings.Contains(got, avoid) {
			t.Fatalf("wrapped WSL command should not contain brittle process wrapper %q: %s", avoid, got)
		}
	}
	if runID := extractWSLRunID(got); runID != "test123" {
		t.Fatalf("extractWSLRunID = %q, want test123", runID)
	}
}

func TestForceNonInteractiveSudo(t *testing.T) {
	got := forceNonInteractiveSudo(`echo '10.129.12.172 connected.htb' | sudo tee -a /etc/hosts`)
	want := `echo '10.129.12.172 connected.htb' | sudo -n tee -a /etc/hosts`
	if got != want {
		t.Fatalf("forceNonInteractiveSudo = %q, want %q", got, want)
	}
	hint := shellFailureHint("wsl", got)
	if !strings.Contains(hint, "non-interactive") || !strings.Contains(hint, "sudo -v") {
		t.Fatalf("expected non-interactive sudo hint, got %q", hint)
	}
	// Idempotent: already-non-interactive sudo must not become `sudo -n -n`.
	if again := ForceNonInteractiveSudo(got); again != got {
		t.Fatalf("ForceNonInteractiveSudo not idempotent: %q", again)
	}
}

func TestCleanShellCommandDecodesHTMLEntities(t *testing.T) {
	got := cleanShellCommand(`curl -s http://example.test 2&amp;gt;&amp;amp;1 &amp;&amp; echo ok`)
	want := `curl -s http://example.test 2>&1 && echo ok`
	if got != want {
		t.Fatalf("cleanShellCommand = %q, want %q", got, want)
	}
}

func TestNormalizeShellCommandTextDecodesRedirectEntities(t *testing.T) {
	got := NormalizeShellCommandText(`curl -sk "https://10.129.13.198/conn.php?cmd=id" 2&gt;/dev/null`)
	want := `curl -sk "https://10.129.13.198/conn.php?cmd=id" 2>/dev/null`
	if got != want {
		t.Fatalf("NormalizeShellCommandText = %q, want %q", got, want)
	}
}

// Regression for the "encoding battle": the model escalated HTML escaping across
// retries (&amp;amp;amp;...) and the old 3-pass cap left deep escaping intact, which
// errored and made it escalate further. Unescaping to a fixpoint must fully collapse
// even this worst real command from the run log back to plain `2>&1`.
func TestCleanShellCommandCollapsesDeeplyEscapedOperators(t *testing.T) {
	deep := `curl -s "http://connected.htb/conn.php?cmd=x" 2&amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;gt;&amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;1`
	want := `curl -s "http://connected.htb/conn.php?cmd=x" 2>&1`
	if got := cleanShellCommand(deep); got != want {
		t.Fatalf("deep unescape = %q, want %q", got, want)
	}
}

func TestDecodeCommandOutputUTF16LE(t *testing.T) {
	raw := []byte{'w', 0, 's', 0, 'l', 0, ':', 0, ' ', 0, 'U', 0, 'n', 0, 'k', 0, 'n', 0, 'o', 0, 'w', 0, 'n', 0}
	got := decodeCommandOutput(raw)
	if got != "wsl: Unknown" {
		t.Fatalf("decodeCommandOutput = %q, want UTF-16LE decoded text", got)
	}
}

func TestProtectedShellMutationBlocksMalwareDirectory(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	cfg.Tools.ProtectedPaths = []string{`C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware`}
	if err := settings.Save(&cfg); err != nil {
		t.Fatal(err)
	}
	err := rejectProtectedShellMutation(`rm -rf "/mnt/c/Users/richa/Documents/MALWARE_TEST_DIR/AI malware"`)
	if err == nil || !strings.Contains(err.Error(), "protected path blocked") {
		t.Fatalf("expected protected path block, got %v", err)
	}
}

func TestProtectedShellMutationAllowsCopyOutOfProtectedDirectory(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	cfg.Tools.ProtectedPaths = []string{`C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware`}
	if err := settings.Save(&cfg); err != nil {
		t.Fatal(err)
	}
	err := rejectProtectedShellMutation(`Copy-Item "C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware\navigator\maps\ad_attack.md" "C:\Users\richa\Desktop\HTB_writeups\"`)
	if err != nil {
		t.Fatalf("copying out of protected directory should be allowed, got %v", err)
	}
	err = rejectProtectedShellMutation(`Copy-Item -Path "C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware\navigator\maps\ad_attack.md" -Destination "C:\Users\richa\Desktop\HTB_writeups\"`)
	if err != nil {
		t.Fatalf("named-arg copy out of protected directory should be allowed, got %v", err)
	}
}

func TestProtectedShellMutationBlocksCopyIntoProtectedDirectory(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	cfg.Tools.ProtectedPaths = []string{`C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware`}
	if err := settings.Save(&cfg); err != nil {
		t.Fatal(err)
	}
	err := rejectProtectedShellMutation(`Copy-Item "C:\Users\richa\Desktop\payload.txt" "C:\Users\richa\Documents\MALWARE_TEST_DIR\AI malware\payload.txt"`)
	if err == nil || !strings.Contains(err.Error(), "protected path blocked") {
		t.Fatalf("expected copy into protected directory to be blocked, got %v", err)
	}
}
