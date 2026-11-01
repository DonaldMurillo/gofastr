//go:build darwin || linux

package service

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/procalive"
)

// fakeDaemon stands in for a running installed daemon: it answers the
// installed layout's control channel with the pid of a real child
// process, so procalive.Alive sees a live process until kill reaps it.
type fakeDaemon struct {
	pid  int
	kill func()
}

// startFakeDaemon starts one. With obeyStop, a control "stop" closes
// the channel and ends the process, as the daemon's drain does.
func startFakeDaemon(t *testing.T, b *Battery, obeyStop bool) *fakeDaemon {
	t.Helper()
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.ensure(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal(err)
	}
	d := &fakeDaemon{pid: child.Process.Pid}
	var once sync.Once
	// Reap the child: a zombie still answers kill(pid, 0).
	d.kill = func() {
		once.Do(func() {
			ln.Close()
			_ = child.Process.Kill()
			_ = child.Wait()
		})
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler), handle: func(op string) controlResponse {
		if op == "stop" && obeyStop {
			go d.kill()
		}
		return controlResponse{OK: true, PID: d.pid}
	}}
	go s.serve()
	t.Cleanup(func() {
		d.kill()
		s.wg.Wait()
	})
	return d
}

func shortStopWait(t *testing.T) {
	t.Helper()
	prev := stopWait
	stopWait = 500 * time.Millisecond
	t.Cleanup(func() { stopWait = prev })
}

func launchdUID(t *testing.T) {
	t.Helper()
	prev := currentUID
	currentUID = func() int { return 501 }
	t.Cleanup(func() { currentUID = prev })
}

// A stopped daemon is already stopped: no launchctl call, no error.
func TestLaunchdStopNotRunningIsNoop(t *testing.T) {
	b, ft, _ := testBattery(t, "darwin", Config{})
	launchdUID(t)
	if err := (launchd{}).stop(b); err != nil {
		t.Fatal(err)
	}
	if got := ft.got(); len(got) != 0 {
		t.Fatalf("argv %q; want none", got)
	}
}

// stop sends SIGTERM and returns only once the process has exited.
func TestLaunchdStopWaitsForExit(t *testing.T) {
	b, ft, _ := testBattery(t, "darwin", Config{})
	launchdUID(t)
	d := startFakeDaemon(t, b, false)
	ft.hook["launchctl kill"] = func() { go func() { time.Sleep(200 * time.Millisecond); d.kill() }() }
	if err := (launchd{}).stop(b); err != nil {
		t.Fatal(err)
	}
	if procalive.Alive(d.pid) {
		t.Fatal("stop returned while the daemon was still alive")
	}
	if got := ft.got(); len(got) != 1 || got[0] != "launchctl kill SIGTERM gui/501/dev.gofastr.svctest" {
		t.Fatalf("argv %q", got)
	}
}

// A daemon that ignores SIGTERM makes stop fail, not report success.
func TestLaunchdStopTimesOut(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	launchdUID(t)
	shortStopWait(t)
	d := startFakeDaemon(t, b, false)
	err := (launchd{}).stop(b)
	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("stop error = %v; want a did-not-exit error", err)
	}
	if !procalive.Alive(d.pid) {
		t.Fatal("fake daemon died on its own")
	}
}

// The control channel closing is not the process exiting: stop keeps
// waiting while the pid is alive.
func TestWaitExitWatchesThePID(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	shortStopWait(t)
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	daemon := procalive.Identify(child.Process.Pid)
	if err := b.waitExit(daemon); err == nil {
		t.Fatal("waitExit returned nil with no control channel but a live pid")
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	if err := b.waitExit(daemon); err != nil {
		t.Fatalf("waitExit after the exit: %v", err)
	}
}

// Once the daemon exits its pid may go to a newer process. That process
// is not the daemon: stop must not wait it out and report "did not
// exit", and uninstall must not refuse over it.
func TestWaitExitIgnoresRecycledPID(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	shortStopWait(t)
	newcomer := exec.Command("sleep", "60")
	if err := newcomer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newcomer.Process.Kill(); _ = newcomer.Wait() })
	now := procalive.Identify(newcomer.Process.Pid)
	if now.Start == 0 {
		t.Fatal("no start value: the recycled-pid check is vacuous here")
	}
	// The daemon held this pid before and started at another moment.
	daemon := procalive.Identity{PID: now.PID, Start: now.Start - 1}
	if err := b.waitExit(daemon); err != nil {
		t.Fatalf("waitExit on a recycled pid: %v", err)
	}
}

