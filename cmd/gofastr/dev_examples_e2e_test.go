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
	{"embed-demo", "/", http.StatusNotFound}, // the app serves embed surfaces only; the customer site is the demo
	{"semantic-demo", "/semantic/stats", http.StatusUnauthorized},
	{"spa", "/", http.StatusOK},
	{"static-site", "/", http.StatusOK},
	{"webmcp-remote-assist", "/", http.StatusOK},
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
			ctx, cancel := context.WithCancel(context.Background())
			dev := exec.CommandContext(ctx, bin, "dev", "-p", port, "--dir", dir, "--no-a11y")
			dev.Env = append(os.Environ(),
				// The child resolves isolation from its cwd; a linked worktree
				// would silently remap the polled port.
				"GOFASTR_ISOLATION=off",
				// The generated apps refuse to seed their admin on a fresh
				// database without this and exit; CI has no .env to supply it.
				"ADMIN_SEED_PASSWORD=dev-loop-examples-admin-seed-2026", // not-a-secret: test fixture
			)
			var out syncBuffer
			dev.Stdout = &out
			dev.Stderr = &out
			configureTestProcessGroup(dev)
			if err := dev.Start(); err != nil {
				t.Fatalf("start gofastr dev: %v", err)
			}
			t.Cleanup(func() {
				_ = killTestProcessTree(dev)
				cancel()
				_ = dev.Wait()
				removeDevServerBinary(dev)
			})
			base := waitForBanner(t, &out, 30*time.Second)
			if want := "http://localhost:" + port; base != want {
				t.Fatalf("banner advertises %s, the requested address was %s; dev output:\n%s", base, want, clip(out.String()))
			}
			url := base + ex.path
			status := waitForStatus(t, url, 90*time.Second, &out)
			if status != ex.wantStatus {
				t.Fatalf("GET %s under gofastr dev: status %d, want %d; dev output:\n%s", url, status, ex.wantStatus, clip(out.String()))
			}
		})
	}
}

// waitForBanner waits for the dev banner's "Server at http://…" line and
// returns that address, so the probe hits what the user was told to open
// rather than the port the test asked for.
func waitForBanner(t *testing.T, devOut *syncBuffer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m := bannerAddr.FindStringSubmatch(devOut.String()); m != nil {
			return m[1]
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no \"Server at http://…\" banner within %s; dev output:\n%s", timeout, clip(devOut.String()))
	return ""
}

var bannerAddr = regexp.MustCompile(`Server at (http://[^\s]+)`)

// waitForStatus polls url until any HTTP response arrives and returns its
// status; a connection that never opens fails with the dev output.
func waitForStatus(t *testing.T, url string, timeout time.Duration, devOut *syncBuffer) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		last = err.Error()
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("server never answered %s (%s); dev output:\n%s", url, last, clip(devOut.String()))
	return 0
}
