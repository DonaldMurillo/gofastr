//go:build linux

package devcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// The app buildAndServe starts carries the parent-death signal, so it ends with the loop.
func TestBuildAndServeTiesTheAppToTheLoop(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(time.Minute) }\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module devtie\n\ngo 1.21\n")
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(DevServerBinaryPath(rt)) })
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	clitest.CovT_capOutput(t, func() { ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd, true, nil, nil) })
	mu.Lock()
	app := cmd
	mu.Unlock()
	defer KillServer(&mu, &cmd)
	if !ok || app == nil {
		t.Fatal("setup broken: the app did not start")
	}
	if app.SysProcAttr == nil || app.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Errorf("the app was started without the parent-death signal: %+v", app.SysProcAttr)
	}
}
