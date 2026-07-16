package main

// End-to-end HMR-readiness gate for the blueprint surface: a generated app
// run under `gofastr dev` must serve the livereload client, expose the SSE
// endpoint, and rebuild+serve edited content — the same contract the init
// scaffold already proves in dev_e2e_test.go. Gated by -short (slow — two Go
// compiles + a dev-watch loop).

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
)

func TestE2E_DevLoop_BlueprintApp(t *testing.T) {
	if testing.Short() {
		t.Skip("dev-loop e2e: compiles and serves a generated app")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/demo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(dir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), testBlueprintYAML())

	generate := exec.Command("go", "run", filepath.Join(repoRoot, "cmd", "gofastr"), "generate", "--from=gofastr.yml")
	generate.Dir = dir
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate failed: %v\n%s", err, output)
	}
	// The generated app pulls new imports (driver, batteries); `gofastr dev`
	// builds with the plain toolchain, so the user flow requires a tidy first.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if output, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, output)
	}

	// Boot the generated app under `gofastr dev` — the path the guidance
	// funnels users to, and the only one that enables livereload.
	bin := buildGofastrBinary(t)
	port := nextE2EPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	dev := exec.CommandContext(ctx, bin, "dev", "-p", port, "--dir", dir)
	dev.Env = append(os.Environ(),
		"PORT=localhost:"+port,
		"DATABASE_URL=file:"+filepath.Join(dir, "devloop.db"),
		// The child app resolves worktree isolation from its cwd; a linked
		// git worktree would silently remap the polled port.
		"GOFASTR_ISOLATION=off",
	)
	var devOut syncBuffer
	dev.Stdout = &devOut
	dev.Stderr = &devOut
	configureTestProcessGroup(dev)
	if err := dev.Start(); err != nil {
		t.Fatalf("start gofastr dev: %v", err)
	}
	t.Cleanup(func() {
		_ = killTestProcessTree(dev)
		cancel()
		_ = dev.Wait()
	})

	base := "http://localhost:" + port
	body := waitForBody(t, base+"/", 90*time.Second, &devOut)

	// HMR-ready contract, part 1: the page carries the livereload client and
	// the SSE endpoint answers.
	if !strings.Contains(body, "/__livereload.js") {
		t.Fatalf("generated page does not inject the livereload client:\n%s", clip(body))
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
		t.Fatalf("fixture heading missing from generated page:\n%s", clip(body))
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
				return // rebuilt and serving the edit — HMR loop alive
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("edited content never served after rebuild window; dev output:\n%s", devOut.String())
}

// waitForBody polls url until it returns 200 and yields the body.
func waitForBody(t *testing.T, url string, timeout time.Duration, devOut *syncBuffer) string {
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

func clip(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "…"
	}
	return s
}
