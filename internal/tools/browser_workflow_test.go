package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func browserFixture(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	var signupPosts atomic.Int32
	var slowPosts atomic.Int32
	slowStarted := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/signup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			signupPosts.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "mauler_session", Value: "fixture", Path: "/", HttpOnly: true})
			http.Redirect(w, r, "/verify", http.StatusSeeOther)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Create account</title><form method="post"><label>Email <input id="email" name="email" type="email" required></label><label>Password <input id="password" name="password" type="password" required></label><button id="submit" type="submit">Continue</button></form>`)
	})
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("mauler_session")
		if err != nil || cookie.Value != "fixture" {
			http.Error(w, "missing session", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Verify email</title><h1>Verification required</h1><p>Use the simulated inbox, then continue.</p><a id="verified" href="/dashboard">I verified my email</a>`)
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("mauler_session")
		if err != nil || cookie.Value != "fixture" {
			http.Error(w, "missing session", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Dashboard</title><h1>Account ready</h1>`)
	})
	mux.HandleFunc("/captcha", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Human check</title><h1>Simulated CAPTCHA</h1>`)
	})
	mux.HandleFunc("/slow-submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			slowPosts.Add(1)
			slowStarted <- struct{}{}
			time.Sleep(750 * time.Millisecond)
			fmt.Fprint(w, `<!doctype html><title>Accepted</title><h1>Accepted once</h1>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Slow form</title><form method="post"><button id="slow" type="submit">Submit once</button></form>`)
	})
	mux.HandleFunc("/files", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>File evidence</title><label>Text <input id="text" name="text"></label><label>Evidence <input id="upload" name="evidence" type="file"></label><button id="noop" type="button">No-op</button><a id="download" href="/download">Download report</a>`)
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="mauler-report.txt"`)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "stable browser evidence\n")
	})
	mux.HandleFunc("/checkpoint", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{Name: "checkpoint_secret", Value: "present", Path: "/", HttpOnly: true})
			fmt.Fprint(w, `<!doctype html><title>Checkpoint source</title><h1>Checkpoint source</h1>`)
			return
		}
		if _, err := r.Cookie("checkpoint_secret"); err == nil {
			fmt.Fprint(w, `<!doctype html><title>Cookie leaked</title><h1>Cookie leaked</h1>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Fresh checkpoint</title><h1>Fresh checkpoint</h1>`)
	})
	return httptest.NewServer(mux), &signupPosts, &slowPosts, slowStarted
}

func requireBrowserRuntime(t *testing.T) {
	t.Helper()
	if status := GetBrowserRuntimeStatus(); !status.Available {
		t.Skip("Chrome or Edge is not installed on this test host")
	}
}

func runBrowserTool(t *testing.T, tool Tool, ctx context.Context, args string) string {
	t.Helper()
	out, err := tool.Run(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s failed: %v", tool.Name(), err)
	}
	return out
}

func snapshotElementRef(t *testing.T, snapshot, name string) string {
	t.Helper()
	marker := "## Interactive elements\n"
	idx := strings.Index(snapshot, marker)
	if idx < 0 {
		t.Fatalf("snapshot has no structured elements: %s", snapshot)
	}
	var elements []BrowserPageElement
	if err := json.Unmarshal([]byte(snapshot[idx+len(marker):]), &elements); err != nil {
		t.Fatalf("decode snapshot elements: %v", err)
	}
	for _, element := range elements {
		if element.Name == name || element.Text == name {
			return element.Ref
		}
	}
	t.Fatalf("snapshot element %q not found: %#v", name, elements)
	return ""
}

