package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mauler/internal/tools"
)

func TestJHUTBrowserVerifierChecksPixelsOrbitAndResponsive(t *testing.T) {
	if !tools.GetBrowserRuntimeStatus().Available {
		t.Skip("Chrome/Edge unavailable")
	}
	root := t.TempDir()
	html := `<!doctype html><title>JHUT - Japanese House</title><style>html,body{margin:0;width:100%;height:100%}canvas{width:100%;height:100%;display:block}</style><canvas></canvas><script>
const c=document.querySelector('canvas'),x=c.getContext('2d'); function draw(){c.width=innerWidth;c.height=innerHeight;const g=x.createLinearGradient(0,0,c.width,c.height);g.addColorStop(0,'#123');g.addColorStop(.5,'#edc');g.addColorStop(1,'#58a');x.fillStyle=g;x.fillRect(0,0,c.width,c.height)} draw(); const moved=()=>c.style.filter='invert(1)'; addEventListener('pointermove',moved); addEventListener('mousemove',moved); addEventListener('resize',draw);
</script>`
	if err := os.WriteFile(filepath.Join(root, "jhut.html"), []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := verifyJHUTBrowser(context.Background(), root, filepath.Join(root, "evidence"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Pass {
		t.Fatalf("browser verifier failed: %#v", report)
	}
	if report.PixelVariance <= 8 || report.PixelCoverage <= .03 || !report.OrbitChanged || !report.Responsive {
		t.Fatalf("missing runtime evidence: %#v", report)
	}
	for _, path := range []string{report.DesktopScreenshot, report.MobileScreenshot} {
		if info, err := os.Stat(path); err != nil || info.Size() < 1000 {
			t.Fatalf("missing screenshot %s: %v", path, err)
		}
	}
}
