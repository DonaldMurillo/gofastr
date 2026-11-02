package devcmd

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func newTestHold(t *testing.T) *devHold {
	t.Helper()
	h, err := newDevHold("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.release)
	return h
}

func holdGet(t *testing.T, addr, accept string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest("GET", "http://"+addr+"/some/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestDevHoldAnswersWhileBuilding(t *testing.T) {
	h := newTestHold(t)
	res, body := holdGet(t, h.addr, "")
	if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q: want 503 with Retry-After", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") || !strings.Contains(body, "building") {
		t.Fatalf("curl should read a plain-text building notice, got %q: %q", res.Header.Get("Content-Type"), body)
	}
	res, body = holdGet(t, h.addr, "text/html,*/*")
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") || !strings.Contains(body, "<title>") {
		t.Fatalf("a browser should get an HTML page, got %q", body)
	}
}

func TestDevHoldShowsBuildError(t *testing.T) {
	h := newTestHold(t)
	h.show(holdBuildFailed, "./main.go:3:2: undefined: <nope>\n")
	_, text := holdGet(t, h.addr, "")
	if !strings.Contains(text, "./main.go:3:2: undefined: <nope>") {
		t.Fatalf("plain text missing the compiler error: %q", text)
	}
	_, page := holdGet(t, h.addr, "text/html")
	if !strings.Contains(page, "undefined: &lt;nope&gt;") || strings.Contains(page, "<nope>") {
		t.Fatalf("HTML page must show the error escaped: %q", page)
	}
}

func TestDevHoldReleaseFreesThePort(t *testing.T) {
	h := newTestHold(t)
	h.release()
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		t.Fatalf("released address still bound: %v", err)
	}
	_ = ln.Close()
	h.show(holdExited, "exit status 2")
	_, body := holdGet(t, h.addr, "")
	if !strings.Contains(body, "exit status 2") {
		t.Fatalf("hold did not take the address back: %q", body)
	}
}

// A relisten still retrying when release runs must not take the port
// back after it: the app release made way for would then fail to bind.
func TestDevHoldReleaseWaitsForRelisten(t *testing.T) {
	h := newTestHold(t)
	h.release()
	blocker, err := net.Listen("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	showDone := make(chan struct{})
	go func() { h.show(holdExited, "exit status 1"); close(showDone) }()
	// Wait until relisten holds its lock, retrying on the blocker.
	for deadline := time.Now().Add(5 * time.Second); h.listenMu.TryLock(); {
		h.listenMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("show never started relistening")
		}
		time.Sleep(5 * time.Millisecond)
	}
	released := make(chan struct{})
	go func() { h.release(); close(released) }()
	time.Sleep(150 * time.Millisecond)
	_ = blocker.Close()
	<-showDone
	<-released
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		t.Fatalf("the hold took the port back after release: %v", err)
	}
	_ = ln.Close()
}

func TestDevHoldRefusesABusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := newDevHold(ln.Addr().String()); !addrInUse(err) {
		t.Fatalf("newDevHold on a busy port = %v, want EADDRINUSE", err)
	}
}

// An open app tab's livereload client reconnects to the hold. It must
// not reload into a "building" page on every save, only when there is
// an error to show; the release then drops it so it reconnects to the app.
func TestDevHoldLiveReloadOnlyOnFailure(t *testing.T) {
	h := newTestHold(t)
	res, err := http.Get("http://" + h.addr + "/__livereload")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	events := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				events <- strings.TrimPrefix(sc.Text(), "data: ")
			}
		}
		// The release ends the stream; how it ends is not under test.
		_ = sc.Err()
		close(events)
	}()
	select {
	case id := <-events:
		t.Fatalf("building sent a ready event %q; open tabs would reload on every save", id)
	case <-time.After(300 * time.Millisecond):
	}
	h.show(holdBuildFailed, "boom")
	select {
	case id, ok := <-events:
		if !ok || id == "" {
			t.Fatal("stream closed without a ready event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no ready event after the build failed")
	}
	h.release()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected second event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("release left the livereload stream open")
	}
}

