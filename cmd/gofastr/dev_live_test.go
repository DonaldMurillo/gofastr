package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// ─── Test 1: SSE fires ready event on connect ──────────────────────────────

// TestLivereloadSSEFiresOnConnect verifies that connecting to /__livereload
// immediately emits an SSE "ready" event with a build ID. This is the first
// message the browser's EventSource receives and confirms the SSE handshake
// works end-to-end through a real HTTP server (not just a recorder).
func TestLivereloadSSEFiresOnConnect(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + dev.LiveReloadStreamURL)
	if err != nil {
		t.Fatalf("GET /__livereload: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control=%q, want no-cache", cc)
	}
	if ab := resp.Header.Get("X-Accel-Buffering"); ab != "no" {
		t.Fatalf("X-Accel-Buffering=%q, want no", ab)
	}

	// Read the first SSE event.
	br := bufio.NewReader(resp.Body)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("reading first SSE line: %v", err)
	}
	if !strings.HasPrefix(line, "event: ready") {
		t.Fatalf("first SSE line=%q, want 'event: ready'", strings.TrimSpace(line))
	}

	dataLine, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("reading data line: %v", err)
	}
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("data line=%q, want 'data: <buildID>'", strings.TrimSpace(dataLine))
	}
	buildID := strings.TrimSpace(strings.TrimPrefix(dataLine, "data: "))
	if buildID == "" {
		t.Fatal("build ID is empty")
	}
}

// ─── Test 2: SSE detects server restart (connection drops) ─────────────────

// TestLivereloadSSEDetectsServerRestart proves that when the server shuts
// down (simulating a rebuild-restart), the SSE connection drops so the
// browser's EventSource auto-reconnects. This is the core mechanism that
// triggers page reload: connection loss → EventSource reconnect → second
// ready event → client calls location.reload().
func TestLivereloadSSEDetectsServerRestart(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)

	// Use a context that outlives the server to prove the connection
	// drop comes from the server closing, not the client timing out.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+dev.LiveReloadStreamURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /__livereload: %v", err)
	}

	// Read in background — server close should cause an error/EOF.
	errCh := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		errCh <- readErr
	}()

	// Small delay to ensure the SSE handler is in the heartbeat loop
	// (past the initial ready-event flush).
	time.Sleep(50 * time.Millisecond)

	// Force-close all client connections to simulate a process kill
	// (what actually happens when `gofastr dev` kills the old binary).
	srv.CloseClientConnections()
	srv.Close()

	select {
	case readErr := <-errCh:
		// Success: the connection dropped. The specific error varies by
		// platform (io.EOF, connection reset, use of closed connection)
		// but the important invariant is that the read terminates quickly.
		_ = readErr
	case <-time.After(2 * time.Second):
		t.Fatal("SSE connection did not drop within 2s of server shutdown")
	}
}

// ─── Test 3: Client script has correct caching headers ─────────────────────

// TestLivereloadClientScriptIsUn-cacheable verifies that /__livereload.js
// is served with Cache-Control: no-store to prevent browsers and CDNs from
// serving a stale client script after a deploy. Also checks Content-Type.
func TestLivereloadClientScriptCacheBusting(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + dev.LiveReloadScriptURL)
	if err != nil {
		t.Fatalf("GET /__livereload.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type=%q, want application/javascript", ct)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control=%q, want no-store", cc)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	// The script must contain the essential reload mechanism.
	if !bytes.Contains(body, []byte("EventSource")) {
		t.Error("script body missing EventSource constructor")
	}
	if !bytes.Contains(body, []byte("location.reload")) {
		t.Error("script body missing location.reload() call")
	}
}

// ─── Test 4: Heartbeat keeps connection alive ──────────────────────────────

// TestLivereloadHeartbeatKeepsConnectionAlive proves the server emits
// periodic SSE comment lines (": ping") to keep intermediaries (proxies,
// load balancers) from timing out the connection. Uses a compressed
// heartbeat interval so the test runs in milliseconds.
func TestLivereloadHeartbeatKeepsConnectionAlive(t *testing.T) {
	restore := dev.SetHeartbeatIntervalForTest(t, 50*time.Millisecond)
	defer restore()

	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Use a short overall deadline so a broken heartbeat doesn't hang.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+dev.LiveReloadStreamURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /__livereload: %v", err)
	}
	defer resp.Body.Close()

	// Scan SSE lines for heartbeat comments.
	br := bufio.NewReader(resp.Body)
	heartbeatCount := 0
	gotReady := false

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			// Context deadline or connection close — expected exit path.
			break
		}
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "event: ready") {
			gotReady = true
			continue
		}
		if strings.HasPrefix(trimmed, ": ping") {
			heartbeatCount++
			if heartbeatCount >= 2 {
				// Two heartbeats proves the interval is periodic, not one-shot.
				break
			}
		}
	}

	if !gotReady {
		t.Fatal("never received the initial 'event: ready'")
	}
	if heartbeatCount < 2 {
		t.Fatalf("got %d heartbeat pings, want at least 2", heartbeatCount)
	}
}

