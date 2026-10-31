//go:build unix

package main

import (
	"os/exec"
	"testing"
	"time"
)

// startServeProc starts script under sh and hands it to watchServe.
func startServeProc(t *testing.T, script string) *serveProc {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return watchServe(cmd)
}

// stopWithin fails the test when p.stop does not return in time.
func stopWithin(t *testing.T, p *serveProc, grace, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { p.stop(grace); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("stop did not return within %v", limit)
	}
}

// A serve that exits before it is ready is seen by the readiness race,
// and the cleanup after it must still return: the single Wait's result
// was consumed by the race, so the cleanup blocked on it forever
// (2026-10-27 K6).
func TestServeExitBeforeReadyStops(t *testing.T) {
	p := startServeProc(t, "exit 1")
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("exited never closed")
	}
	stopWithin(t, p, 50*time.Millisecond, 5*time.Second)
}

// SIGTERM ends a serve well inside the grace period.
func TestServeStopTerminates(t *testing.T) {
	p := startServeProc(t, "exec sleep 30")
	stopWithin(t, p, 5*time.Second, 4*time.Second)
}

// A serve that ignores SIGTERM is killed after the grace period.
func TestServeStopKillsAfterGrace(t *testing.T) {
	p := startServeProc(t, "trap '' TERM; while :; do sleep 0.05; done")
	time.Sleep(100 * time.Millisecond) // let the trap install
	stopWithin(t, p, 100*time.Millisecond, 5*time.Second)
	select {
	case <-p.exited:
	default:
		t.Fatal("stop returned with the child still running")
	}
}