func TestBuildAndServeHoldShowsCompileError(t *testing.T) {
	dir := t.TempDir()
	writeDevFile(t, filepath.Join(dir, "go.mod"), "module holdtest\n\ngo 1.21\n")
	writeDevFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { undefinedThing() }\n")
	h := newTestHold(t)
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	clitest.CovT_capStdout(t, func() { ok = buildAndServe(dir, ".", h.addr, nil, &mu, &cmd, true, h, nil) })
	if ok {
		KillServer(&mu, &cmd)
		t.Fatal("broken build reported success")
	}
	_, body := holdGet(t, h.addr, "")
	if !strings.Contains(body, "undefined: undefinedThing") {
		t.Fatalf("hold page lacks the compiler error: %q", body)
	}
}

func TestBuildAndServeHoldShowsA11yFindings(t *testing.T) {
	root := clitest.WriteA11yTree(t, map[string]string{"app/screen.go": clitest.BadScreen})
	rt, err := isolation.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHold(t)
	var mu sync.Mutex
	var cmd *exec.Cmd
	clitest.CovT_capStdout(t, func() { buildAndServe(root, ".", h.addr, rt, &mu, &cmd, false, h, nil) })
	_, body := holdGet(t, h.addr, "")
	if !strings.Contains(body, "screen.go") || !strings.Contains(body, "Accessibility lint failed") {
		t.Fatalf("hold page lacks the a11y findings: %q", body)
	}
}

// holdApp writes a module whose server binds the --addr gofastr dev
// passes, or exits at once with status 3 when crash is set.
func holdApp(t *testing.T, dir string, crash bool) {
	t.Helper()
	main := `package main

import (
	"net/http"
	"os"
)

func main() {
	if CRASH {
		os.Exit(3)
	}
	addr := os.Args[len(os.Args)-1]
	_ = http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("app is up"))
	}))
}
`
	if crash {
		main = strings.Replace(main, "CRASH", "true", 1)
	} else {
		main = strings.Replace(main, "CRASH", "false", 1)
	}
	writeDevFile(t, filepath.Join(dir, "go.mod"), "module holdtest\n\ngo 1.21\n")
	writeDevFile(t, filepath.Join(dir, "main.go"), main)
}

func pollHold(t *testing.T, addr, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		res, err := (&http.Client{Timeout: time.Second}).Get("http://" + addr + "/")
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			res.Body.Close()
			body = string(b)
			if strings.Contains(body, want) {
				return body
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never answered %q; last body %q", addr, want, body)
	return ""
}

func TestBuildAndServeHandsThePortToTheApp(t *testing.T) {
	dir := t.TempDir()
	holdApp(t, dir, false)
	h := newTestHold(t)
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	clitest.CovT_capStdout(t, func() { ok = buildAndServe(dir, ".", h.addr, nil, &mu, &cmd, true, h, nil) })
	defer KillServer(&mu, &cmd)
	if !ok {
		t.Fatal("build failed")
	}
	pollHold(t, h.addr, "app is up")
}

// A save kills the app on purpose. Its Wait goroutine must not report
// that as an exit, or every rebuild would flash "The app stopped".
func TestBuildAndServeKillIsNotAnExit(t *testing.T) {
	dir := t.TempDir()
	holdApp(t, dir, false)
	h := newTestHold(t)
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	clitest.CovT_capStdout(t, func() { ok = buildAndServe(dir, ".", h.addr, nil, &mu, &cmd, true, h, nil) })
	if !ok {
		KillServer(&mu, &cmd)
		t.Fatal("build failed")
	}
	pollHold(t, h.addr, "app is up")
	KillServer(&mu, &cmd)
	time.Sleep(500 * time.Millisecond)
	if phase, _, _, _ := h.state(); phase != holdBuilding {
		t.Fatalf("hold phase after a kill = %q, want %q", phase, holdBuilding)
	}
}

func TestBuildAndServeHoldShowsAppExit(t *testing.T) {
	dir := t.TempDir()
	holdApp(t, dir, true)
	h := newTestHold(t)
	var mu sync.Mutex
	var cmd *exec.Cmd
	clitest.CovT_capStdout(t, func() { buildAndServe(dir, ".", h.addr, nil, &mu, &cmd, true, h, nil) })
	defer KillServer(&mu, &cmd)
	pollHold(t, h.addr, "exit status 3")
}

func TestHoldDevAddrRejectsABusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := holdDevAddr(ln.Addr().String()); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("busy port: err = %v, want an already-in-use refusal", err)
	}
	if h, err := holdDevAddr("127.0.0.1:0"); h != nil || err != nil {
		t.Fatalf("port 0 should get no hold, got %v, %v", h, err)
	}
}

