package main

// The host binary builds the module from the parent directory:
//gofastr:test-reads examples/processmodule-demo/

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests drive the REAL host binary, not this test binary: the point
// of issue #608 is that a shipped app, with no probe hook in its main,
// answers the sandbox probe and can host an untrusted module. The
// sandbox-conformance CI job runs them on Linux (bwrap) and macOS
// (sandbox-exec).

var (
	buildOnce sync.Once
	hostBin   string
	moduleBin string
	buildErr  error
)

// binaries builds the host and the demo module once per test run.
func binaries(t *testing.T) (host, module string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds two binaries; skipped in -short")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "processmodule-demo-host-test-*")
		if err == nil {
			// Readable by the sandbox account: on a root host the bwrap
			// backend runs the child as uid 65534, which must be able to
			// mount and exec what is built here.
			err = os.Chmod(dir, 0o755)
		}
		if err != nil {
			buildErr = err
			return
		}
		exe := ""
		if runtime.GOOS == "windows" {
			exe = ".exe"
		}
		hostBin = filepath.Join(dir, "host"+exe)
		moduleBin = filepath.Join(dir, "module"+exe)
		for _, b := range []struct{ out, pkg string }{
			{hostBin, "."},
			{moduleBin, ".."},
		} {
			cmd := exec.Command("go", "build", "-o", b.out, b.pkg)
			if out, err := cmd.CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("go build %s: %v\n%s", b.pkg, err, out)
				return
			}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return hostBin, moduleBin
}

// runCheckSandbox runs `host -check-sandbox` and decodes its report.
func runCheckSandbox(t *testing.T, host string) sandboxReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, host, "-check-sandbox")
	cmd.Dir = t.TempDir()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("host -check-sandbox: %v\nstderr:\n%s", err, stderr.String())
	}
	var rep sandboxReport
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	t.Logf("host sandbox report: backend=%s available=%v conforms=%v", rep.Backend, rep.Available, rep.Conforms)
	for _, p := range rep.Probes {
		t.Logf("  %s %-11s reported=%v %s", p.ID, p.Status, p.Reported, p.Detail)
	}
	return rep
}

// The host binary is its own probe child. Every P1–P7 probe the app runs
// re-executes the host, and every one must come back with a result line:
// the framework's package init ran the probe body instead of the host's
// main. Whether the backend conforms is the host's business; this test is
// about the wiring, so it holds on a conforming host and a failing one.
func TestHostBinaryAnswersSandboxProbe(t *testing.T) {
	host, _ := binaries(t)
	rep := runCheckSandbox(t, host)
	if !rep.Available {
		t.Skipf("no sandbox backend on %s (%s): untrusted modules fail closed here", runtime.GOOS, rep.Missing)
	}
	if len(rep.Probes) != 7 {
		t.Fatalf("report has %d probes, want P1-P7", len(rep.Probes))
	}
	for _, p := range rep.Probes {
		if !p.Reported {
			t.Errorf("probe %s: the host binary never reported (status=%s detail=%q); it ran something other than the probe body", p.ID, p.Status, p.Detail)
		}
	}
}

// The untrusted demo module runs where the sandbox conforms and is refused,
// with the report, where it does not. There is no third outcome: a host
// that cannot confine the module never runs it unconfined.
func TestHostUntrustedModule(t *testing.T) {
	host, module := binaries(t)
	rep := runCheckSandbox(t, host)
	if !rep.Conforms && os.Getenv("GOFASTR_REQUIRE_SANDBOX_CONFORMANCE") != "" {
		t.Fatalf("the %s backend does not conform and GOFASTR_REQUIRE_SANDBOX_CONFORMANCE is set", rep.Backend)
	}
	if rep.Conforms {
		srv := startHost(t, host, module, "untrusted")
		body := getUntil200(t, srv.url+"/demo/hello")
		if !strings.Contains(body, "Hello from the demo module") {
			t.Fatalf("GET /demo/hello body = %q, want the demo screen", body)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, host, "-module", module, "-addr", freeAddr(t))
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != exitNoSandbox {
		t.Fatalf("host on a non-conforming machine: err=%v, want exit %d\n%s", err, exitNoSandbox, out)
	}
	for _, want := range []string{"cannot run untrusted modules", "no probe-passing sandbox backend"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("refusal output missing %q:\n%s", want, out)
		}
	}
}

// The trusted tier runs the same module everywhere, with crash isolation
// only: the example works on any machine, and the module's screen renders
// through the host.
func TestHostTrustedModule(t *testing.T) {
	host, module := binaries(t)
	srv := startHost(t, host, module, "trusted")
	body := getUntil200(t, srv.url+"/demo/hello")
	if !strings.Contains(body, "Hello from the demo module") {
		t.Fatalf("GET /demo/hello body = %q, want the demo screen", body)
	}
}

type hostServer struct {
	url string
	out *bytes.Buffer
}

// startHost runs the host serving the module at tier on a free port and
// stops it when the test ends.
func startHost(t *testing.T, host, module, tier string) hostServer {
	t.Helper()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, host, "-module", module, "-trust", tier, "-addr", addr)
	cmd.Dir = t.TempDir()
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start host: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("host output:\n%s", out.String())
		}
	})
	return hostServer{url: "http://" + addr, out: out}
}

// getUntil200 polls url until it answers 200: the module starts after the
// host does, and the host answers 503 until it is Ready.
func getUntil200(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return string(body)
			}
			last = fmt.Sprintf("%d %s", resp.StatusCode, body)
		} else {
			last = err.Error()
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("GET %s never answered 200; last: %s", url, last)
	return ""
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}
