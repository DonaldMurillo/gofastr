//go:build darwin || linux

package service

import (
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

// Windows stop read the daemon's start value after asking it to stop.
// A daemon that exits at once can have its pid handed to a newcomer by
// then, and stop watched the newcomer until it gave up and ran
// `schtasks /End`. The daemon now reports its own start value in the
// reply, read while the pid was certainly its own.
func TestStopUsesDaemonReportedStart(t *testing.T) {
	b, ft, _ := testBattery(t, "windows", Config{})
	shortStopWait(t)
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.ensure(); err != nil {
		t.Fatal(err)
	}
	// The newcomer now holding the daemon's old pid.
	newcomer := exec.Command("sleep", "60")
	if err := newcomer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newcomer.Process.Kill(); _ = newcomer.Wait() })
	now := procalive.Identify(newcomer.Process.Pid)
	if now.Start == 0 {
		t.Fatal("no start value: the recycled-pid check is vacuous here")
	}
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler),
		handle: func(op string) controlResponse {
			if op == "stop" {
				go ln.Close() // the daemon is gone at once
			}
			// It started at another moment than the newcomer.
			return controlResponse{OK: true, PID: now.PID, ProcStart: now.Start - 1}
		}}
	t.Cleanup(s.Close)
	go s.serve()

	start := time.Now()
	if err := (taskScheduler{}).stop(b); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := ft.got(); len(got) != 0 {
		t.Fatalf("stop ran %q against a daemon that had exited", got)
	}
	if took := time.Since(start); took >= stopWait {
		t.Fatalf("stop waited %v on the newcomer", took)
	}
}

// runningDaemon takes the identity the daemon reports, not one sampled
// from its pid afterwards.
func TestRunningDaemonTakesReport(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.ensure(); err != nil {
		t.Fatal(err)
	}
	self := procalive.Identify(os.Getpid())
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler),
		handle: func(string) controlResponse {
			return controlResponse{OK: true, PID: self.PID, ProcStart: self.Start + 7}
		}}
	t.Cleanup(s.Close)
	go s.serve()
	got, err := b.runningDaemon()
	if err != nil {
		t.Fatal(err)
	}
	if want := (procalive.Identity{PID: self.PID, Start: self.Start + 7}); got != want {
		t.Fatalf("runningDaemon = %+v, want the reported %+v", got, want)
	}
}

// The daemon's status and stop replies carry its own start value.
func TestDaemonReportsOwnStart(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	h := b.controlHandler(nil, "127.0.0.1:1", false, time.Now(), make(chan struct{}, 1))
	want := procalive.Identify(os.Getpid()).Start
	if want == 0 {
		t.Fatal("no start value for this process: the check is vacuous")
	}
	for _, op := range []string{"status", "stop"} {
		if got := h(op); got.ProcStart != want {
			t.Fatalf("%s reply ProcStart = %d, want %d", op, got.ProcStart, want)
		}
	}
}