func TestBrowserWorkflowPersistsCookiesRedirectsAndManualHandoff(t *testing.T) {
	requireBrowserRuntime(t)
	server, signupPosts, _, _ := browserFixture(t)
	defer server.Close()
	owner := "fixture-account-flow"
	ctx := WithBrowserSessionOwner(context.Background(), owner)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow(owner) })

	runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/signup"))
	// Native browser validation must prevent an empty submit from reaching the server.
	runBrowserTool(t, &BrowserClick{}, ctx, `{"selector":"#submit"}`)
	if got := signupPosts.Load(); got != 0 {
		t.Fatalf("invalid form reached server %d times, want 0", got)
	}
	typed := runBrowserTool(t, &BrowserType{}, ctx, `{"selector":"#email","text":"fixture@example.test"}`)
	if strings.Contains(typed, "fixture@example.test") {
		t.Fatal("browser type output leaked entered text")
	}
	runBrowserTool(t, &BrowserType{}, ctx, `{"selector":"#password","text":"not-a-real-secret"}`)
	runBrowserTool(t, &BrowserClick{}, ctx, `{"selector":"#submit"}`)
	if got := signupPosts.Load(); got != 1 {
		t.Fatalf("valid form reached server %d times, want 1", got)
	}
	snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctx, `{}`)
	if !strings.Contains(snapshot, "Verification required") || !strings.Contains(snapshot, "/verify") {
		t.Fatalf("verification redirect was not observed: %s", snapshot)
	}
	paused, err := PauseBrowserWorkflow(owner, false)
	if err != nil || !paused.Paused || paused.State != "waiting_user" {
		t.Fatalf("pause status = %#v, err=%v", paused, err)
	}
	if _, err := (&BrowserSnapshot{}).Run(ctx, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("automation was not blocked during manual handoff: %v", err)
	}
	if _, err := ResumeBrowserWorkflow(owner); err != nil {
		t.Fatalf("resume: %v", err)
	}
	runBrowserTool(t, &BrowserClick{}, ctx, `{"selector":"#verified"}`)
	snapshot = runBrowserTool(t, &BrowserSnapshot{}, ctx, `{}`)
	if !strings.Contains(snapshot, "Account ready") || !strings.Contains(snapshot, "/dashboard") {
		t.Fatalf("post-verification state was not re-observed: %s", snapshot)
	}
}

func TestBrowserWorkflowIsolationAndCancelledSubmissionDoesNotRetry(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, slowPosts, slowStarted := browserFixture(t)
	defer server.Close()
	ownerA, ownerB := "fixture-owner-a", "fixture-owner-b"
	ctxA := WithBrowserSessionOwner(context.Background(), ownerA)
	ctxB := WithBrowserSessionOwner(context.Background(), ownerB)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow("") })

	runBrowserTool(t, &BrowserOpen{}, ctxA, fmt.Sprintf(`{"url":%q}`, server.URL+"/slow-submit"))
	if _, err := (&BrowserOpen{}).Run(ctxB, json.RawMessage(fmt.Sprintf(`{"url":%q}`, server.URL+"/signup"))); err == nil || !strings.Contains(err.Error(), "owned by another") {
		t.Fatalf("second workflow was not isolated: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctxA)
	clickDone := make(chan error, 1)
	go func() {
		_, err := (&BrowserClick{}).Run(cancelCtx, json.RawMessage(`{"selector":"#slow"}`))
		clickDone <- err
	}()
	select {
	case <-slowStarted:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("slow form submission never reached the fixture server")
	}
	if err := <-clickDone; err == nil {
		t.Fatal("ambiguous cancelled submit unexpectedly reported success")
	}
	time.Sleep(900 * time.Millisecond)
	if got := slowPosts.Load(); got != 1 {
		t.Fatalf("ambiguous submission executed %d times, want exactly 1", got)
	}
	if _, err := CloseBrowserWorkflow(ownerA); err != nil {
		t.Fatalf("close owner A: %v", err)
	}
	runBrowserTool(t, &BrowserOpen{}, ctxB, fmt.Sprintf(`{"url":%q}`, server.URL+"/verify"))
	snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctxB, `{}`)
	if !strings.Contains(snapshot, "missing session") {
		t.Fatalf("cookie leaked between workflow owners: %s", snapshot)
	}
}

