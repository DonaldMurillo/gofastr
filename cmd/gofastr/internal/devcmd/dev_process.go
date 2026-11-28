package devcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// DevServerBinDir is this process's private temp directory for the rebuilt binary, or "" if it could not be made.
var DevServerBinDir = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "gofastr-dev-server-")
	if err != nil {
		return ""
	}
	return dir
})

// DevServerBinaryPath is the rebuilt binary's path in DevServerBinDir, or "" without one.
func DevServerBinaryPath(runtimeIsolation *isolation.Runtime) string {
	dir := DevServerBinDir()
	if dir == "" {
		return ""
	}
	name := "server"
	if runtimeIsolation.Active() {
		name += "-" + runtimeIsolation.ID()
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

// BuildCommand is the dev loops' go build of pkg into bin.
func BuildCommand(bin, pkg string) *exec.Cmd {
	return exec.Command("go", "build", "-buildvcs=false", "-o", bin, pkg)
}

// RunBuildWithHeartbeat runs the build, printing a line every 10s; when interrupt
// closes it kills the build with its children and returns ErrBuildInterrupted.
func RunBuildWithHeartbeat(buildCmd *exec.Cmd, what string, interrupt <-chan struct{}) error {
	w := os.Stdout
	start := time.Now()
	if interrupt != nil {
		ownBuildGroup(buildCmd)
	}
	if closed(interrupt) {
		return ErrBuildInterrupted
	}
	if err := buildCmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- buildCmd.Wait() }()
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-interrupt:
			killBuild(buildCmd)
			<-done
			return ErrBuildInterrupted
		case <-ticker.C:
			fmt.Fprintf(w, "%s\n", cli.InfoString("Building %s (%ds)...", what, int(time.Since(start).Seconds())))
		}
	}
}

var killGrace = 5 * time.Second

var heartbeatEvery = 10 * time.Second

const exitPoll = 10 * time.Millisecond

// ErrBuildInterrupted is RunBuildWithHeartbeat's error for a build ended by its interrupt.
var ErrBuildInterrupted = errors.New("build interrupted by shutdown")

// KillServer stops the app in *cmd, SIGTERM then SIGKILL after killGrace, and clears
// *cmd; the caller must be waiting on the process.
func KillServer(mu *sync.Mutex, cmd **exec.Cmd) {
	mu.Lock()
	defer mu.Unlock()

	if *cmd != nil && (*cmd).Process != nil {
		stopProcess((*cmd).Process, killGrace)
		*cmd = nil
	}
}

var noSIGTERM = runtime.GOOS == "windows"

func stopProcess(p *os.Process, grace time.Duration) {
	if noSIGTERM || p.Signal(syscall.SIGTERM) != nil {
		_ = p.Kill()
		waitExit(p, grace)
		return
	}
	if !waitExit(p, grace) {
		_ = p.Kill()
	}
}

func waitExit(p *os.Process, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if errors.Is(p.Signal(syscall.Signal(0)), os.ErrProcessDone) {
			return true
		}
		time.Sleep(exitPoll)
	}
	return false
}
