package main

// Boots every server example under `gofastr dev`, the command the site's
// examples page and the READMEs lead with, reads the address the dev
// banner prints, and checks each answers there. The examples used to hardcode their port
// or prefix $PORT with a colon, so the banner said one address and the
// child bound another; isolation.ListenAddr is the fix and this is its
// gate. The process module is a stdio child, not a server, so it is not
// here.

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// devExample names one example directory (relative to examples/), the
// path to probe once the server is up, and the status that proves the
// example, not a placeholder, answered.
type devExample struct {
	dir, path  string
	wantStatus int
}

var devExamples = []devExample{
	{"api-tour", "/posts", http.StatusOK},
	{"backoffice", "/login", http.StatusOK},
	{"blog", "/posts", http.StatusOK},
	{"desktop-notes", "/", http.StatusOK},
	{"desktop-focus", "/", http.StatusOK},
	{"embed-demo", "/", http.StatusNotFound}, // the app serves embed surfaces only; the customer site is the demo
	{"semantic-demo", "/semantic/stats", http.StatusUnauthorized},
	{"spa", "/", http.StatusOK},
	{"static-site", "/", http.StatusOK},
	{"webmcp-remote-assist", "/", http.StatusOK},
	{"rtc-call", "/", http.StatusOK},
	{"meridian", "/", http.StatusOK},
	{"ecommerce/app", "/", http.StatusOK},
	{"site", "/examples", http.StatusOK},
}

func TestE2E_DevLoop_Examples(t *testing.T) {
	if testing.Short() {
		t.Skip("dev-loop e2e: builds and serves every example")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGofastrBinary(t)
	for _, ex := range devExamples {
		t.Run(ex.dir, func(t *testing.T) {
			dir := filepath.Join(repoRoot, "examples", ex.dir)
			port := nextE2EPort(t)
			// One env slice for the prewarm build and the dev child, so
			// both run under the same GOCACHE, GOMODCACHE, and GOFLAGS:
			// whatever this test does to the build environment, the
			// prewarm sees it too, and the child's build is a cache hit.
			env := append(append(os.Environ(), devTempEnv(t)...),
				// The child resolves isolation from its cwd; a linked worktree
				// would silently remap the polled port.
				"GOFASTR_ISOLATION=off",
				// The generated apps refuse to seed their admin on a fresh
				// database without this and exit; CI has no .env to supply it.
				"ADMIN_SEED_PASSWORD=dev-loop-examples-admin-seed-2026", // not-a-secret: test fixture
			)
			prewarmExampleBuild(t, dir, env)
			ctx, cancel := context.WithCancel(context.Background())
			dev := exec.CommandContext(ctx, bin, "dev", "-p", port, "--dir", dir, "--no-a11y")
			dev.Env = env
			var out syncBuffer
			dev.Stdout = &out
			dev.Stderr = &out
			configureTestProcessGroup(dev)
			if err := dev.Start(); err != nil {
				t.Fatalf("start gofastr dev: %v", err)
			}
			// dev.Wait runs here, once, so both the cleanup and the waits
			// below can see the process exit without racing over it.
			devDone := make(chan error, 1)
			go func() { devDone <- dev.Wait() }()
			t.Cleanup(func() {
				_ = killTestProcessTree(dev)
				cancel()
				// Non-blocking: a wait that saw the process exit already
				// drained the only send this goroutine makes.
				select {
				case <-devDone:
				default:
				}
			})
			base := waitForBanner(t, &out, devDone, 30*time.Second)
			if want := "http://localhost:" + port; base != want {
				t.Fatalf("banner advertises %s, the requested address was %s; dev output:\n%s", base, want, clip(out.String()))
			}
			url := base + ex.path
			status := waitForDevServe(t, url, &out, devDone)
			if status != ex.wantStatus {
				t.Fatalf("GET %s under gofastr dev: status %d, want %d; dev output:\n%s", url, status, ex.wantStatus, clip(out.String()))
			}
		})
	}
}

// prewarmExampleBuild compiles the example with the same working dir,
// target package, and environment the dev child's own `go build` uses,
// so the child's build is a cache hit and only link and startup remain
// under the wait. This suite is about the dev loop serving the example,
// not about cold compile speed, and the failure it was written for
// (issues #413 and #456) was a cold compile racing a fixed budget
// under load. A broken example fails here with the compiler's output
// instead of as a connection refused long after.
func prewarmExampleBuild(t *testing.T, dir string, env []string) {
	t.Helper()
	prewarmBin := filepath.Join(t.TempDir(), "prewarm")
	build := exec.Command("go", "build", "-o", prewarmBin, ".")
	build.Dir = dir
	build.Env = env
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("prewarm go build in %s: %v\n%s", dir, err, clip(string(out)))
	}
}

