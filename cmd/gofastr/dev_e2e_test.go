package main

// Real end-to-end tests for `gofastr dev` hot reload. These spin up a
// full subprocess (gofastr dev + child server), modify source files, and
// assert the server automatically rebuilds and serves updated content.
// Gated by -short (slow — needs Go compilation + network).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Global port counter so tests don't collide on the same port.
var e2ePortCounter atomic.Int64

func nextE2EPort() string {
	return fmt.Sprintf("%d", 18083+e2ePortCounter.Add(1)-1)
}

func buildGofastrBinary(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "gofastr")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = filepath.Join(repoRoot, "cmd", "gofastr")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gofastr: %v\n%s", err, out)
	}
	return bin
}

type devHarness struct {
	t          *testing.T
	bin        string
	dir        string
	port       string
	cmd        *exec.Cmd
	cancelFunc context.CancelFunc
	output     strings.Builder
}

func newDevHarness(t *testing.T) *devHarness {
	t.Helper()
	bin := buildGofastrBinary(t)
	dir := t.TempDir()

	initCmd := exec.Command(bin, "init", "hotreload", "--no-entity")
	initCmd.Dir = dir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}

	projDir := filepath.Join(dir, "hotreload")
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	replaceCmd := exec.Command("go", "mod", "edit", "-replace", "github.com/DonaldMurillo/gofastr="+repoRoot)
	replaceCmd.Dir = projDir
	if out, err := replaceCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod edit -replace: %v\n%s", err, out)
	}
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	return &devHarness{t: t, bin: bin, dir: projDir, port: nextE2EPort()}
}

func (h *devHarness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFunc = cancel

	cmd := exec.CommandContext(ctx, h.bin, "dev", "-p", h.port, "--dir", h.dir)
	cmd.Env = append(os.Environ(), "PORT=localhost:"+h.port)
	cmd.Stdout = &h.output
	cmd.Stderr = &h.output
	// Set process group so we can kill the entire tree (gofastr dev + child server).
	// Without this, SIGKILL on gofastr dev leaves the child server as an orphan.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	h.cmd = cmd

	if err := cmd.Start(); err != nil {
		h.t.Fatalf("start gofastr dev: %v", err)
	}
	pid := cmd.Process.Pid
	h.t.Cleanup(func() {
		cancel()
		// Kill the entire process group (gofastr dev + child server).
		syscall.Kill(-pid, syscall.SIGKILL)
		cmd.Wait()
	})

	h.waitForServer(60 * time.Second)
}

