package framework_test

// Lifecycle edge cases of App.Run, against the exported API only: which
// error a stop that coincides with a failure reports, whose deadline the
// teardown runs on, and how a host's failing or panicking Stop is
// contained. Helpers (newLocalRun, recordingHost, events) are in
// host_run_test.go.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

type lifecycleLogWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (writer *lifecycleLogWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.Write(value)
}

func (writer *lifecycleLogWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

func TestShutdownDuringBootLogsFailureAfterCallerDeadline(t *testing.T) {
	for _, watcher := range []bool{false, true} {
		name := "external Shutdown"
		if watcher {
			name = "Run context watcher"
		}
		t.Run(name, func(t *testing.T) {
			logs := &lifecycleLogWriter{}
			run := newLocalRun(t, t.TempDir(), framework.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
			run.app.Config.ShutdownTimeout = 50 * time.Millisecond
			stopError := errors.New("transport would not close after caller deadline")
			host := newRecordingHost(run.rec)
			host.stopErr = stopError
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			run.app.OnStart(func(ctx context.Context) error {
				entered <- ctx
				<-release
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := run.run(ctx, host)
			lifecycle := recv(t, entered, "start hook")
			if watcher {
				cancel()
				recv(t, lifecycle.Done(), "watcher shutdown")
				time.Sleep(100 * time.Millisecond)
			} else {
				shutdownContext, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer stop()
				if err := run.app.Shutdown(shutdownContext); !errors.Is(err, context.DeadlineExceeded) {
					close(release)
					t.Fatalf("Shutdown = %v, want caller deadline", err)
				}
			}
			close(release)
			if err := waitRun(t, done); err != nil && (!watcher || !errors.Is(err, context.DeadlineExceeded)) {
				t.Fatalf("Run = %v", err)
			}
			if count := strings.Count(logs.String(), stopError.Error()); count != 1 {
				t.Fatalf("cleanup error logged %d times, want once: %s", count, logs.String())
			}
			if run.rec.has("host.run") || run.rec.count("onstop") != 1 || !dbClosed(t, run.data) {
				t.Fatalf("teardown incomplete: %v", run.rec.snapshot())
			}
		})
	}
}

func TestStartInteractiveSetupLogsIndependentFailureDuringStop(t *testing.T) {
	logs := &lifecycleLogWriter{}
	run := newLocalRun(t, t.TempDir(), framework.WithSetup(&wizardSetup{}), framework.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	address := make(chan string, 1)
	run.app.OnReady(func(value string) { address <- value })
	var base atomic.Value
	shutdown := make(chan error, 1)
	hookError := errors.New("deferred license check failed during stop")
	run.app.OnStart(func(context.Context) error {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			shutdown <- run.app.Shutdown(ctx)
		}()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			response, err := http.Get(base.Load().(string) + "/readyz")
			if err != nil {
				return hookError
			}
			response.Body.Close()
			if response.StatusCode == http.StatusServiceUnavailable {
				return hookError
			}
		}
		return errors.New("shutdown never started")
	})
	done := make(chan error, 1)
	go func() { done <- run.app.Start("127.0.0.1:0") }()
	select {
	case value := <-address:
		base.Store("http://" + value)
	case err := <-done:
		t.Fatalf("Start returned before OnReady: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("OnReady never fired")
	}
	response, err := http.Get(base.Load().(string) + "/finish")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := recv(t, shutdown, "Shutdown"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if strings.Contains(logs.String(), hookError.Error()) {
			return
		}
	}
	t.Fatalf("independent deferred failure not logged: %s", logs.String())
}

// teardownKey carries a value the teardown's context must keep.
type teardownKey struct{}

// teardownCtx records the deadline and the teardownKey value of the
// context one teardown step received.
type teardownCtx struct {
	mu          sync.Mutex
	seen        bool
	hasDeadline bool
	left        time.Duration
	value       any
}

func (o *teardownCtx) observe(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = true
	dl, ok := ctx.Deadline()
	o.hasDeadline = ok
	if ok {
		o.left = time.Until(dl)
	}
	o.value = ctx.Value(teardownKey{})
}

// check fails unless the step ran on a context whose deadline left more
// than min and at most max, carrying value.
func (o *teardownCtx) check(t *testing.T, step string, minLeft, maxLeft time.Duration, value any) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case !o.seen:
		t.Fatalf("%s never ran", step)
	case !o.hasDeadline:
		t.Fatalf("%s ran with no deadline", step)
	case o.left <= minLeft || o.left > maxLeft:
		t.Fatalf("%s had %v left, want more than %v and at most %v", step, o.left.Round(time.Millisecond), minLeft, maxLeft)
	case o.value != value:
		t.Fatalf("%s context carried %v, want %v", step, o.value, value)
	}
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// stallingBattery's Init blocks until the lifecycle context (captured by
// a seed hook, which runs first) is cancelled, then fails on its own.
type stallingBattery struct {
	lifecycle *context.Context
	entered   chan struct{}
	err       error
}

func (*stallingBattery) Name() string { return "stalling-init" }

func (b *stallingBattery) Init(*framework.App) error {
	close(b.entered)
	<-(*b.lifecycle).Done() // a Shutdown is pending
	return b.err
}

// A boot phase that fails for its own reason while a Shutdown is pending
// still fails Run: only the stop's own cancellation is a graceful stop.
func TestRunReportsIndependentBootErrorDuringStop(t *testing.T) {
	t.Run("plugin init", func(t *testing.T) {
		errInit := errors.New("battery config is invalid")
		r := newLocalRun(t, t.TempDir())
		var lifecycle context.Context
		r.app.WithSeed(func(ctx context.Context) error { lifecycle = ctx; return nil })
		b := &stallingBattery{lifecycle: &lifecycle, entered: make(chan struct{}), err: errInit}
		r.app.RegisterBattery(b)
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran after a failed boot")
		done := r.run(context.Background(), host)
		recv(t, b.entered, "plugin Init")

		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		err := waitRun(t, done)
		if !errors.Is(err, errInit) || !strings.Contains(err.Error(), "init plugins") {
			t.Fatalf("Run = %v, want the plugin's own Init failure", err)
		}
		if r.rec.has("host.run") || readyClosed(r.app) || r.rec.count("host.stop") != 1 || r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("teardown incomplete: ready %v, events %v", readyClosed(r.app), r.rec.snapshot())
		}
	})
	t.Run("start hook ignoring cancellation", func(t *testing.T) {
		errStart := errors.New("license check failed")
		r := newLocalRun(t, t.TempDir())
		entered := make(chan context.Context, 1)
		release := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			entered <- ctx
			<-release // not ctx: a check that does not watch for the stop
			return errStart
		})
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran after a failed boot")
		done := r.run(context.Background(), host)
		lifecycle := recv(t, entered, "the start hook")

		shut := make(chan error, 1)
		go func() {
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			shut <- r.app.Shutdown(sctx)
		}()
		recv(t, lifecycle.Done(), "Shutdown to cancel the lifecycle context")
		close(release)

		err := waitRun(t, done)
		if !errors.Is(err, errStart) || !strings.Contains(err.Error(), "start hooks") {
			t.Fatalf("Run = %v, want the start hook's own failure", err)
		}
		if err := recv(t, shut, "Shutdown"); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if r.rec.has("host.run") || readyClosed(r.app) || r.rec.count("battery.stop") != 1 || r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("teardown incomplete: ready %v, events %v", readyClosed(r.app), r.rec.snapshot())
		}
	})
}