func TestCompactBrowserPauseWaitsForSameRunResume(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, _, _ := browserFixture(t)
	defer server.Close()
	owner := "fixture-handoff-wait"
	ctx := WithBrowserSessionOwner(context.Background(), owner)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow(owner) })
	runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/captcha"))

	observed := make(chan BrowserRuntimeStatus, 1)
	ctx = WithBrowserHandoffObserver(ctx, func(status BrowserRuntimeStatus) { observed <- status })
	done := make(chan error, 1)
	go func() {
		_, err := (&Browser{}).Run(ctx, json.RawMessage(`{"action":"pause"}`))
		done <- err
	}()
	select {
	case status := <-observed:
		if !status.Paused || status.State != "waiting_user" {
			t.Fatalf("handoff status = %#v", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handoff observer was not called")
	}
	select {
	case err := <-done:
		t.Fatalf("pause returned before the user resumed it: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := ResumeBrowserWorkflow(owner); err != nil {
		t.Fatalf("resume: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pause completion: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("paused tool did not resume")
	}
}

func TestBrowserWorkflowStableRefsUploadDownloadReliabilityPass5(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, _, _ := browserFixture(t)
	defer server.Close()
	root := t.TempDir()
	for pass := 1; pass <= 5; pass++ {
		owner := fmt.Sprintf("browser-reliability-%d", pass)
		ctx := WithBrowserWorkspaceRoot(WithBrowserSessionOwner(context.Background(), owner), root)
		inputName := fmt.Sprintf("upload-%d.txt", pass)
		if err := os.WriteFile(filepath.Join(root, inputName), []byte(fmt.Sprintf("pass %d\n", pass)), 0o600); err != nil {
			t.Fatal(err)
		}
		runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/files"))
		snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctx, `{}`)
		uploadRef := snapshotElementRef(t, snapshot, "evidence")
		downloadRef := snapshotElementRef(t, snapshot, "Download report")
		upload := runBrowserTool(t, &BrowserUpload{}, ctx, fmt.Sprintf(`{"ref":%q,"path":%q}`, uploadRef, inputName))
		if !strings.Contains(upload, "[browser_upload_evidence]") || !strings.Contains(upload, "sha256:") {
			t.Fatalf("pass %d upload evidence missing: %s", pass, upload)
		}
		download := runBrowserTool(t, &BrowserDownload{}, ctx, fmt.Sprintf(`{"ref":%q,"timeout_secs":10}`, downloadRef))
		if !strings.Contains(download, "[browser_download_evidence]") || !strings.Contains(download, "mauler-report") || !strings.Contains(download, "sha256:") {
			t.Fatalf("pass %d download evidence missing: %s", pass, download)
		}
		if _, err := CloseBrowserWorkflow(owner); err != nil {
			t.Fatalf("pass %d close: %v", pass, err)
		}
	}
}

func TestBrowserRecoveryClassification(t *testing.T) {
	tests := []struct {
		action, message, class string
		retryable, ambiguous   bool
	}{
		{"snapshot", "context deadline exceeded", "timeout", true, false},
		{"click", "context deadline exceeded", "timeout", false, true},
		{"click", `stale browser element reference "e2"`, "stale_element", false, false},
		{"upload", "browser upload is restricted to the active workspace", "scope_policy", false, false},
		{"download", "download timed out after the page action", "timeout", false, true},
	}
	for _, tt := range tests {
		recovery := ClassifyBrowserRecovery(tt.action, fmt.Errorf("%s", tt.message))
		if recovery.Class != tt.class || recovery.Retryable != tt.retryable || recovery.Ambiguous != tt.ambiguous {
			t.Errorf("%s/%s => %#v", tt.action, tt.message, recovery)
		}
	}
}

func TestBrowserWorkflowMultipleTabsRemainOwnerScoped(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, _, _ := browserFixture(t)
	defer server.Close()
	owner := "browser-tabs"
	ctx := WithBrowserSessionOwner(context.Background(), owner)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow(owner) })
	runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/signup"))
	newTab := runBrowserTool(t, &BrowserNewTab{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/captcha"))
	if !strings.Contains(newTab, "tab t2") {
		t.Fatalf("second tab did not receive stable ref: %s", newTab)
	}
	tabs := runBrowserTool(t, &BrowserTabs{}, ctx, `{}`)
	if !strings.Contains(tabs, `"ref": "t1"`) || !strings.Contains(tabs, `"ref": "t2"`) {
		t.Fatalf("tab list missing owner refs: %s", tabs)
	}
	runBrowserTool(t, &BrowserSwitchTab{}, ctx, `{"tab_ref":"t1"}`)
	snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctx, `{}`)
	if !strings.Contains(snapshot, "Create account") {
		t.Fatalf("switch did not restore t1: %s", snapshot)
	}
	runBrowserTool(t, &BrowserCloseTab{}, ctx, `{"tab_ref":"t2"}`)
	if status := GetBrowserWorkflowStatus(owner); status.TabCount != 1 || status.ActiveTab != "t1" {
		t.Fatalf("tab status after close = %#v", status)
	}
}

func TestBrowserCheckpointPersistsMetadataNotSecrets(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, _, _ := browserFixture(t)
	defer server.Close()
	root := t.TempDir()
	owner := "browser-checkpoint"
	ctx := WithBrowserWorkspaceRoot(WithBrowserSessionOwner(context.Background(), owner), root)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow(owner) })
	runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, server.URL+"/checkpoint?token=super-secret#fragment"))
	checkpoint, err := SaveBrowserCheckpoint(ctx, "login-return")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(checkpoint.URL, "super-secret") || strings.Contains(checkpoint.URL, "fragment") || checkpoint.CookiesPersisted || checkpoint.CredentialsPersisted {
		t.Fatalf("checkpoint retained secret state: %#v", checkpoint)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".mauler", "browser-checkpoints", "login-return.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret") || strings.Contains(string(raw), "fragment") || strings.Contains(string(raw), "checkpoint_secret") {
		t.Fatalf("checkpoint file contains secret state: %s", raw)
	}
	if _, err := CloseBrowserWorkflow(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeBrowserCheckpoint(ctx, "login-return"); err != nil {
		t.Fatal(err)
	}
	snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctx, `{}`)
	if !strings.Contains(snapshot, "Fresh checkpoint") || strings.Contains(snapshot, "Cookie leaked") {
		t.Fatalf("checkpoint restored browser secrets: %s", snapshot)
	}
}

