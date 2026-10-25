package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestE2E_DevLoop_AppSpecApp(t *testing.T) {
	if testing.Short() {
		t.Skip("dev-loop e2e: compiles and serves a generated app")
	}
	clitest.VariantBuild(t)
	// clitest.TestAppSpecYAML's app, generated and tidied once for the
	// package (testAppSpecTree). This test edits a generated source file,
	// so it works on a copy of its own.
	shared, _ := testAppSpecTree(t)
	dir := clitest.CopyTree(t, shared)

	// Boot the generated app under `gofastr dev`, the path the guidance
	// funnels users to, and the only one that enables livereload.
	bin := clitest.BuildGofastrBinary(t)
	port := clitest.NextE2EPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	dev := exec.CommandContext(ctx, bin, "dev", "-p", port, "--dir", dir)
	dev.Env = append(append(os.Environ(), clitest.DevTempEnv(t)...),
		"PORT=localhost:"+port,
		"DATABASE_URL=file:"+filepath.Join(dir, "devloop.db"),
		// The child app resolves worktree isolation from its cwd; a linked
		// git worktree would silently remap the polled port.
		"GOFASTR_ISOLATION=off",
	)
	var devOut clitest.SyncBuffer
	dev.Stdout = &devOut
	dev.Stderr = &devOut
	clitest.ConfigureTestProcessGroup(dev)
	if err := dev.Start(); err != nil {
		t.Fatalf("start gofastr dev: %v", err)
	}
	t.Cleanup(func() {
		_ = clitest.KillTestProcessTree(dev)
		cancel()
		_ = dev.Wait()
	})

	base := "http://localhost:" + port
	body := waitForBody(t, base+"/", 90*time.Second, &devOut)

	// HMR-ready contract, part 1: the page carries the livereload client and
	// the SSE endpoint answers.
	if !strings.Contains(body, "/__livereload.js") {
		t.Fatalf("generated page does not inject the livereload client:\n%s", clitest.Clip(body))
	}
	resp, err := http.Get(base + "/__livereload.js")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("livereload client script: err=%v status=%v", err, resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// HMR-ready contract, part 2: editing generated source rebuilds and the
	// new content serves without any manual restart.
	const marker = "HMR-LOOP-ALIVE"
	if !strings.Contains(body, "Generated from YAML.") {
		t.Fatalf("fixture heading missing from generated page:\n%s", clitest.Clip(body))
	}
	edited := false
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "Generated from YAML.") {
			continue
		}
		next := strings.Replace(string(src), "Generated from YAML.", "Generated from YAML. "+marker, 1)
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			t.Fatal(err)
		}
		edited = true
		break
	}
	if !edited {
		t.Fatal("no generated .go file contains the fixture heading to edit")
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if strings.Contains(string(b), marker) {
				return // rebuilt and serving the edit, HMR loop alive
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("edited content never served after rebuild window; dev output:\n%s", devOut.String())
}

// waitForBody polls url until it returns 200 and yields the body.
func waitForBody(t *testing.T, url string, timeout time.Duration, devOut *clitest.SyncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			b, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK {
				return string(b)
			}
			last = fmt.Sprintf("status=%d", resp.StatusCode)
		} else {
			last = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("server never became ready (%s); dev output:\n%s", last, devOut.String())
	return ""
}