// The daemon's start value is read when it answers, while its pid is
// still certainly its own; read later, it could be a newcomer's.
func TestRunningDaemonReadsStart(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	d := startFakeDaemon(t, b, false)
	got, err := b.runningDaemon()
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != d.pid || got.Start == 0 || got.Start != procalive.Identify(d.pid).Start {
		t.Fatalf("runningDaemon = %+v; want pid %d with its start value", got, d.pid)
	}
}

// Windows stop drains over the control channel; no /End when the
// daemon exits in time.
func TestTaskSchedulerStopDrains(t *testing.T) {
	b, ft, _ := testBattery(t, "windows", Config{})
	d := startFakeDaemon(t, b, true)
	if err := (taskScheduler{}).stop(b); err != nil {
		t.Fatal(err)
	}
	if procalive.Alive(d.pid) {
		t.Fatal("stop returned while the daemon was still alive")
	}
	if got := ft.got(); len(got) != 0 {
		t.Fatalf("argv %q; want none", got)
	}
}

// A drain that overruns gets /End, and stop still waits for the exit.
func TestTaskSchedulerStopEndsAStuckDaemon(t *testing.T) {
	b, ft, _ := testBattery(t, "windows", Config{})
	shortStopWait(t)
	d := startFakeDaemon(t, b, false)
	ft.hook["schtasks /End"] = d.kill
	if err := (taskScheduler{}).stop(b); err != nil {
		t.Fatal(err)
	}
	if procalive.Alive(d.pid) {
		t.Fatal("stop returned while the daemon was still alive")
	}
	want := []string{"schtasks /End /TN dev.gofastr.svctest-S-1-5-21-7"}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

// uninstall deletes nothing while the daemon it unregistered is alive:
// it holds the database and the binary.
func TestUninstallKeepsFilesWhileRunning(t *testing.T) {
	b, _, out := testBattery(t, "darwin", Config{})
	launchdUID(t)
	shortStopWait(t)
	startFakeDaemon(t, b, false)
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := b.binDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	err = b.uninstall(true)
	if err == nil || !strings.Contains(err.Error(), "nothing was deleted") {
		t.Fatalf("uninstall error = %v; want nothing was deleted", err)
	}
	if !fileExists(bin) || !fileExists(inst.dir) {
		t.Fatal("uninstall deleted files while the daemon was running")
	}
	if strings.Contains(out.String(), "uninstalled") {
		t.Fatalf("uninstall reported success: %q", out)
	}
}

// Two daemons of one instance cannot both pass the lock.
func TestLockInstanceRefusesSecondHolder(t *testing.T) {
	dir := shortTempDir(t)
	release, err := lockInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockInstance(dir); !errors.Is(err, errAlreadyRunning) {
		t.Fatalf("second lock error = %v; want errAlreadyRunning", err)
	}
	release()
	release2, err := lockInstance(dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	release2()
	fi, err := os.Stat(filepath.Join(dir, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("lock file mode %v; want owner-only", fi.Mode().Perm())
	}
}

// silentDaemon binds the installed layout's control socket and accepts
// without ever answering, the way a daemon still in its builder does.
// end closes the socket and every held connection: the process exiting.
func silentDaemon(t *testing.T, b *Battery) (end func()) {
	t.Helper()
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.ensure(); err != nil {
		t.Fatal(err)
	}
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	var once sync.Once
	end = func() {
		once.Do(func() {
			ln.Close()
			mu.Lock()
			for _, c := range held {
				c.Close()
			}
			mu.Unlock()
		})
	}
	t.Cleanup(end)
	return end
}

// noStartingGrace makes one silent probe count: the daemon is starting
// (or wedged) with no wait for it to answer.
func noStartingGrace(t *testing.T) {
	t.Helper()
	prev := startingWait
	startingWait = 0
	t.Cleanup(func() { startingWait = prev })
}

// A daemon that accepts the stop request and never answers is wedged:
// Windows stop ends the task instead of failing, so uninstall can go on.
func TestTaskSchedulerStopEndsAWedgedDaemon(t *testing.T) {
	noStartingGrace(t)
	b, ft, _ := testBattery(t, "windows", Config{})
	ft.hook["schtasks /End"] = silentDaemon(t, b)
	if err := (taskScheduler{}).stop(b); err != nil {
		t.Fatalf("stop of a wedged daemon: %v", err)
	}
	want := []string{"schtasks /End /TN dev.gofastr.svctest-S-1-5-21-7"}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

// A daemon whose builder never returns answers no status. macOS stop
// boots the job out and waits for the exit, instead of refusing. A
// SIGTERM would kill it (no handler before ready) and launchd's
// KeepAlive would restart the crash.
func TestLaunchdStopBootsOutStartingDaemon(t *testing.T) {
	noStartingGrace(t)
	b, ft, _ := testBattery(t, "darwin", Config{})
	launchdUID(t)
	shortStopWait(t)
	ft.hook["launchctl bootout"] = silentDaemon(t, b)
	if err := (launchd{}).stop(b); err != nil {
		t.Fatalf("stop of a starting daemon: %v", err)
	}
	want := []string{"launchctl bootout gui/501/dev.gofastr.svctest"}
	if got := ft.got(); !reflect.DeepEqual(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
}

// uninstall of a daemon still in its builder unregisters it, waits for
// it to go, and then deletes, instead of refusing.
func TestUninstallProceedsWhileStarting(t *testing.T) {
	noStartingGrace(t)
	b, ft, out := testBattery(t, "darwin", Config{})
	launchdUID(t)
	shortStopWait(t)
	ft.hook["launchctl bootout"] = silentDaemon(t, b)
	bin, err := b.binDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := b.uninstall(false); err != nil {
		t.Fatalf("uninstall of a starting daemon: %v", err)
	}
	if fileExists(bin) {
		t.Fatal("uninstall left the installed binary")
	}
	if !strings.Contains(out.String(), "uninstalled") {
		t.Fatalf("uninstall output %q", out)
	}
}

// Files stay while a starting daemon outlives the unregister.
func TestUninstallKeepsFilesWhileStarting(t *testing.T) {
	noStartingGrace(t)
	b, _, _ := testBattery(t, "darwin", Config{})
	launchdUID(t)
	shortStopWait(t)
	silentDaemon(t, b)
	bin, err := b.binDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	err = b.uninstall(false)
	if err == nil || !strings.Contains(err.Error(), "nothing was deleted") {
		t.Fatalf("uninstall error = %v; want nothing was deleted", err)
	}
	if !fileExists(bin) {
		t.Fatal("uninstall deleted files while the daemon was starting")
	}
}

// A daemon still building its app has bound its socket but answers only
// once ready. Windows stop waits for that answer and lets it drain,
// instead of hard-killing it, possibly mid-migration, as wedged.
func TestTaskSchedulerStopWaitsOutStartup(t *testing.T) {
	b, ft, _ := testBattery(t, "windows", Config{})
	inst, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.ensure(); err != nil {
		t.Fatal(err)
	}
	ln, err := listenControl(inst.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler),
		handle: func(op string) controlResponse {
			if op == "stop" {
				go ln.Close() // the drain: the control channel goes away
			}
			return controlResponse{OK: true}
		}}
	t.Cleanup(s.Close)
	go func() {
		time.Sleep(controlDeadline + time.Second) // the first stop goes unanswered
		s.serve()
	}()
	if err := (taskScheduler{}).stop(b); err != nil {
		t.Fatalf("stop of a starting daemon: %v", err)
	}
	if got := ft.got(); len(got) != 0 {
		t.Fatalf("stop hard-killed a starting daemon: %q", got)
	}
}

// rotate-secret on a socket that answers but refuses says the running
// copy did not take the new secret, instead of reporting a clean
// rotate.
func TestRotateReportsUnreachableDaemon(t *testing.T) {
	b, _, out := testBattery(t, "darwin", Config{})
	if err := b.rotate(true); err != nil {
		t.Fatalf("rotate with nothing running: %v", err)
	}
	dev, err := b.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := listenControl(dev.socketPath())
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: ln, log: slog.New(slog.DiscardHandler), handle: func(string) controlResponse {
		return controlResponse{Error: "busy"}
	}}
	go s.serve()
	t.Cleanup(s.Close)
	err = b.rotate(true)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("rotate error = %v; want a did-not-answer error", err)
	}
	if strings.Contains(out.String(), "restart the dev run") {
		t.Fatalf("rotate claimed the daemon answered: %q", out)
	}
}

// flakyListener fails its first Accept with an error that is not a
// closed listener.
type flakyListener struct {
	net.Listener
	once sync.Once
}

func (f *flakyListener) Accept() (net.Conn, error) {
	var fail bool
	f.once.Do(func() { fail = true })
	if fail {
		return nil, errors.New("accept: too many open files")
	}
	return f.Listener.Accept()
}

// One failed Accept does not leave the control channel deaf.
func TestControlServerSurvivesAcceptError(t *testing.T) {
	prev := acceptRetryDelay
	acceptRetryDelay = time.Millisecond
	t.Cleanup(func() { acceptRetryDelay = prev })
	path := filepath.Join(shortTempDir(t), "c.sock")
	ln, err := listenControl(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &controlServer{ln: &flakyListener{Listener: ln}, handle: echoOp, log: slog.New(slog.DiscardHandler)}
	go s.serve()
	t.Cleanup(s.Close)
	if _, err := callControl(path, "status"); err != nil {
		t.Fatalf("control channel after an accept error: %v", err)
	}
}