func TestBrowserPhaseCancellationIsClassifiedAndBounded(t *testing.T) {
	requireBrowserRuntime(t)
	server, _, _, _ := browserFixture(t)
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "upload.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		tool Tool
		args string
	}{
		{"open", &BrowserOpen{}, fmt.Sprintf(`{"url":%q}`, server.URL+"/files")},
		{"snapshot", &BrowserSnapshot{}, `{}`},
		{"click", &BrowserClick{}, `{"selector":"#noop"}`},
		{"type", &BrowserType{}, `{"selector":"#text","text":"cancelled"}`},
		{"extract", &BrowserExtract{}, `{"selector":"body"}`},
		{"upload", &BrowserUpload{}, `{"selector":"#upload","path":"upload.txt"}`},
		{"download", &BrowserDownload{}, `{"selector":"#download","timeout_secs":2}`},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := fmt.Sprintf("browser-cancel-%d", index)
			base := WithBrowserWorkspaceRoot(WithBrowserSessionOwner(context.Background(), owner), root)
			if tt.name != "open" {
				runBrowserTool(t, &BrowserOpen{}, base, fmt.Sprintf(`{"url":%q}`, server.URL+"/files"))
			}
			cancelled, cancel := context.WithCancel(base)
			cancel()
			started := time.Now()
			_, err := tt.tool.Run(cancelled, json.RawMessage(tt.args))
			if err == nil {
				t.Fatal("cancelled browser phase unexpectedly succeeded")
			}
			if time.Since(started) > 3*time.Second {
				t.Fatalf("cancelled browser phase took too long: %s", time.Since(started))
			}
			if recovery := ClassifyBrowserRecovery(tt.name, err); recovery.Class == "unknown" {
				t.Fatalf("cancelled browser phase was not classified: %v", err)
			}
			_, _ = CloseBrowserWorkflow(owner)
		})
	}
}

func TestBrowserLiveLoginPageSmoke(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("MAULER_BROWSER_SMOKE_URL"))
	if url == "" {
		t.Skip("set MAULER_BROWSER_SMOKE_URL for an explicitly authorised non-destructive smoke")
	}
	requireBrowserRuntime(t)
	owner := "live-login-smoke"
	ctx := WithBrowserSessionOwner(context.Background(), owner)
	t.Cleanup(func() { _, _ = CloseBrowserWorkflow(owner) })
	runBrowserTool(t, &BrowserOpen{}, ctx, fmt.Sprintf(`{"url":%q}`, url))
	snapshot := runBrowserTool(t, &BrowserSnapshot{}, ctx, `{"max_chars":4000}`)
	if !strings.Contains(snapshot, "URL: http") {
		t.Fatalf("live login page did not return an HTTP location: %s", snapshot)
	}
}
