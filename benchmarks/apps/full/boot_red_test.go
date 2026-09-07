//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5 (fringe wave; tier T2).
//
// Property: a shipped bench app reaches the serving state. The app's own
// source registers GET /healthz "so the resource runner can wait for
// readiness" (main.go:172-175) — the resource-bench lane is dead if that
// probe can never answer.
//
// Surfaces: benchmarks/apps/full/main.go:172-175 registers GET /healthz
// manually; framework/health.go:184-188 (registerHealthEndpoints) mounts
// GET /healthz again during App.Start (framework/app.go:3219,
// unconditional), so the router sees a duplicate route and panics before
// the listener binds — every boot, exit 2. cmd/bench-resources waitReady
// then never sees an answer and every resource-table row carries
// RuntimeErr (the whole resource-bench lane, broken since the auto-mount
// landed, unwired in CI so nothing noticed). These two apps are the only
// in-tree manual /healthz registrants.
//
// Finding (verified by execution: the built binary exits 2 immediately
// with the route-conflict panic on stderr; the minimal app, which does
// NOT register /healthz itself, boots fine): this shipped app can never
// serve a single request.
//
// Fix direction: drop the manual /healthz registration from the bench
// apps (the framework auto-mount covers the probe). Boot shape mirrors
// examples/meridian/e2e_ui_test.go::e2eBootApp (build → boot on a free
// port → probe readiness); pinned sibling:
// examples/ecommerce/app/seed_guard_test.go::bootSeededStorefront.

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestBootRedHealthzReachable builds the full bench app (every framework
// surface wired at once), boots it on a free port, and requires GET
// /healthz to answer 200 within a bounded deadline. Today the child dies
// at Start (duplicate /healthz route) and the probe never answers; the
// captured stderr tail names the conflict.
func TestBootRedHealthzReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	dir := t.TempDir()
	binName := "app"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	bin := filepath.Join(dir, binName)
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("setup broken: build: %v", err)
	}

	// main.go listens on ":"+PORT, so PORT carries the bare port number
	// and the probe targets 127.0.0.1:<port>.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup broken: free port: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()
	base := "http://127.0.0.1:" + port

	var stderr bytes.Buffer
	srv := exec.Command(bin)
	srv.Dir = dir
	srv.Env = append(os.Environ(), "PORT="+port)
	srv.Stdout = io.Discard
	srv.Stderr = &stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("setup broken: start: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- srv.Wait() }()
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if resp, err := client.Get(base + "/healthz"); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return // serving: the readiness probe answered
			}
		}
		select {
		case werr := <-exited:
			t.Errorf("SECURITY: [bench-healthz-boot] full bench app exited before /healthz answered (wait=%v) — the manual GET /healthz (main.go:172) collides with the framework auto-mount (health.go:186 via app.go:3219) and the router panic kills every boot, bricking the resource-bench lane; stderr tail: %.400s", werr, stderr.String())
			return
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Errorf("SECURITY: [bench-healthz-boot] full bench app never answered GET /healthz within 10s (resource-runner readiness brick); stderr tail: %.400s", stderr.String())
			return
		}
	}
}