// Database work the stop cancelled is the stop, not a failure, in the
// words the driver reports it in: context.Canceled for a statement, and
// database/sql's ErrTxDone for a transaction it rolled back when its
// context ended (what an interrupted migration's commit returns).
func TestRunCancelledDatabaseWorkIsGraceful(t *testing.T) {
	t.Run("statement", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		var stmtErr error
		entered := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			_, stmtErr = r.data.DB.ExecContext(ctx, "INSERT INTO notes (id, user_id, title) VALUES ('n1', 'u', 'late')")
			return stmtErr
		})
		done := r.run(context.Background(), newRecordingHost(r.rec))
		recv(t, entered, "the start hook")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run = %v, want nil for a statement the stop cancelled", err)
		}
		if !errors.Is(stmtErr, context.Canceled) {
			t.Fatalf("the statement returned %v; the test needs a cancelled statement", stmtErr)
		}
	})
	t.Run("transaction rolled back by its context", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		var commitErr error
		entered := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			tx, err := r.data.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			close(entered)
			<-ctx.Done()
			// database/sql rolls the transaction back once its context
			// ends; wait for that, so Commit reports it.
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
				if _, err := tx.ExecContext(context.Background(), "SELECT 1"); errors.Is(err, sql.ErrTxDone) {
					break
				}
				if time.Now().After(deadline) {
					return errors.New("database/sql never rolled the transaction back")
				}
			}
			if commitErr = tx.Commit(); commitErr != nil {
				return fmt.Errorf("record progress: commit: %w", commitErr)
			}
			return nil
		})
		done := r.run(context.Background(), newRecordingHost(r.rec))
		recv(t, entered, "the start hook")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run = %v, want nil for a transaction the stop rolled back", err)
		}
		if !errors.Is(commitErr, sql.ErrTxDone) {
			t.Fatalf("Commit returned %v; the test needs ErrTxDone", commitErr)
		}
		if r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("teardown incomplete: %v", r.rec.snapshot())
		}
	})
}

