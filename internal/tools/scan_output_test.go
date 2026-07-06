package tools

import (
	"strings"
	"testing"
)

func TestCollapseLineCarriageReturns(t *testing.T) {
	cases := map[string]string{
		"foo":                 "foo",
		"foo\r":               "foo",
		"a\rb":                "b",
		"\rabc":               "abc",
		"p1\rp2\rdone":        "done",
		"admin [Status: 200]": "admin [Status: 200]",
	}
	for in, want := range cases {
		if got := CollapseLineCarriageReturns(in); got != want {
			t.Errorf("CollapseLineCarriageReturns(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsScanProgressLine(t *testing.T) {
	progress := []string{
		":: Progress: [20/4614] :: Job [1/1] :: 0 req/sec :: Duration: [0:00:00] :: Errors: 0 ::",
		"Progress: 1234 / 4614 (26.75%)",
		"  :: Progress: [1/4614]",
	}
	for _, p := range progress {
		if !IsScanProgressLine(p) {
			t.Errorf("expected progress line: %q", p)
		}
	}
	findings := []string{
		"admin                   [Status: 200, Size: 10430, Words: 627, Lines: 119]",
		".bashrc                 [Status: 403, Size: 209]",
		"",
		"some normal output",
	}
	for _, f := range findings {
		if IsScanProgressLine(f) {
			t.Errorf("did not expect progress for: %q", f)
		}
	}
}

func TestCleanCommandOutputKeepsFindingsDropsNoise(t *testing.T) {
	// Models ffuf's real raw stream: \r-overwritten progress with findings
	// interleaved, each logical line terminated by \n.
	raw := "\r:: Progress: [1/4614] :: 0 req/sec ::\radmin [Status: 200, Size: 10430]\n" +
		"\r:: Progress: [20/4614] :: 0 req/sec ::\n" +
		"\r:: Progress: [21/4614] :: 0 req/sec ::\r.bashrc [Status: 403]\n" +
		"\r:: Progress: [22/4614] :: 0 req/sec ::\r.bashrc [Status: 403]\n" +
		"\r:: Progress: [4614/4614] :: 568 req/sec ::\n"
	got := CleanCommandOutput(raw)
	if !strings.Contains(got, "admin [Status: 200, Size: 10430]") {
		t.Fatalf("finding dropped:\n%s", got)
	}
	if strings.Contains(got, ":: Progress:") || strings.Contains(got, "req/sec") {
		t.Fatalf("progress noise survived:\n%s", got)
	}
	// consecutive duplicate .bashrc collapsed to one
	if strings.Count(got, ".bashrc [Status: 403]") != 1 {
		t.Fatalf("expected deduped .bashrc, got:\n%s", got)
	}
}

func TestApplyScanToolHygiene(t *testing.T) {
	cases := map[string]string{
		"ffuf -w list -u http://x/FUZZ":            "ffuf -s -w list -u http://x/FUZZ",
		"ffuf -s -w list -u http://x/FUZZ":         "ffuf -s -w list -u http://x/FUZZ", // idempotent
		"sudo ffuf -u http://x/FUZZ":               "sudo ffuf -s -u http://x/FUZZ",
		"ffuf -u http://x/FUZZ | grep -v 403":      "ffuf -s -u http://x/FUZZ | grep -v 403",
		"nmap -sV 10.10.10.5":                      "nmap -sV 10.10.10.5", // untouched
		"echo ffufnotacommand":                     "echo ffufnotacommand", // word-boundary safe
	}
	for in, want := range cases {
		if got := ApplyScanToolHygiene(in); got != want {
			t.Errorf("ApplyScanToolHygiene(%q)=%q want %q", in, got, want)
		}
	}
}

func TestIsLowSignalArtLine(t *testing.T) {
	art := []string{
		"?????????? ?????                  ????? ????????????????",
		"?????  ????  ? ?  ????????   ????? ????? ????????  ???????",
	}
	for _, a := range art {
		if !isLowSignalArtLine(a) {
			t.Errorf("expected art: %q", a)
		}
	}
	keep := []string{
		"FreePBX 16 SQLi -> Admin -> RCE (CVE-2025-57819)",
		"admin [Status: 200, Size: 10430]",
		"uid=0(root) gid=0(root) groups=0(root)",
		"is this ok? yes",
		"",
	}
	for _, k := range keep {
		if isLowSignalArtLine(k) {
			t.Errorf("did not expect art: %q", k)
		}
	}
}
