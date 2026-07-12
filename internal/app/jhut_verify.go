package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type JHUTBrowserReport struct {
	Pass              bool     `json:"pass"`
	URL               string   `json:"url"`
	DesktopScreenshot string   `json:"desktop_screenshot"`
	MobileScreenshot  string   `json:"mobile_screenshot"`
	CanvasWidth       int      `json:"canvas_width"`
	CanvasHeight      int      `json:"canvas_height"`
	PixelVariance     float64  `json:"pixel_variance"`
	PixelCoverage     float64  `json:"pixel_coverage"`
	OrbitChanged      bool     `json:"orbit_changed"`
	Responsive        bool     `json:"responsive"`
	ConsoleErrors     []string `json:"console_errors"`
	RuntimeErrors     []string `json:"runtime_errors"`
	Failures          []string `json:"failures"`
	VerifierVersion   string   `json:"verifier_version"`
}

type jhutCanvasMetrics struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Variance float64 `json:"variance"`
	Coverage float64 `json:"coverage"`
}

func (a *App) RunJHUTBrowserVerification(workspace string) JHUTBrowserReport {
	report, _ := verifyJHUTBrowser(context.Background(), workspace, "")
	return report
}

func verifyJHUTBrowser(parent context.Context, workspace, evidenceDir string) (JHUTBrowserReport, error) {
	report := JHUTBrowserReport{VerifierVersion: "jhut-browser-v1"}
	workspace = filepath.Clean(workspace)
	if _, err := os.Stat(filepath.Join(workspace, "jhut.html")); err != nil {
		report.Failures = append(report.Failures, "jhut.html is missing")
		return report, err
	}
	if evidenceDir == "" {
		evidenceDir = filepath.Join(workspace, "mauler_artifacts", "jhut-browser")
	}
	if err := os.MkdirAll(evidenceDir, 0o750); err != nil {
		return report, err
	}
	server := httptest.NewServer(http.FileServer(http.Dir(workspace)))
	defer server.Close()
	report.URL = server.URL + "/jhut.html"

	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("headless", true), chromedp.Flag("disable-gpu", false), chromedp.Flag("hide-scrollbars", true))
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, 60*time.Second)
	defer timeoutCancel()
	var eventMu sync.Mutex
	chromedp.ListenTarget(ctx, func(event any) {
		eventMu.Lock()
		defer eventMu.Unlock()
		switch ev := event.(type) {
		case *runtime.EventExceptionThrown:
			report.RuntimeErrors = append(report.RuntimeErrors, ev.ExceptionDetails.Text)
		case *runtime.EventConsoleAPICalled:
			if ev.Type == runtime.APITypeError || ev.Type == runtime.APITypeWarning {
				parts := make([]string, 0, len(ev.Args))
				for _, arg := range ev.Args {
					parts = append(parts, arg.Description)
				}
				report.ConsoleErrors = append(report.ConsoleErrors, strings.Join(parts, " "))
			}
		}
	})
	var desktop, mobile, canvasShot, afterOrbit []byte
	var metrics jhutCanvasMetrics
	var mobileMetrics jhutCanvasMetrics
	var interactionDispatched bool
	actions := []chromedp.Action{
		chromedp.EmulateViewport(1440, 900), chromedp.Navigate(report.URL), chromedp.WaitVisible("canvas", chromedp.ByQuery), chromedp.Sleep(3 * time.Second),
		chromedp.Evaluate(jhutMetricsJS, &metrics), chromedp.Screenshot("canvas", &canvasShot, chromedp.ByQuery), chromedp.FullScreenshot(&desktop, 90),
		chromedp.ActionFunc(func(ctx context.Context) error { return input.DispatchMouseEvent(input.MouseMoved, 720, 450).Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MousePressed, 720, 450).WithButton(input.Left).WithClickCount(1).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, 860, 500).WithButton(input.Left).WithButtons(1).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseReleased, 860, 500).WithButton(input.Left).Do(ctx)
		}),
		chromedp.Evaluate(`(() => { const c=document.querySelector('canvas'); if(!c)return false; const opts={bubbles:true,clientX:860,clientY:500,buttons:1,pointerId:1,pointerType:'mouse'}; c.dispatchEvent(new PointerEvent('pointermove',opts)); c.dispatchEvent(new MouseEvent('mousemove',opts)); return true; })()`, &interactionDispatched),
		chromedp.Sleep(time.Second), chromedp.Screenshot("canvas", &afterOrbit, chromedp.ByQuery),
		chromedp.EmulateViewport(390, 844), chromedp.Sleep(time.Second), chromedp.Evaluate(jhutMetricsJS, &mobileMetrics), chromedp.FullScreenshot(&mobile, 90),
	}
	if err := chromedp.Run(ctx, actions...); err != nil {
		report.Failures = append(report.Failures, "browser run: "+err.Error())
	}
	variance, coverage := pngVisualMetrics(canvasShot)
	report.CanvasWidth, report.CanvasHeight, report.PixelVariance, report.PixelCoverage = metrics.Width, metrics.Height, variance, coverage
	report.OrbitChanged = len(canvasShot) > 0 && byteHash(canvasShot) != byteHash(afterOrbit)
	report.Responsive = len(mobile) > 1000 && metrics.Width >= 300 && metrics.Height >= 200 && mobileMetrics.Width >= 300 && mobileMetrics.Width <= 420 && mobileMetrics.Height >= 200
	report.DesktopScreenshot = filepath.Join(evidenceDir, "desktop.png")
	report.MobileScreenshot = filepath.Join(evidenceDir, "mobile.png")
	if len(desktop) > 0 {
		_ = os.WriteFile(report.DesktopScreenshot, desktop, 0o640)
	}
	if len(mobile) > 0 {
		_ = os.WriteFile(report.MobileScreenshot, mobile, 0o640)
	}
	if report.PixelVariance < 8 {
		report.Failures = append(report.Failures, fmt.Sprintf("canvas variance %.2f is too low", report.PixelVariance))
	}
	if report.PixelCoverage < .03 {
		report.Failures = append(report.Failures, fmt.Sprintf("canvas coverage %.3f is too low", report.PixelCoverage))
	}
	if !report.OrbitChanged {
		report.Failures = append(report.Failures, "orbit drag did not change the rendered page")
	}
	if !interactionDispatched {
		report.Failures = append(report.Failures, "orbit interaction could not be dispatched")
	}
	if !report.Responsive {
		report.Failures = append(report.Failures, "mobile resize/screenshot failed")
	}
	if len(report.RuntimeErrors) > 0 {
		report.Failures = append(report.Failures, "page emitted runtime errors")
	}
	report.Pass = len(report.Failures) == 0
	return report, nil
}

func byteHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func pngVisualMetrics(data []byte) (float64, float64) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	bounds := img.Bounds()
	stepX, stepY := max(1, bounds.Dx()/256), max(1, bounds.Dy()/256)
	var n, covered float64
	var sum, sum2 float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			v := float64(r+g+b) / (3 * 257)
			sum += v
			sum2 += v * v
			n++
			if v > 8 {
				covered++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	mean := sum / n
	return math.Sqrt(math.Max(0, sum2/n-mean*mean)), covered / n
}

const jhutMetricsJS = `(() => { const c=document.querySelector('canvas'); return c ? {width:c.clientWidth,height:c.clientHeight,variance:0,coverage:0} : {width:0,height:0,variance:0,coverage:0}; })()`