// A failure and a failing teardown are both reported, each once: Run
// reports what it tore down itself, and a Shutdown that started the
// teardown reports the teardown's error.
func TestRunReportsBootAndTeardownFailures(t *testing.T) {
	errStart := errors.New("license check failed")
	errStop := errors.New("transport would not close")
	// failingRun has a start hook that fails once released and a host
	// whose Stop fails.
	failingRun := func(t *testing.T) (*localRun, *recordingHost, chan context.Context, chan struct{}) {
		r := newLocalRun(t, t.TempDir())
		entered := make(chan context.Context, 1)
		release := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			entered <- ctx
			<-release
			return errStart
		})
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran after a failed boot")
		host.stopErr = errStop
		return r, host, entered, release
	}
	t.Run("rollback", func(t *testing.T) {
		r, host, entered, release := failingRun(t)
		close(release)
		done := r.run(context.Background(), host)
		recv(t, entered, "the start hook")
		err := waitRun(t, done)
		if !errors.Is(err, errStart) || !errors.Is(err, errStop) {
			t.Fatalf("Run = %v, want the start failure and the host's Stop failure", err)
		}
		if strings.Count(err.Error(), errStop.Error()) != 1 {
			t.Fatalf("Run reported the teardown failure more than once: %v", err)
		}
		if r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("teardown incomplete: %v", r.rec.snapshot())
		}
	})
	t.Run("shutdown pending", func(t *testing.T) {
		r, host, entered, release := failingRun(t)
		done := r.run(context.Background(), host)
		lifecycle := recv(t, entered, "the start hook")
		shut := make(chan error, 1)
		go func() {
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			shut <- r.app.Shutdown(sctx)
		}()
		recv(t, lifecycle.Done(), "Shutdown to cancel the lifecycle context")
		close(release)
		err := waitRun(t, done)
		if !errors.Is(err, errStart) || errors.Is(err, errStop) {
			t.Fatalf("Run = %v, want the start failure alone (the Shutdown reports the teardown)", err)
		}
		if serr := recv(t, shut, "Shutdown"); !errors.Is(serr, errStop) || !strings.Contains(serr.Error(), "stopping host") {
			t.Fatalf("Shutdown = %v, want the host's Stop failure", serr)
		}
		if r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("teardown incomplete: %v", r.rec.snapshot())
		}
	})
	t.Run("run context cancelled", func(t *testing.T) {
		r, host, entered, release := failingRun(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := r.run(ctx, host)
		lifecycle := recv(t, entered, "the start hook")
		cancel()
		recv(t, lifecycle.Done(), "Run's context to cancel the lifecycle context")
		close(release)
		err := waitRun(t, done)
		if !errors.Is(err, errStart) || !errors.Is(err, errStop) {
			t.Fatalf("Run = %v, want the start failure and the host's Stop failure", err)
		}
		if strings.Count(err.Error(), errStop.Error()) != 1 {
			t.Fatalf("Run reported the teardown failure more than once: %v", err)
		}
	})
}

