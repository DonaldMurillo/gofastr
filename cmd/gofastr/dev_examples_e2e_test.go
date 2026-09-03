package main

// Boots every server example under `gofastr dev`, the command the site's
// examples page and the READMEs lead with, and checks each answers on the
// address the dev banner prints. The examples used to hardcode their port
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
			url := "http://localhost:" + port + ex.path
			status := waitForStatus(t, url, 90*time.Second, &out)
			if status != ex.wantStatus {
				t.Fatalf("GET %s under gofastr dev: status %d, want %d; dev output:\n%s", url, status, ex.wantStatus, clip(out.String()))
			}
		})
	}
}

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