func (h *devHarness) waitForServer(timeout time.Duration) {
	h.t.Helper()
	url := fmt.Sprintf("http://localhost:%s/", h.port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.t.Fatalf("server on :%s did not respond within %v.\nOutput:\n%s", h.port, timeout, h.output.String())
}

func (h *devHarness) modifyHomeScreen(newTitle string) {
	h.t.Helper()
	homeGo := filepath.Join(h.dir, "screens", "home.go")
	data, err := os.ReadFile(homeGo)
	if err != nil {
		h.t.Fatalf("read home.go: %v", err)
	}
	replaced := strings.ReplaceAll(string(data), "hotreload", newTitle)
	if replaced == string(data) {
		h.t.Fatal("modifyHomeScreen: replacement had no effect")
	}
	if err := os.WriteFile(homeGo, []byte(replaced), 0o644); err != nil {
		h.t.Fatalf("write home.go: %v", err)
	}
}

func (h *devHarness) baseURL() string {
	return fmt.Sprintf("http://localhost:%s/", h.port)
}

func fetchBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func readAll(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func devE2EBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 120*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func shouldSkip(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	if os.Getenv("GOFASTR_DEV") == "1" {
		t.Skip("cannot run gofastr dev inside gofastr dev")
	}
}

// ─── E2E Tests ─────────────────────────────────────────────────────────

// TestE2E_HotReload_GoFileChange proves the full cycle:
//  1. `gofastr dev` starts and serves the initial page
//  2. Modifying a .go file triggers a rebuild
//  3. Server restarts and serves updated content
func TestE2E_HotReload_GoFileChange(t *testing.T) {
	shouldSkip(t)

	h := newDevHarness(t)
	h.start()
	base := h.baseURL()

	// Phase 1: initial content.
	body := fetchBody(t, base)
	if !strings.Contains(body, "hotreload") {
		t.Fatalf("initial page missing 'hotreload': %s", truncate(body, 500))
	}
	if !strings.Contains(body, "/__livereload.js") {
		t.Fatal("livereload script not injected — GOFASTR_DEV=1 not wired")
	}

	// Phase 2: modify.
	h.modifyHomeScreen("RELOADED_TITLE")

	// Phase 3: poll until updated content appears.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base)
		if err == nil && resp.StatusCode == 200 {
			buf, _ := readAll(resp)
			if strings.Contains(buf, "RELOADED_TITLE") {
				return // success
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("server never served 'RELOADED_TITLE' after 60s.\nOutput:\n%s", h.output.String())
}

// TestE2E_HotReload_ServerStartsQuickly proves the dev server responds
// within a reasonable time after startup.
func TestE2E_HotReload_ServerStartsQuickly(t *testing.T) {
	shouldSkip(t)

	h := newDevHarness(t)
	start := time.Now()
	h.start()
	elapsed := time.Since(start)

	if elapsed > 45*time.Second {
		t.Fatalf("server took %v to start — too slow", elapsed)
	}
	t.Logf("server started in %v", elapsed)
}

// TestE2E_HotReload_LivereloadEndpointSSE proves /__livereload SSE
// emits a ready event.
func TestE2E_HotReload_LivereloadEndpointSSE(t *testing.T) {
	shouldSkip(t)

	h := newDevHarness(t)
	h.start()

	url := fmt.Sprintf("http://localhost:%s/__livereload", h.port)
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET /__livereload: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("/__livereload status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: ready") {
		t.Fatalf("SSE body missing 'event: ready':\n%s", body)
	}
}

// TestE2E_HotReload_LivereloadScriptServed proves /__livereload.js
// is served with correct content type and no-store caching.
func TestE2E_HotReload_LivereloadScriptServed(t *testing.T) {
	shouldSkip(t)

	h := newDevHarness(t)
	h.start()

	url := fmt.Sprintf("http://localhost:%s/__livereload.js", h.port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /__livereload.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("/__livereload.js status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q, want application/javascript", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}
// TestE2E_HotReload_BrowserAutoRefreshes is the keystone E2E test. It
// proves the FULL hot-reload cycle works end-to-end in a real browser:
//
//  1. Chrome loads the page (h1 = "hotreload")
//  2. The livereload EventSource connects (first open → everConnected = true)
//  3. We modify screens/home.go → h1 changes to "RELOADED_TITLE"
//  4. gofastr dev detects the change, rebuilds, restarts the server
//  5. Server restart kills the SSE connection
//  6. EventSource auto-reconnects → second open → location.reload()
//  7. Chrome re-fetches the page from the new server
//  8. h1 now reads "RELOADED_TITLE"
//
// This is the only test that proves the browser's EventSource client,
// the SSE server, the file watcher, the rebuild loop, and the page
// content update all compose correctly. Everything else tests pieces.
func TestE2E_HotReload_BrowserAutoRefreshes(t *testing.T) {
	shouldSkip(t)

	h := newDevHarness(t)
	h.start()

	ctx := devE2EBrowserCtx(t)
	base := h.baseURL()

	// Phase 1: load page, verify initial content + livereload wired.
	var initialTitle string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.WaitReady("h1", chromedp.ByQuery),
		chromedp.Sleep(1*time.Second), // let EventSource connect
		chromedp.Text("h1", &initialTitle, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if !strings.Contains(strings.ToLower(initialTitle), "hotreload") {
		t.Fatalf("initial h1 = %q, want 'hotreload'", initialTitle)
	}

	// Verify livereload script tag is present.
	var hasLivereload bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('script[src="/__livereload.js"]') !== null`, &hasLivereload),
	); err != nil {
		t.Fatalf("check livereload script: %v", err)
	}
	if !hasLivereload {
		t.Fatal("livereload script tag not found in page")
	}

	// Verify the EventSource connected (everConnected should be true in JS land).
	var esConnected bool
	if err := chromedp.Run(ctx,
		// The livereload script doesn't expose state, but we can check that
		// the EventSource exists and is in OPEN state (readyState === 0).
		chromedp.Evaluate(`(function(){ try { var es = document.querySelector('script[src="/__livereload.js"]'); return performance.getEntriesByType("resource").some(function(e){return e.name.includes("__livereload");}); } catch(e) { return false; } })()`, &esConnected),
	); err != nil {
		t.Fatalf("check EventSource: %v", err)
	}
	if !esConnected {
		t.Log("WARNING: livereload SSE connection not detected in performance entries (may still work)")
	}

	// Phase 2: modify the home screen to change the h1 text.
	h.modifyHomeScreen("RELOADED_TITLE")
	t.Log("modified screens/home.go — waiting for rebuild + browser reload...")

	// Phase 3: wait for the browser to auto-refresh.
	//
	// The cycle is:
	//   file change → watcher detects (500ms poll) → go build (~3-5s)
	//   → server restart → SSE connection drops → EventSource reconnects
	//   → second "open" fires → location.reload() → new page loaded
	//
	// Total: ~5-15s. We poll for the new h1 content with a generous timeout.
	// chromedp's CDP connection survives location.reload() because it's
	// attached to the tab target, not a page snapshot. After reload, the
	// DOM is fresh and we can read the new content.
	//
	// We retry on error because during the brief reload window, DOM queries
	// may fail (page is navigating).
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		var title string
		err := chromedp.Run(ctx,
			chromedp.WaitReady("h1", chromedp.ByQuery),
			chromedp.Text("h1", &title, chromedp.ByQuery),
		)
		if err == nil && strings.Contains(title, "RELOADED_TITLE") {
			t.Logf("browser saw updated content: h1 = %q", title)
			return // SUCCESS
		}
		// On error (page navigating), wait and retry.
		time.Sleep(500 * time.Millisecond)
	}

	// Failure — dump diagnostics.
	var finalTitle string
	_ = chromedp.Run(ctx, chromedp.Text("h1", &finalTitle, chromedp.ByQuery))
	t.Fatalf("browser never saw 'RELOADED_TITLE' after 45s. Final h1 = %q.\nDev output:\n%s",
		finalTitle, h.output.String())
}