// waitForBanner waits for the dev banner's "Server at http://…" line and
// returns that address, so the probe hits what the user was told to open
// rather than the port the test asked for. The banner prints before any
// build runs, so a clock is fine here; only a dev process that dies
// first ends the wait early.
func waitForBanner(t *testing.T, devOut *syncBuffer, devDone <-chan error, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m := bannerAddr.FindStringSubmatch(devOut.String()); m != nil {
			return m[1]
		}
		select {
		case err := <-devDone:
			t.Fatalf("gofastr dev exited (err: %v) before printing the banner; dev output:\n%s", err, clip(devOut.String()))
		default:
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no \"Server at http://…\" banner within %s; dev output:\n%s", timeout, clip(devOut.String()))
	return ""
}

var bannerAddr = regexp.MustCompile(`Server at (http://[^\s]+)`)

// devWaitStall is how long the dev child may print nothing, with the URL
// still refused, before the wait calls the loop wedged. The dev build
// heartbeat (runBuildWithHeartbeat in dev.go) prints every 10s while
// `go build` runs, so a silent child is not a compiling one.
const devWaitStall = 45 * time.Second

// devWaitCap bounds the whole wait, for a child that keeps printing
// (heartbeats included) without ever answering: failed, not trusted.
const devWaitCap = 5 * time.Minute

// devFatalMarkers are the dev parent's lines that mean the URL will
// never answer. Both leave `gofastr dev` itself alive (the watch loop
// waits for the next save), so without checking for them the wait
// would burn its whole budget on a dead loop.
var devFatalMarkers = []string{"Initial build failed", "Server exited"}

// waitForDevServe polls url until the dev loop answers it. The old fixed
// ninety-second clock measured compile speed: under the pre-push sweep
// the ecommerce example was still compiling when it expired, the
// failure of issues #413 and #456. This wait ends on the child's own
// behaviour instead: the answer, the dev process exiting, a fatal dev
// line, or silence past devWaitStall. devWaitCap is the backstop.
func waitForDevServe(t *testing.T, url string, devOut *syncBuffer, devDone <-chan error) int {
	t.Helper()
	capEnd := time.Now().Add(devWaitCap)
	seen := len(devOut.String())
	fresh := time.Now()
	lastErr := ""
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		lastErr = err.Error()
		out := devOut.String()
		select {
		case err := <-devDone:
			t.Fatalf("gofastr dev exited (err: %v) before answering %s; dev output:\n%s", err, url, clip(out))
		default:
		}
		for _, marker := range devFatalMarkers {
			if strings.Contains(out, marker) {
				t.Fatalf("%s, so %s will never answer; dev output:\n%s", marker, url, clip(out))
			}
		}
		if n := len(out); n != seen {
			seen, fresh = n, time.Now()
		}
		if d := time.Since(fresh); d >= devWaitStall {
			t.Fatalf("no output from gofastr dev for %s while %s stayed refused (last error: %s); dev output:\n%s",
				devWaitStall, url, lastErr, clip(out))
		}
		if time.Now().After(capEnd) {
			t.Fatalf("gofastr dev never answered %s within %s (last error: %s); dev output:\n%s",
				url, devWaitCap, lastErr, clip(out))
		}
		time.Sleep(250 * time.Millisecond)
	}
}
