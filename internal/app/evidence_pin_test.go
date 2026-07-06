package app

import (
	"strings"
	"testing"
)

func TestExtractEvidencePinsFindsCompactOpsFacts(t *testing.T) {
	text := `
Target: 10.129.23.158
Webshell: https://connected.htb/abc/shell.php?cmd=id
uid=999(asterisk) gid=1000(asterisk) groups=1000(asterisk)
CVE-2025-57819
password = s3cr3tish
`
	pins := extractEvidencePins(text, 10)
	joined := strings.Join(pins, "\n")
	for _, want := range []string{"Target: 10.129.23.158", "https://connected.htb/abc/shell.php?cmd=id", "uid=999(asterisk)", "CVE-2025-57819", "password = s3cr3tish"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing evidence pin %q from %#v", want, pins)
		}
	}
}

func TestExtractEvidencePinsDedupesAndCaps(t *testing.T) {
	text := strings.Repeat("CVE-2025-57819\n", 10) + "CVE-2025-61678\n"
	pins := extractEvidencePins(text, 1)
	if len(pins) != 1 || pins[0] != "CVE-2025-57819" {
		t.Fatalf("expected one capped deduped pin, got %#v", pins)
	}
}
