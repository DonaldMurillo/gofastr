package main

// HMR-readiness sweep: every runnable example, booted the way `gofastr dev`
// boots it (GOFASTR_DEV=1), must expose the livereload endpoint, and every
// example that serves HTML must inject the livereload client into its pages —
// otherwise the developer editing that surface gets no browser refresh and
// silently loses the framework's hot-reload loop.
//
// Gated by -short (compiles and boots each example).

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type exampleSurface struct {
	name string // directory under examples/
	pkg  string // package to build, relative to repo root
	// fixedAddr is set for examples that ignore PORT and bind a constant.
	fixedAddr string
	// page is the HTML page asserted to carry the livereload client;
	// empty for API-only surfaces (endpoint check only).
	page string
	env  []string
}

func TestExamplesAreHMRReady(t *testing.T) {
	if testing.Short() {
		t.Skip("boots every example")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	surfaces := []exampleSurface{
		{name: "site", pkg: "./examples/site", page: "/"},
		{name: "meridian", pkg: "./examples/meridian", page: "/"},
		{name: "backoffice", pkg: "./examples/backoffice", page: "/"},
		{name: "spa", pkg: "./examples/spa", fixedAddr: "127.0.0.1:3090", page: "/"},
		{name: "static-site", pkg: "./examples/static-site", fixedAddr: "127.0.0.1:3070", page: "/"},
		{name: "ecommerce", pkg: "./examples/ecommerce/app", page: "/"},
		// API-only surfaces: no HTML pages, so only the SSE endpoint applies.
		{name: "blog", pkg: "./examples/blog", page: ""},
		{name: "api-tour", pkg: "./examples/api-tour", page: ""},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			assertHMRReady(t, repoRoot, s)
		})
	}
}

func assertHMRReady(t *testing.T, repoRoot string, s exampleSurface) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), s.name)
	build := exec.Command("go", "build", "-o", bin, s.pkg)
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", s.pkg, err, out)
	}

	addr := s.fixedAddr
	if addr == "" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr = ln.Addr().String()
		_ = ln.Close()
	} else if ln, err := net.Listen("tcp", addr); err != nil {
		t.Skipf("fixed address %s busy on this machine: %v", addr, err)
	} else {
		_ = ln.Close()
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = filepath.Join(repoRoot, strings.TrimPrefix(s.pkg, "./"))
	cmd.Env = append(os.Environ(),
		"GOFASTR_DEV=1", // what `gofastr dev` sets on the child
		// The test owns exact loopback ports and temp databases; linked-worktree
		// isolation would remap both and make the readiness probe watch the wrong port.
		"GOFASTR_ISOLATION=off",
		"PORT="+port,
		"DATABASE_URL=file:"+filepath.Join(t.TempDir(), s.name+".db"),
	)
	cmd.Env = append(cmd.Env, s.env...)
	var out syncBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", s.name, err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})

	base := "http://" + net.JoinHostPort("127.0.0.1", port)
	if !waitFor200(base+"/healthz", 60*time.Second) && !waitFor200(base+"/", 15*time.Second) {
		t.Fatalf("%s never became ready on %s; output:\n%s", s.name, base, out.String())
	}

	// Contract 1 (every framework app): the livereload endpoint is wired.
	resp, err := http.Get(base + "/__livereload.js")
	if err != nil {
		t.Fatalf("%s livereload client script: %v", s.name, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("%s: /__livereload.js = %d under GOFASTR_DEV=1 — the dev SSE surface is not wired", s.name, resp.StatusCode)
	}

	// Contract 2 (HTML-serving apps): pages inject the livereload client so
	// the browser actually refreshes after `gofastr dev` rebuilds.
	if s.page == "" {
		return
	}
	pageResp, err := http.Get(base + s.page)
	if err != nil {
		t.Fatalf("%s GET %s: %v", s.name, s.page, err)
	}
	body, _ := io.ReadAll(pageResp.Body)
	_ = pageResp.Body.Close()
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("%s GET %s = %d", s.name, s.page, pageResp.StatusCode)
	}
	if !strings.Contains(string(body), "/__livereload.js") {
		t.Errorf("%s: %s does not inject the livereload client under GOFASTR_DEV=1 — edits rebuild but the browser never refreshes", s.name, s.page)
	}
}

func waitFor200(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// syncBuffer guards live child-process output against the -race data race
// between os/exec's copy goroutine and failure-path reads.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
