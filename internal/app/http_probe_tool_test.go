package app

import (
	"strings"
	"testing"
)

func TestHTTPProbeHelpersBuildBoundedCommand(t *testing.T) {
	paths := normaliseProbePaths([]string{"", "admin/", "/admin/", "/robots.txt", "/extra"}, 3)
	if strings.Join(paths, ",") != "/admin/,/robots.txt,/extra" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	cmd := buildHTTPProbeCommand("http://connected.htb/base", paths, []string{"X-Test: yes"}, 7, ".mauler_artifacts/http_probe/out.txt")
	for _, want := range []string{
		"curl -skS -i -L",
		"--max-time 7",
		"X-Test: yes",
		"http://connected.htb/admin/",
		"__MAULER_HTTP_PROBE_ARTIFACT__=.mauler_artifacts/http_probe/out.txt",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
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

	summary := summarizeHTTPProbeOutput(raw, ".mauler_artifacts/http_probe/out.txt")
	for _, want := range []string{
		"/: HTTP/1.1 301 Moved Permanently",
		"location=http://connected.htb/admin/",
		"/admin/: HTTP/1.1 403 Forbidden",
		"Artifact: .mauler_artifacts/http_probe/out.txt",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}