// exposedHold binds every interface, as `gofastr dev --addr 0.0.0.0:...`
// does, and returns the hold plus a loopback address to reach it on.
func exposedHold(t *testing.T) (*devHold, string) {
	t.Helper()
	h, err := newDevHold("0.0.0.0:0")
	if err != nil {
		t.Skipf("cannot bind every interface: %v", err)
	}
	t.Cleanup(h.release)
	_, port, err := net.SplitHostPort(h.addr)
	if err != nil {
		t.Fatal(err)
	}
	return h, net.JoinHostPort("127.0.0.1", port)
}

// The hold page answered every path with the last 64 KiB of build and
// lint output: on a LAN bind, any peer read absolute paths and quoted
// source. Off loopback the page says what happened and leaves the output
// to the terminal.
func TestExposedHoldHidesBuildOutput(t *testing.T) {
	h, addr := exposedHold(t)
	h.show(holdBuildFailed, "/home/dev/secret/main.go:3:2: undefined: x\n")
	for _, accept := range []string{"", "text/html"} {
		res, body := holdGet(t, addr, accept)
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", res.StatusCode)
		}
		if strings.Contains(body, "secret/main.go") {
			t.Fatalf("an exposed hold served the build output (Accept %q): %q", accept, body)
		}
		if !strings.Contains(body, "terminal") {
			t.Fatalf("an exposed hold must point at the terminal (Accept %q): %q", accept, body)
		}
	}
}

// A loopback hold served its build output to any Host. A page on
// evil.example whose name a rebinding DNS server points at 127.0.0.1 is
// same-origin with the hold, so its script read the absolute paths and
// quoted source the exposed-bind rule above keeps off the network. A
// request naming a host that is not this machine gets the page without
// the output; every loopback spelling still gets it.
func TestLoopbackHoldWithholdsBuildOutputFromAForeignHost(t *testing.T) {
	h := newTestHold(t)
	h.show(holdBuildFailed, "/home/dev/secret/main.go:3:2: undefined: x\n")
	_, port, err := net.SplitHostPort(h.addr)
	if err != nil {
		t.Fatal(err)
	}
	get := func(host, accept string) string {
		t.Helper()
		req, err := http.NewRequest("GET", "http://"+h.addr+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("Host %q: status %d, want 503", host, res.StatusCode)
		}
		return string(body)
	}
	for _, accept := range []string{"", "text/html"} {
		for _, host := range []string{"evil.example:" + port, "evil.example", "127.0.0.1.evil.example:" + port} {
			body := get(host, accept)
			if strings.Contains(body, "secret/main.go") {
				t.Errorf("Host %q (Accept %q) read the build output: %q", host, accept, body)
			}
			if !strings.Contains(body, "terminal") {
				t.Errorf("Host %q (Accept %q): the page must point at the terminal: %q", host, accept, body)
			}
		}
		for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "LOCALHOST:" + port} {
			if body := get(host, accept); !strings.Contains(body, "secret/main.go") {
				t.Errorf("Host %q (Accept %q) lost the build output: %q", host, accept, body)
			}
		}
	}
}

// The hold's own /__livereload follows the app's rule: off on an exposed
// bind unless dev.LiveReloadExposeEnv opts in.
func TestExposedHoldDropsLiveReload(t *testing.T) {
	t.Setenv(dev.LiveReloadExposeEnv, "")
	_, addr := exposedHold(t)
	if code := holdStreamStatus(t, addr); code != http.StatusNotFound {
		t.Fatalf("exposed hold /__livereload answered %d, want 404", code)
	}
	t.Setenv(dev.LiveReloadExposeEnv, "1")
	_, addr = exposedHold(t)
	if code := holdStreamStatus(t, addr); code != http.StatusOK {
		t.Fatalf("with the opt-in, /__livereload answered %d, want 200", code)
	}
}

func holdStreamStatus(t *testing.T, addr string) int {
	t.Helper()
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/__livereload")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}
