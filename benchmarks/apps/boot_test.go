package apps_test

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

// Every bench app boots and answers GET /healthz: cmd/bench-resources waits
// on it, so an app that dies at Start (a duplicate /healthz) bricks the lane.
func TestBootHealthzReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binaries")
	}
	for _, app := range []string{"minimal", "crud", "full"} {
		t.Run(app, func(t *testing.T) {
			t.Parallel()
			bootAndProbeHealthz(t, app)
		})
	}
}

func bootAndProbeHealthz(t *testing.T, app string) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "app")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./"+app)
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("setup broken: build: %v", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup broken: free port: %v", err)
	}
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()

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
	t.Cleanup(func() { _ = srv.Process.Kill(); <-exited })

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if resp, err := client.Get("http://127.0.0.1:" + port + "/healthz"); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case werr := <-exited:
			exited <- werr
			t.Fatalf("SECURITY: [bench-healthz-boot] %s bench app exited before /healthz answered (wait=%v); stderr tail: %.400s", app, werr, stderr.String())
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("SECURITY: [bench-healthz-boot] %s bench app never answered GET /healthz within 10s; stderr tail: %.400s", app, stderr.String())
		}
	}
}