// A host's Stop that fails or panics is reported by Shutdown, and the
// teardown after it (batteries, OnStop hooks) still runs.
func TestShutdownReportsHostStopFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*recordingHost)
		want  string
	}{
		{"error", func(h *recordingHost) { h.stopErr = errors.New("transport would not close") }, "transport would not close"},
		{"panic", func(h *recordingHost) { h.stopPanic = true }, "panicked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLocalRun(t, t.TempDir())
			host := newRecordingHost(r.rec)
			tc.setup(host)
			done := r.run(context.Background(), host)
			waitReady(t, r.app, done)
			recv(t, host.ran, "host.Run")

			var err error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err = r.app.Shutdown(sctx)
			}()
			if panicked != nil {
				t.Fatalf("the host's Stop panic escaped Shutdown: %v", panicked)
			}
			if err == nil || !strings.Contains(err.Error(), "stopping host") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Shutdown = %v, want a stopping-host error naming %q", err, tc.want)
			}
			got := r.rec.snapshot()
			hostStop, batStop, onStop := indexOf(got, "host.stop"), indexOf(got, "battery.stop"), indexOf(got, "onstop")
			if hostStop < 0 || batStop < hostStop || onStop < batStop || !dbClosed(t, r.data) {
				t.Fatalf("teardown after the host's Stop did not run in order: %v", got)
			}
			if err := waitRun(t, done); err != nil {
				t.Fatalf("Run after Shutdown = %v", err)
			}
		})
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// A stop during boot tears down on the budget of the stop that started
// it: the caller's Shutdown deadline, or for Run's own context the
// SIGTERM budget. Neither is cut to a fixed rollback timeout. A caller
// that gave up before the boot phase returned still gets its cleanup, on
// a bounded budget of its own.
func TestShutdownDuringBootKeepsTheStopsBudget(t *testing.T) {
	// stalledBoot has a start hook that holds boot until ctx is done.
	stalledBoot := func(t *testing.T) (*localRun, *recordingHost, *teardownCtx, *teardownCtx, chan struct{}) {
		r := newLocalRun(t, t.TempDir())
		var bat, hst teardownCtx
		r.bat.onStop = bat.observe
		host := newRecordingHost(r.rec)
		host.onStop = hst.observe
		entered := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		return r, host, &bat, &hst, entered
	}
	t.Run("Shutdown deadline", func(t *testing.T) {
		r, host, bat, hst, entered := stalledBoot(t)
		done := r.run(context.Background(), host)
		recv(t, entered, "the start hook")
		sctx, cancel := context.WithTimeout(context.WithValue(context.Background(), teardownKey{}, "shutdown"), 30*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run: %v", err)
		}
		hst.check(t, "host.Stop", 25*time.Second, 30*time.Second, "shutdown")
		bat.check(t, "battery OnStop", 25*time.Second, 30*time.Second, "shutdown")
	})
	t.Run("Run context", func(t *testing.T) {
		r, host, bat, hst, entered := stalledBoot(t)
		r.app.Config.ShutdownTimeout = 42 * time.Second
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), teardownKey{}, "run"))
		defer cancel()
		done := r.run(ctx, host)
		recv(t, entered, "the start hook")
		cancel()
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run: %v", err)
		}
		hst.check(t, "host.Stop", 35*time.Second, 42*time.Second, "run")
		bat.check(t, "battery OnStop", 35*time.Second, 42*time.Second, "run")
	})
	t.Run("caller gave up first", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		var bat teardownCtx
		r.bat.onStop = bat.observe
		entered := make(chan struct{})
		release := make(chan struct{})
		r.app.OnStart(func(context.Context) error {
			close(entered)
			<-release // ignores ctx: boot outlasts the caller's deadline
			return nil
		})
		done := r.run(context.Background(), newRecordingHost(r.rec))
		recv(t, entered, "the start hook")
		sctx, cancel := context.WithTimeout(context.WithValue(context.Background(), teardownKey{}, "short"), 50*time.Millisecond)
		defer cancel()
		shut := make(chan error, 1)
		go func() { shut <- r.app.Shutdown(sctx) }()
		select {
		case err := <-shut:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Shutdown = %v, want its own deadline", err)
			}
		case <-time.After(5 * time.Second):
			close(release) // let the run unwind before failing
			t.Fatal("Shutdown waited past its 50ms deadline for a boot phase still running")
		}
		if r.rec.has("battery.stop") || r.rec.has("onstop") {
			t.Fatalf("the teardown ran under a boot phase still running: %v", r.rec.snapshot())
		}
		close(release)
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run: %v", err)
		}
		bat.check(t, "battery OnStop", time.Second, 5*time.Second, "short")
		if r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("cleanup after an expired caller did not run: %v", r.rec.snapshot())
		}
	})
}

