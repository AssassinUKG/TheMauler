package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceBundleCollectsFilesAndRendersSummary(t *testing.T) {
	dir := t.TempDir()
	scans := filepath.Join(dir, "scans")
	if err := os.MkdirAll(scans, 0o750); err != nil {
		t.Fatal(err)
	}
	nmap := filepath.Join(scans, "quick.nmap")
	if err := os.WriteFile(nmap, []byte("PORT STATE SERVICE\n80/tcp open http\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scans, "ignore.bin"), []byte{0, 1, 2}, 0o640); err != nil {
		t.Fatal(err)
	}

	files := collectEvidenceFiles([]string{scans}, 10, 200)
	if len(files) != 1 || !strings.HasSuffix(files[0].Path, "quick.nmap") {
		t.Fatalf("unexpected evidence files: %#v", files)
	}
	content := renderEvidenceBundle(evidenceBundleArgs{
		Title:  "Target evidence",
		Target: "10.129.1.2",
		Notes:  "Found HTTP.",
	}, files, ".mauler_artifacts/evidence_bundle/target.md")
	for _, want := range []string{"# Target evidence", "Target: 10.129.1.2", "Found HTTP.", "80/tcp open http"} {
		if !strings.Contains(content, want) {
			t.Fatalf("bundle missing %q:\n%s", want, content)
		}
	}
}

func TestEvidenceSlugFallback(t *testing.T) {
	if got := evidenceSlug("Connected HTB / FreePBX!"); got != "connected-htb-freepbx" {
		t.Fatalf("unexpected slug %q", got)
	}
	if got := evidenceSlug("!!!"); got != "evidence" {
		t.Fatalf("unexpected empty fallback %q", got)
	}
}