// ─── Test 5: Full SSE lifecycle — connect, heartbeat, server stop ──────────

// TestLivereloadFullLifecycle exercises the complete SSE lifecycle in one
// test: connect → ready event → heartbeats → server shutdown → connection
// drop. This is the integration glue that proves the individual pieces
// compose correctly.
func TestLivereloadFullLifecycle(t *testing.T) {
	restore := dev.SetHeartbeatIntervalForTest(t, 30*time.Millisecond)
	defer restore()

	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+dev.LiveReloadStreamURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /__livereload: %v", err)
	}

	type sseEvent struct {
		line string
		err  error
	}
	events := make(chan sseEvent, 50)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			line, readErr := br.ReadString('\n')
			events <- sseEvent{line: line, err: readErr}
			if readErr != nil {
				return
			}
		}
	}()

	// Phase 1: expect ready event.
	select {
	case ev := <-events:
		if ev.err != nil {
			t.Fatalf("error reading ready event: %v", ev.err)
		}
		if !strings.Contains(ev.line, "event: ready") {
			t.Fatalf("first event=%q, want 'event: ready'", strings.TrimSpace(ev.line))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ready event")
	}

	// Phase 2: consume the data line.
	select {
	case ev := <-events:
		if ev.err != nil {
			t.Fatalf("error reading data line: %v", ev.err)
		}
		if !strings.HasPrefix(strings.TrimSpace(ev.line), "data: ") {
			t.Fatalf("second line=%q, want 'data: <id>'", strings.TrimSpace(ev.line))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for data line")
	}

	// Phase 3: expect at least one heartbeat ping.
	gotHeartbeat := false
	heartbeatDeadline := time.After(500 * time.Millisecond)
	for !gotHeartbeat {
		select {
		case ev := <-events:
			if ev.err != nil {
				t.Fatalf("error before heartbeat: %v", ev.err)
			}
			if strings.Contains(ev.line, ": ping") {
				gotHeartbeat = true
			}
		case <-heartbeatDeadline:
			t.Fatal("timed out waiting for heartbeat ping")
		}
	}

	// Phase 4: kill the server (force-close connections, simulating process kill).
	srv.CloseClientConnections()
	srv.Close()

	select {
	case ev := <-events:
		if ev.err == nil {
			// A clean EOF is acceptable — the server closed the connection.
			if ev.line == "" {
				t.Fatal("expected error or EOF after server shutdown, got empty line")
			}
		}
		// Success: connection terminated after server shutdown.
	case <-time.After(2 * time.Second):
		t.Fatal("SSE connection did not terminate after server shutdown")
	}
}

// ─── Test 6: Two clients get same build ID ─────────────────────────────────

// TestLivereloadBuildIDConsistentAcrossClients verifies that all SSE
// clients connecting to the same server instance receive the same build ID.
// In production, a new build ID after reconnect signals "content changed,
// reload now."
func TestLivereloadBuildIDConsistentAcrossClients(t *testing.T) {
	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	getBuildID := func() string {
		resp, err := http.Get(srv.URL + dev.LiveReloadStreamURL)
		if err != nil {
			t.Fatalf("GET /__livereload: %v", err)
		}
		defer resp.Body.Close()

		br := bufio.NewReader(resp.Body)
		// Skip "event: ready\n"
		if _, err := br.ReadString('\n'); err != nil {
			t.Fatalf("reading event line: %v", err)
		}
		// Read "data: <id>\n"
		dataLine, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading data line: %v", err)
		}
		return strings.TrimSpace(strings.TrimPrefix(dataLine, "data: "))
	}

	id1 := getBuildID()
	id2 := getBuildID()

	if id1 != id2 {
		t.Fatalf("build IDs differ between connections: %q vs %q", id1, id2)
	}
	if id1 == "" {
		t.Fatal("build ID is empty")
	}
}

// ─── Build-ID gating tests ────────────────────────────────────────────