// Concurrent stops with different deadlines, and Run's own context,
// racing a boot phase keep the invariants: one teardown, every Shutdown
// returns nil or its own deadline, and Run returns nil.
func TestShutdownDuringBootBudgetRace(t *testing.T) {
	for i := range 30 {
		r := newLocalRun(t, t.TempDir())
		var bat teardownCtx
		r.bat.onStop = bat.observe
		entered := make(chan struct{})
		r.app.OnStart(func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			time.Sleep(time.Duration(i%4) * time.Millisecond)
			return ctx.Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		host := newRecordingHost(r.rec)
		done := r.run(ctx, host)
		recv(t, entered, "the start hook")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, d := range []time.Duration{time.Duration(i%3) * 10 * time.Millisecond, 10 * time.Second} {
			wg.Go(func() {
				sctx, scancel := context.WithTimeout(context.Background(), d)
				defer scancel()
				errs <- r.app.Shutdown(sctx)
			})
		}
		wg.Go(cancel)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("iteration %d: Shutdown = %v", i, err)
			}
		}
		if err := waitRun(t, done); err != nil {
			t.Fatalf("iteration %d: Run = %v", i, err)
		}
		for _, step := range []string{"host.stop", "battery.stop", "onstop"} {
			if n := r.rec.count(step); n != 1 {
				t.Fatalf("iteration %d: %s ran %d times: %v", i, step, n, r.rec.snapshot())
			}
		}
		bat.mu.Lock()
		hasDeadline := bat.hasDeadline
		bat.mu.Unlock()
		if !hasDeadline || r.rec.has("host.run") || !dbClosed(t, r.data) {
			t.Fatalf("iteration %d: deadline %v, events %v", i, hasDeadline, r.rec.snapshot())
		}
	}
}

// Under interactive setup the HTTP host starts the deferred OnStart hooks
// once the wizard completes. A Shutdown that lands after the last hook
// succeeded and before ServicesReady would close wins: ServicesReady
// never closes.
func TestStartInteractiveSetupStopWinsDeferredStart(t *testing.T) {
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(&wizardSetup{}))
	addr := make(chan string, 1)
	r.app.OnReady(func(a string) { addr <- a })
	var base atomic.Value
	var stopSeen atomic.Bool
	shut := make(chan error, 1)
	r.app.OnStart(func(context.Context) error {
		// The last deferred hook starts a Shutdown and returns once it is
		// under way: /readyz reports draining, or the listener is closed.
		go func() {
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			shut <- r.app.Shutdown(sctx)
		}()
		url := base.Load().(string) + "/readyz"
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			resp, err := http.Get(url)
			if err != nil {
				stopSeen.Store(true)
				return nil
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusServiceUnavailable && strings.Contains(string(body), "draining") {
				stopSeen.Store(true)
				return nil
			}
		}
		return errors.New("the Shutdown never began")
	})
	done := make(chan error, 1)
	go func() { done <- r.app.Start("127.0.0.1:0") }()
	select {
	case a := <-addr:
		base.Store("http://" + a)
	case err := <-done:
		t.Fatalf("Start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("OnReady never fired")
	}
	resp, err := http.Get(base.Load().(string) + "/finish")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := recv(t, shut, "Shutdown"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !stopSeen.Load() || !r.rec.has("onstart") {
		t.Fatalf("the deferred hooks did not run up to the stop: stop seen %v, events %v", stopSeen.Load(), r.rec.snapshot())
	}
	if readyClosed(r.app) {
		t.Fatal("ServicesReady closed after Shutdown began")
	}
}

const reentrantShutdownChildEnv = "GOFASTR_TEST_REENTRANT_SHUTDOWN_CHILD"

// A Shutdown with no deadline, called synchronously from inside a boot
// callback, never returns: it waits for a teardown that runs after the
// callback returns. That is the documented contract (bounded re-entry
// returns at its deadline; see TestRunReentrantShutdownIsBounded). The
// proof runs in a child process, so the goroutines it leaves blocked end
// with that process instead of leaking into this test binary.
func TestRunReentrantShutdownWithoutDeadlineBlocks(t *testing.T) {
	if os.Getenv(reentrantShutdownChildEnv) == "1" {
		reentrantShutdownChild(t)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRunReentrantShutdownWithoutDeadlineBlocks$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), reentrantShutdownChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "reentrant shutdown: still blocked") {
		t.Fatalf("child process: %v\n%s", err, out)
	}
}

func reentrantShutdownChild(t *testing.T) {
	app := framework.NewApp(framework.WithoutDefaultMiddleware(), framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Config.DisableSignalHandling = true
	entered := make(chan struct{})
	inner := make(chan error, 1)
	app.OnStart(func(context.Context) error {
		close(entered)
		inner <- app.Shutdown(context.Background())
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- app.Run(context.Background(), framework.ServicesHost()) }()
	recv(t, entered, "the start hook")
	select {
	case err := <-inner:
		t.Fatalf("Shutdown(context.Background()) inside OnStart returned %v; the documented contract is that it waits forever", err)
	case err := <-done:
		t.Fatalf("Run returned %v while its start hook was blocked", err)
	case <-time.After(500 * time.Millisecond):
	}
	fmt.Println("reentrant shutdown: still blocked")
	// The blocked run is the contract under test: end the process rather
	// than unwind it.
	os.Exit(0)
}
