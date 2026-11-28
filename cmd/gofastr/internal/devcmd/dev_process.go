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

// DevServerBinDir is the per-process 0700 directory under the temp root
// that holds the rebuilt server binary. Minted once per process with
// os.MkdirTemp so every caller (each rebuild's build -o and exec, the
// shutdown cleanup) agrees on one unguessable path: a predictable name
// in the shared temp root let a local co-user pre-plant a symlink and
// swap the dev server binary between build and exec (CWE-377).
var DevServerBinDir = sync.OnceValue(func() string {
	dir, err := os.MkdirTemp("", "gofastr-dev-server-")
	if err != nil {
		return ""
	}
	return dir
})

// DevServerBinaryPath is the per-process temp path the rebuilt server is
// compiled to, inside the private 0700 dir above. The isolation ID keeps
// concurrent isolated dev instances distinct inside the same dir.
func DevServerBinaryPath(runtimeIsolation *isolation.Runtime) string {
	dir := DevServerBinDir()
	if dir == "" {
		return "" // MkdirTemp failed; buildAndServe refuses on empty
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

// RunBuildWithHeartbeat waits out the dev build, printing a progress
// line every 10s it runs. `go build` is silent however long it takes,
// the banner above has already named an address, and under load a
// build can run for minutes after that: the heartbeat is what tells
// a reader (a person at the terminal, or the dev-loop e2e suite,
// which fails on output silence rather than a wall clock) "still
// building" from "wedged". The writer is captured at entry for the
// same reason the crash watcher below captures its copy: coverage
// tests swap os.Stdout around buildAndServe.
//
// When interrupt closes (the caller's SIGINT or SIGTERM), the build is
// killed with its children (the compiler, the linker, cgo's C
// toolchain) and ErrBuildInterrupted returned, so shutdown does not wait
// out a build whose result nobody will run. A non-nil interrupt puts the
// build in a process group of its own so the kill reaches all of them;
// the terminal's Ctrl-C then reaches only the caller, which must own
// the signal and close interrupt. A caller that does not own it passes
// nil, and the build stays in the terminal's group.
func RunBuildWithHeartbeat(buildCmd *exec.Cmd, what string, interrupt <-chan struct{}) error {
	w := os.Stdout
	start := time.Now()
	if interrupt != nil {
		ownBuildGroup(buildCmd)
	}
	// A signal before the build: nothing to start.
	if closed(interrupt) {
		return ErrBuildInterrupted
	}
	// Started here, not inside the waiting goroutine, so the interrupt
	// arm below reads a Process that Start has already set.
	if err := buildCmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- buildCmd.Wait() }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-interrupt:
			killBuild(buildCmd)
			<-done // WaitDelay bounds a child holding the output pipe
			return ErrBuildInterrupted
		case <-ticker.C:
			fmt.Fprintf(w, "%s\n", cli.InfoString("Building %s (%ds)...", what, int(time.Since(start).Seconds())))
		}
	}
}

// killGrace is how long KillServer lets the app drain after SIGTERM
// before it sends SIGKILL: shutdown hooks, the process-module drain (a
// 2s budget) and outbox flushes run in it. The framework ends open
// event streams (livereload, the SSE bus) when its drain starts, so an
// app usually exits well inside it, and KillServer returns as soon as
// it does.
var killGrace = 5 * time.Second

// exitPoll is how often KillServer looks for the process's exit.
const exitPoll = 10 * time.Millisecond

// ErrBuildInterrupted is RunBuildWithHeartbeat's answer for a build
// killed by shutdown.
var ErrBuildInterrupted = errors.New("build interrupted by shutdown")

// KillServer stops the current server process: SIGTERM, then up to
// killGrace for the app to exit, then SIGKILL. The caller must have a
// goroutine waiting on the process (both dev loops do, and a build's
// Run waits on its own): exit is observed when that wait reaps it, so a
// process nobody reaps reads as running until the grace ends.
func KillServer(mu *sync.Mutex, cmd **exec.Cmd) {
	mu.Lock()
	defer mu.Unlock()

	if *cmd != nil && (*cmd).Process != nil {
		stopProcess((*cmd).Process, killGrace)
		*cmd = nil
	}
}

// noSIGTERM is true where the OS has no SIGTERM to send (Windows); a
// test sets it to run that branch on this OS.
var noSIGTERM = runtime.GOOS == "windows"

// stopProcess sends SIGTERM and waits up to grace for p to exit before
// it kills it. Windows has no SIGTERM to send, so p is killed at once,
// and stopProcess still waits up to grace for the exit: TerminateProcess
// returns before the process is gone, and shutdown removes the binary
// dir next, which fails on an .exe still running.
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

// waitExit reports whether p exited within grace. Signal 0 delivers
// nothing; once the waiter has reaped p it answers os.ErrProcessDone,
// on Windows as on Unix.
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