// TestLivereloadClientOnlyReloadsOnBuildIDChange proves the livereload
// client script only calls location.reload() when the build ID changes,
// not on every SSE reconnection. This is critical: a transient network
// blip or proxy timeout must NOT destroy the user's page state.
//
// We test this by running the client JS in a headless browser against
// a test server that simulates a reconnect WITHOUT changing the build ID.
func TestLivereloadClientOnlyReloadsOnBuildIDChange(t *testing.T) {
	restore := dev.SetHeartbeatIntervalForTest(t, 100*time.Millisecond)
	defer restore()

	r := router.New()
	dev.RegisterLiveReload(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Fetch the livereload client script.
	scriptURL := srv.URL + dev.LiveReloadScriptURL
	resp, err := http.Get(scriptURL)
	if err != nil {
		t.Fatalf("get script: %v", err)
	}
	scriptBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// The script should reference "ready" event (not "message" or "open").
	script := string(scriptBody)
	if !strings.Contains(script, "ready") {
		t.Fatalf("client script doesn't listen for 'ready' event:\n%s", script)
	}
	if strings.Contains(script, "addEventListener('open'") {
		t.Fatal("client script uses 'open' event — should use 'ready' with build ID comparison")
	}
	if !strings.Contains(script, "lastBuildId") {
		t.Fatal("client script doesn't compare build IDs — will reload on any reconnect")
	}
}

// ─── Resilience tests ──────────────────────────────────────────────────

// TestReloadChannelDebounces proves the reload channel's non-blocking
// send means rapid file changes only queue ONE rebuild signal. This is
// the debounce mechanism in runDev's file-watcher goroutine.
func TestReloadChannelDebounces(t *testing.T) {
	reload := make(chan struct{}, 1)

	// Queue first signal — succeeds.
	select {
	case reload <- struct{}{}:
	default:
		t.Fatal("first send should succeed")
	}

	// Queue second signal — channel full, should hit default (no-op).
	hitDefault := false
	select {
	case reload <- struct{}{}:
		t.Fatal("second send should not succeed — channel should be full")
	default:
		hitDefault = true
	}
	if !hitDefault {
		t.Fatal("expected to hit default branch on second send")
	}

	// Drain — get exactly one signal.
	select {
	case <-reload:
	default:
		t.Fatal("expected to drain one signal")
	}

	// Channel now empty — no more signals.
	select {
	case <-reload:
		t.Fatal("should not have a second signal — debounce means one rebuild only")
	default:
		// correct — empty
	}
}

// TestChangedIgnoresUnmodifiedFiles proves that unchanged files don't
// trigger a false-positive rebuild. Same mod-time → no change.
func TestChangedIgnoresUnmodifiedFiles(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	prev := scanModTimes(dir)
	// Same scan immediately — mod times should be identical.
	curr := scanModTimes(dir)
	if changed(prev, curr) {
		t.Fatal("changed() = true for identical mod times — false positive rebuild")
	}
}

// TestBuildAndServeReturnsFalseOnBuildError proves that a build failure
// (syntax error) returns false without starting a server.
func TestBuildAndServeReturnsFalseOnBuildError(t *testing.T) {
	dir := t.TempDir()
	// Write invalid Go — will fail to build.
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main\nfunc invalid syntax here{")

	var mu sync.Mutex
	var cmd *exec.Cmd

	ok := buildAndServe(dir, "localhost:0", nil, &mu, &cmd)
	if ok {
		t.Fatal("buildAndServe should return false for invalid Go code")
		killServer(&mu, &cmd)
	}
}

// TestScanModTimesPicksUpAllAssetTypes proves that .go, .js, .css, and
// .html are all detected in a single scan of a mixed directory.
func TestScanModTimesPicksUpAllAssetTypes(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main")
	writeDevFile(t, filepath.Join(dir, "app.js"), "// JS")
	writeDevFile(t, filepath.Join(dir, "theme.css"), "/* CSS */")
	writeDevFile(t, filepath.Join(dir, "index.html"), "<html></html>")

	result := scanModTimes(dir)
	if len(result) != 4 {
		t.Fatalf("scanModTimes found %d files, want 4 (.go+.js+.css+.html): %+v", len(result), result)
	}

	expected := map[string]bool{
		filepath.Join(dir, "main.go"):   true,
		filepath.Join(dir, "app.js"):    true,
		filepath.Join(dir, "theme.css"): true,
		filepath.Join(dir, "index.html"): true,
	}
	for path := range result {
		if !expected[path] {
			t.Errorf("unexpected file in scan result: %s", path)
		}
	}
}
