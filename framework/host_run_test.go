package framework_test

// Acceptance tests for App.Run and framework.Host, written against the
// exported API only: an external presentation library sees exactly this.
// Every App runs on a real SQLite installation from localapp.Open.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// events records lifecycle steps in the order they happen.
type events struct {
	mu   sync.Mutex
	list []string
}

func (e *events) add(s string) {
	e.mu.Lock()
	e.list = append(e.list, s)
	e.mu.Unlock()
}

func (e *events) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

func (e *events) has(s string) bool { return slices.Contains(e.snapshot(), s) }

func (e *events) count(s string) int {
	n := 0
	for _, v := range e.snapshot() {
		if v == s {
			n++
		}
	}
	return n
}

// notesBattery declares the app's schema from its Init, the way a plugin
// or battery does, so the engine's second migrate pass is what creates
// the table. Its start and stop are recorded.
type notesBattery struct {
	rec     *events
	initErr error
	init    func(*framework.App) // extra Init behavior
	seedErr error
	onStop  func(context.Context) // observes the teardown's context
}

func (*notesBattery) Name() string { return "notes-test" }

func (b *notesBattery) Init(app *framework.App) error {
	b.rec.add("battery.init")
	if b.initErr != nil {
		return b.initErr
	}
	app.Entity("notes", framework.EntityConfig{
		Scope: &framework.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true},
		},
		Seed: func(context.Context, *sql.DB) error {
			b.rec.add("entity.seed")
			return b.seedErr
		},
	})
	if b.init != nil {
		b.init(app)
	}
	return nil
}

func (b *notesBattery) OnStart(context.Context) error { b.rec.add("battery.start"); return nil }
func (b *notesBattery) OnStop(ctx context.Context) error {
	b.rec.add("battery.stop")
	if b.onStop != nil {
		b.onStop(ctx)
	}
	return nil
}

// recordingHost is an external Host: it compiles against the exported
// API alone. Its Run blocks until the App stops unless told otherwise.
type recordingHost struct {
	rec       *events
	runErr    error
	early     bool // return nil at once
	panics    bool
	stopErr   error                 // Stop returns it
	stopPanic bool                  // Stop panics
	onStop    func(context.Context) // observes Stop's context
	ran       chan struct{}
	once      sync.Once
}

func newRecordingHost(rec *events) *recordingHost {
	return &recordingHost{rec: rec, ran: make(chan struct{})}
}

func (h *recordingHost) Run(ctx context.Context, s *framework.Session) error {
	h.rec.add("host.run")
	h.once.Do(func() { close(h.ran) })
	if s == nil {
		return errors.New("nil session")
	}
	switch {
	case h.panics:
		panic("host exploded")
	case h.runErr != nil:
		return h.runErr
	case h.early:
		return nil
	}
	<-ctx.Done()
	return nil
}

func (h *recordingHost) Stop(ctx context.Context) error {
	h.rec.add("host.stop")
	if h.onStop != nil {
		h.onStop(ctx)
	}
	if h.stopPanic {
		panic("host stop exploded")
	}
	return h.stopErr
}

var _ framework.Host = (*recordingHost)(nil)

type localRun struct {
	app  *framework.App
	data *localapp.Data
	rec  *events
	bat  *notesBattery
}

// newLocalRun builds an App on the installation in dir. OnStop closes the
// data, the ownership localapp documents for WithDB.
func newLocalRun(t *testing.T, dir string, opts ...framework.AppOption) *localRun {
	t.Helper()
	if prev := owner.GetExtractor(); prev == nil || !localapp.IsExtractor(prev) {
		owner.SetExtractor(localapp.Extractor)
		t.Cleanup(func() { owner.SetExtractor(prev) })
	}

	data, err := localapp.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	rec := &events{}
	app := framework.NewApp(append([]framework.AppOption{
		framework.WithDB(data.DB),
		framework.WithSecret(data.Secret),
		framework.WithoutDefaultMiddleware(),
		framework.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, opts...)...)
	app.Config.DisableSignalHandling = true
	app.OnStop(func() error {
		rec.add("onstop")
		return data.Close()
	})
	bat := &notesBattery{rec: rec}
	app.RegisterBattery(bat)
	app.WithSeed(func(context.Context) error { rec.add("seed.hook"); return nil })
	app.OnStart(func(context.Context) error { rec.add("onstart"); return nil })
	return &localRun{app: app, data: data, rec: rec, bat: bat}
}

// run starts app.Run on a goroutine and returns its result channel.
func (r *localRun) run(ctx context.Context, host framework.Host) chan error {
	done := make(chan error, 1)
	go func() { done <- r.app.Run(ctx, host) }()
	return done
}

func waitReady(t *testing.T, app *framework.App, done chan error) {
	t.Helper()
	select {
	case <-app.ServicesReady():
	case err := <-done:
		t.Fatalf("Run returned before services were ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("services never became ready")
	}
}

func waitRun(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func readyClosed(app *framework.App) bool {
	select {
	case <-app.ServicesReady():
		return true
	default:
		return false
	}
}

func dbClosed(t *testing.T, data *localapp.Data) bool {
	t.Helper()
	err := data.DB.Ping()
	return err != nil && strings.Contains(err.Error(), "closed")
}

// A local installation runs end to end under the services-only host:
// plugin-declared schema, the engine's phase order, owner-scoped data,
// and identity, secret and rows persisting across a rebuilt App.
func TestRunServicesHostLocalInstallation(t *testing.T) {
	dir := t.TempDir()
	first := newLocalRun(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := first.run(ctx, framework.ServicesHost())
	waitReady(t, first.app, done)

	want := []string{"seed.hook", "battery.init", "entity.seed", "battery.start", "onstart"}
	if got := first.rec.snapshot(); !slices.Equal(got, want) {
		t.Fatalf("boot order = %v, want %v", got, want)
	}

	notes := first.app.MustCrudHandler("notes")
	mine := handler.SetUser(context.Background(), first.data.User)
	row, err := notes.CreateOne(mine, map[string]any{"title": "kept"})
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}
	id, _ := row["id"].(string)
	if id == "" {
		t.Fatalf("created row has no id: %v", row)
	}
	var rowOwner string
	if err := first.data.DB.QueryRow("SELECT user_id FROM notes WHERE id = ?", id).Scan(&rowOwner); err != nil || rowOwner != first.data.User.GetID() {
		t.Fatalf("stored owner = %q (%v), want the installation identity %q", rowOwner, err, first.data.User.GetID())
	}
	stranger, err := localapp.LoadOrMint(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	theirs := handler.SetUser(context.Background(), stranger)
	if _, err := notes.GetOne(theirs, id, nil); err == nil {
		t.Fatal("another identity read the owner's row")
	}
	if rows, err := notes.ListAll(theirs, crud.ListOptions{}); err != nil || len(rows) != 0 {
		t.Fatalf("another identity listed %d rows (%v), want 0", len(rows), err)
	}
	if _, err := notes.CreateOne(context.Background(), map[string]any{"title": "anonymous"}); err == nil {
		t.Fatal("a create with no identity succeeded on an owner-scoped entity")
	}

	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run after its context ended: %v", err)
	}
	got := first.rec.snapshot()
	stop := got[len(want):]
	if !slices.Equal(stop, []string{"battery.stop", "onstop"}) {
		t.Fatalf("stop order = %v, want battery.stop then onstop", stop)
	}
	if !dbClosed(t, first.data) {
		t.Fatal("OnStop did not close the installation's database")
	}

	// Rebuild from the same directory: same identity and secret, the row
	// survives, and the seed ledger keeps the entity seed from rerunning.
	second := newLocalRun(t, dir)
	if second.data.User.GetID() != first.data.User.GetID() || second.data.Secret != first.data.Secret {
		t.Fatal("the rebuilt installation minted a new identity or secret")
	}
	done = second.run(context.Background(), framework.ServicesHost())
	waitReady(t, second.app, done)
	again := handler.SetUser(context.Background(), second.data.User)
	got2, err := second.app.MustCrudHandler("notes").GetOne(again, id, nil)
	if err != nil || got2["title"] != "kept" {
		t.Fatalf("row after rebuild = %v (%v), want title kept", got2, err)
	}
	if second.rec.has("entity.seed") {
		t.Fatal("the entity seed ran again on a rebuilt installation")
	}
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	if err := second.app.Shutdown(sctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run after Shutdown: %v", err)
	}
	for range 3 {
		if err := second.app.Shutdown(sctx); err != nil {
			t.Fatalf("repeated Shutdown: %v", err)
		}
	}
	if n := second.rec.count("onstop"); n != 1 {
		t.Fatalf("OnStop ran %d times, want 1", n)
	}
}

// Every boot failure rolls back: the host never runs, its Stop does,
// started batteries stop, OnStop hooks run (closing the installation),
// and ServicesReady never closes. Host failures roll back the same way
// after a successful boot.
func TestRunRollsBackFailures(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(*localRun, *recordingHost)
		wantErr   string // "" means Run returns nil
		booted    bool   // boot succeeded, so the host ran
		started   bool   // batteries started before the failure
		wantPanic bool
	}{
		{name: "plugin init", wantErr: "init plugins", setup: func(r *localRun, _ *recordingHost) {
			r.bat.initErr = errors.New("init broke")
		}},
		{name: "migration", wantErr: "auto-migrate", setup: func(r *localRun, _ *recordingHost) {
			// SQLite resolves a view's tables lazily; a syntax error fails
			// the CREATE itself.
			r.app.View(migrate.View{Name: "broken_view", Select: "SELEC nope"})
		}},
		{name: "entity seed", wantErr: "run seeds", setup: func(r *localRun, _ *recordingHost) {
			r.bat.seedErr = errors.New("seed broke")
		}},
		{name: "seed hook", wantErr: "seed hooks", setup: func(r *localRun, _ *recordingHost) {
			r.app.WithSeed(func(context.Context) error { return errors.New("hook broke") })
		}},
		{name: "start hook", wantErr: "start hooks", started: true, setup: func(r *localRun, _ *recordingHost) {
			r.app.OnStart(func(context.Context) error { return errors.New("start broke") })
		}},
		{name: "host error", wantErr: "host broke", booted: true, started: true, setup: func(_ *localRun, h *recordingHost) {
			h.runErr = errors.New("host broke")
		}},
		{name: "host returns early", booted: true, started: true, setup: func(_ *localRun, h *recordingHost) {
			h.early = true
		}},
		{name: "host panics", booted: true, started: true, wantPanic: true, setup: func(_ *localRun, h *recordingHost) {
			h.panics = true
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newLocalRun(t, t.TempDir())
			host := newRecordingHost(r.rec)
			if !tc.booted {
				// Fail fast instead of blocking if boot wrongly succeeds.
				host.runErr = errors.New("host ran after a failed boot")
			}
			tc.setup(r, host)
			var err error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				err = r.app.Run(context.Background(), host)
			}()
			if tc.wantPanic {
				if panicked != "host exploded" {
					t.Fatalf("Run panicked with %v, want the host's own value", panicked)
				}
			} else if panicked != nil {
				t.Fatalf("Run panicked: %v", panicked)
			}
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Run: %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("Run error = %v, want one naming %q", err, tc.wantErr)
			}
			if got := r.rec.has("host.run"); got != tc.booted {
				t.Fatalf("host.Run called = %v, want %v (events %v)", got, tc.booted, r.rec.snapshot())
			}
			if readyClosed(r.app) != tc.booted {
				t.Fatalf("ServicesReady closed = %v, want %v", !tc.booted, tc.booted)
			}
			if n := r.rec.count("host.stop"); n != 1 {
				t.Fatalf("host.Stop ran %d times, want 1 (events %v)", n, r.rec.snapshot())
			}
			if got := r.rec.has("battery.start") && r.rec.has("battery.stop"); tc.started && !got {
				t.Fatalf("a started battery was not stopped (events %v)", r.rec.snapshot())
			}
			if n := r.rec.count("onstop"); n != 1 {
				t.Fatalf("OnStop ran %d times, want 1 (events %v)", n, r.rec.snapshot())
			}
			if !dbClosed(t, r.data) {
				t.Fatal("rollback left the installation's database open")
			}
			if err := r.app.Run(context.Background(), newRecordingHost(r.rec)); !errors.Is(err, framework.ErrAlreadyStarted) {
				t.Fatalf("Run after a finished run = %v, want ErrAlreadyStarted", err)
			}
		})
	}
}

// Cancelling Run's context during boot stops the run gracefully once the
// running phase returns: the host never runs, its Stop does, and Run
// returns nil.
func TestRunContextCancelDuringBoot(t *testing.T) {
	r := newLocalRun(t, t.TempDir())
	entered := make(chan struct{})
	r.app.OnStart(func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	r.app.OnStart(func(context.Context) error { r.rec.add("late.start"); return nil })
	host := newRecordingHost(r.rec)
	ctx, cancel := context.WithCancel(context.Background())
	done := r.run(ctx, host)
	<-entered
	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run after a cancel during boot: %v", err)
	}
	if r.rec.has("host.run") || r.rec.has("late.start") || readyClosed(r.app) {
		t.Fatalf("work started after the stop (events %v, ready %v)", r.rec.snapshot(), readyClosed(r.app))
	}
	for _, step := range []string{"host.stop", "battery.stop", "onstop"} {
		if r.rec.count(step) != 1 {
			t.Fatalf("%s ran %d times (events %v)", step, r.rec.count(step), r.rec.snapshot())
		}
	}
}

// A Shutdown that lands while a start hook runs waits for the hook to
// return before tearing down, so nothing is stopped under a phase still
// starting it, and nothing after that phase starts.
func TestShutdownDuringBootWaitsForThePhase(t *testing.T) {
	r := newLocalRun(t, t.TempDir())
	entered := make(chan struct{})
	r.app.OnStart(func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // still unwinding when the stop is requested
		r.rec.add("hook.exit")
		return nil
	})
	r.app.OnStart(func(context.Context) error { r.rec.add("late.start"); return nil })
	host := newRecordingHost(r.rec)
	done := r.run(context.Background(), host)
	<-entered
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.app.Shutdown(sctx); err != nil {
		t.Fatalf("Shutdown during boot: %v", err)
	}
	got := r.rec.snapshot()
	exit, stop := slices.Index(got, "hook.exit"), slices.Index(got, "battery.stop")
	if exit < 0 || stop < 0 || exit > stop {
		t.Fatalf("teardown ran under a running start hook: %v", got)
	}
	if !r.rec.has("onstop") {
		t.Fatal("Shutdown returned before the teardown finished")
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.rec.has("host.run") || r.rec.has("late.start") || readyClosed(r.app) {
		t.Fatalf("work started after the stop: %v", r.rec.snapshot())
	}
}

// Shutdown called synchronously from a boot callback cannot wait for the
// phase it is part of: it returns at its own deadline, and the run still
// tears down once the callback returns.
func TestRunReentrantShutdownIsBounded(t *testing.T) {
	r := newLocalRun(t, t.TempDir())
	inner := make(chan error, 1)
	r.app.OnStart(func(context.Context) error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		inner <- r.app.Shutdown(ctx)
		return nil
	})
	host := newRecordingHost(r.rec)
	if err := waitRun(t, r.run(context.Background(), host)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := <-inner; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the re-entrant Shutdown returned %v, want its deadline", err)
	}
	if r.rec.has("host.run") || r.rec.count("onstop") != 1 || r.rec.count("host.stop") != 1 {
		t.Fatalf("events %v", r.rec.snapshot())
	}
}

// countingListener records how often it is closed.
type countingListener struct {
	closes atomic.Int32
	done   chan struct{}
	once   sync.Once
}

func newCountingListener() *countingListener { return &countingListener{done: make(chan struct{})} }

func (l *countingListener) Accept() (net.Conn, error) { <-l.done; return nil, net.ErrClosed }
func (l *countingListener) Close() error {
	l.closes.Add(1)
	l.once.Do(func() { close(l.done) })
	return nil
}
func (l *countingListener) Addr() net.Addr { return memAddr{} }

type memAddr struct{}

func (memAddr) Network() string { return "memory" }
func (memAddr) String() string  { return "memory" }

// An App runs once. Concurrent and later Run, Start and Serve calls are
// refused with ErrAlreadyStarted (Serve still closes its listener), a
// refused host is never called, and a run that begins after Shutdown
// starts nothing.
func TestRunAdmission(t *testing.T) {
	t.Run("duplicates", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		done := r.run(context.Background(), framework.ServicesHost())
		waitReady(t, r.app, done)
		other := newRecordingHost(r.rec)
		if err := r.app.Run(context.Background(), other); !errors.Is(err, framework.ErrAlreadyStarted) {
			t.Fatalf("second Run = %v", err)
		}
		if err := r.app.Start("127.0.0.1:0"); !errors.Is(err, framework.ErrAlreadyStarted) {
			t.Fatalf("Start during Run = %v", err)
		}
		ln := newCountingListener()
		if err := r.app.Serve(ln); !errors.Is(err, framework.ErrAlreadyStarted) {
			t.Fatalf("Serve during Run = %v", err)
		}
		if n := ln.closes.Load(); n != 1 {
			t.Fatalf("refused Serve closed its listener %d times, want 1", n)
		}
		if r.rec.has("host.run") || r.rec.has("host.stop") {
			t.Fatalf("a refused host was called: %v", r.rec.snapshot())
		}
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatal(err)
		}
		if err := waitRun(t, done); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		const n = 8
		results := make(chan error, n)
		var started sync.WaitGroup
		started.Add(n)
		for range n {
			go func() {
				started.Done()
				started.Wait()
				results <- r.app.Run(context.Background(), framework.ServicesHost())
			}()
		}
		<-r.app.ServicesReady()
		refused := 0
		for range n - 1 {
			if err := waitRun(t, results); errors.Is(err, framework.ErrAlreadyStarted) {
				refused++
			}
		}
		if refused != n-1 {
			t.Fatalf("%d of %d concurrent runs were refused, want %d", refused, n, n-1)
		}
		if c := r.rec.count("onstart"); c != 1 {
			t.Fatalf("OnStart ran %d times", c)
		}
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.app.Shutdown(sctx)
		if err := waitRun(t, results); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("after shutdown", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatal(err)
		}
		host := newRecordingHost(r.rec)
		if err := r.app.Run(context.Background(), host); err != nil {
			t.Fatalf("Run after Shutdown = %v, want nil", err)
		}
		if r.rec.has("host.run") || r.rec.has("seed.hook") || r.rec.has("onstart") || readyClosed(r.app) {
			t.Fatalf("a run after Shutdown started work: %v", r.rec.snapshot())
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := r.app.Run(ctx, newRecordingHost(r.rec)); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run with a done context = %v", err)
		}
		if len(r.rec.snapshot()) != 0 {
			t.Fatalf("a refused run started work: %v", r.rec.snapshot())
		}
		// Not admitted, so the App still runs.
		done := r.run(context.Background(), framework.ServicesHost())
		waitReady(t, r.app, done)
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_ = r.app.Shutdown(sctx)
		if err := waitRun(t, done); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("nil arguments", func(t *testing.T) {
		app := framework.NewApp(framework.WithoutDefaultMiddleware())
		if err := app.Run(context.Background(), nil); err == nil {
			t.Fatal("Run(nil host) succeeded")
		}
		//nolint:staticcheck // a nil context is the misuse under test
		if err := app.Run(nil, framework.ServicesHost()); err == nil {
			t.Fatal("Run(nil ctx) succeeded")
		}
	})
}

// Shutdown is idempotent and bounded: concurrent callers share one
// teardown, and every later call returns at once.
func TestRunShutdownIsIdempotent(t *testing.T) {
	r := newLocalRun(t, t.TempDir())
	host := newRecordingHost(r.rec)
	done := r.run(context.Background(), host)
	waitReady(t, r.app, done)
	<-host.ran
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs <- r.app.Shutdown(ctx)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Shutdown: %v", err)
		}
	}
	if err := waitRun(t, done); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := r.app.Shutdown(context.Background()); err != nil || time.Since(start) > time.Second {
		t.Fatalf("a later Shutdown took %v (%v)", time.Since(start), err)
	}
	for _, step := range []string{"host.stop", "battery.stop", "onstop"} {
		if n := r.rec.count(step); n != 1 {
			t.Fatalf("%s ran %d times", step, n)
		}
	}
}

// A host that holds a transport before Run (here a listener) gets Stop
// when a stop during boot means Run is never called.
type listenerHost struct {
	ln  *countingListener
	rec *events
}

func (h *listenerHost) Run(ctx context.Context, _ *framework.Session) error {
	h.rec.add("host.run")
	<-ctx.Done()
	return nil
}

func (h *listenerHost) Stop(context.Context) error {
	h.rec.add("host.stop")
	return h.ln.Close()
}

func TestHostStopBeforeRunReleasesTransport(t *testing.T) {
	r := newLocalRun(t, t.TempDir())
	entered := make(chan struct{})
	r.app.OnStart(func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	host := &listenerHost{ln: newCountingListener(), rec: r.rec}
	done := r.run(context.Background(), host)
	<-entered
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.app.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatal(err)
	}
	if r.rec.has("host.run") || host.ln.closes.Load() != 1 {
		t.Fatalf("events %v, listener closes %d", r.rec.snapshot(), host.ln.closes.Load())
	}
}

// OnReady hooks need an HTTP listener. A host without one refuses them
// before boot, or rolls back when a plugin registers one during Init,
// rather than skipping a registered hook silently.
func TestRunRefusesOnReadyWithoutListener(t *testing.T) {
	t.Run("before boot", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		r.app.OnReady(func(string) {})
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran") // fail fast if not refused
		err := r.app.Run(context.Background(), host)
		if err == nil || !strings.Contains(err.Error(), "ServicesReady") {
			t.Fatalf("Run = %v, want a refusal naming ServicesReady", err)
		}
		if len(r.rec.snapshot()) != 0 {
			t.Fatalf("a refused run started work: %v", r.rec.snapshot())
		}
	})
	t.Run("from plugin init", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		r.bat.init = func(app *framework.App) { app.OnReady(func(string) {}) }
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran")
		err := r.app.Run(context.Background(), host)
		if err == nil || !strings.Contains(err.Error(), "OnReady") {
			t.Fatalf("Run = %v, want an OnReady refusal", err)
		}
		if r.rec.has("host.run") || r.rec.has("onstart") || r.rec.count("onstop") != 1 || r.rec.count("host.stop") != 1 {
			t.Fatalf("events %v", r.rec.snapshot())
		}
	})
	// An OnStart hook runs after the refusal that follows plugin Init, so
	// only the check after route finalization sees the hook it registers.
	t.Run("from start hook", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		r.app.OnStart(func(context.Context) error {
			r.app.OnReady(func(string) {})
			return nil
		})
		host := newRecordingHost(r.rec)
		host.runErr = errors.New("host ran")
		err := r.app.Run(context.Background(), host)
		if err == nil || !strings.Contains(err.Error(), "OnReady") {
			t.Fatalf("Run = %v, want an OnReady refusal", err)
		}
		if r.rec.has("host.run") || readyClosed(r.app) {
			t.Fatalf("the host ran or ServicesReady closed: ready %v, events %v", readyClosed(r.app), r.rec.snapshot())
		}
		if r.rec.count("battery.stop") != 1 || r.rec.count("host.stop") != 1 || r.rec.count("onstop") != 1 || !dbClosed(t, r.data) {
			t.Fatalf("the refusal did not roll back: events %v", r.rec.snapshot())
		}
	})
	t.Run("HTTP host still fires them", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		addr := make(chan string, 1)
		r.app.OnReady(func(a string) { addr <- a })
		done := make(chan error, 1)
		go func() { done <- r.app.Start("127.0.0.1:0") }()
		var got string
		select {
		case got = <-addr:
		case err := <-done:
			t.Fatalf("Start: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("OnReady never fired")
		}
		if !readyClosed(r.app) {
			t.Fatal("ServicesReady was not closed before OnReady")
		}
		resp, err := http.Get("http://" + got + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.app.Shutdown(sctx)
		if err := waitRun(t, done); err != nil {
			t.Fatal(err)
		}
	})
}

// interactiveSetup is a first-run setup that cannot run headless.
type interactiveSetup struct{}

func (interactiveSetup) Incomplete(context.Context) (bool, error)     { return true, nil }
func (interactiveSetup) CanRunHeadless(context.Context) (bool, error) { return false, nil }
func (interactiveSetup) RunSteps(context.Context) error               { return nil }
func (interactiveSetup) Handler(func(), http.HandlerFunc, http.HandlerFunc) http.Handler {
	return http.NotFoundHandler()
}
func (interactiveSetup) SetupURL(string, string) string { return "" }

// The setup wizard is served over HTTP, so a host without a listener
// rolls back instead of waiting forever for a wizard nobody can reach.
func TestRunRefusesInteractiveSetupWithoutListener(t *testing.T) {
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(interactiveSetup{}))
	host := newRecordingHost(r.rec)
	host.runErr = errors.New("host ran")
	err := r.app.Run(context.Background(), host)
	if err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatalf("Run = %v, want a setup refusal", err)
	}
	if r.rec.has("host.run") || r.rec.has("onstart") || r.rec.count("onstop") != 1 {
		t.Fatalf("events %v", r.rec.snapshot())
	}
}

// The configuration checks Start and Serve make before boot hold under
// every host. An EntityEndpoints queue stays in place once a registration
// claims it, so a claimed queue boots and an unclaimed one is refused; an
// OAuth protected-resource config naming a non-HTTPS URL is refused.
func TestRunChecksConfigurationBeforeBoot(t *testing.T) {
	revoke := framework.Endpoint{Method: http.MethodPost, Path: "{id}/revoke", Public: true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	t.Run("claimed endpoints boot", func(t *testing.T) {
		r := newLocalRun(t, t.TempDir())
		r.app.EntityEndpoints("licenses", revoke)
		r.app.Entity("licenses", framework.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}})
		ctx, cancel := context.WithCancel(context.Background())
		done := r.run(ctx, framework.ServicesHost())
		waitReady(t, r.app, done)
		rec := httptest.NewRecorder()
		r.app.InProcessHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/licenses/abc/revoke", nil))
		cancel()
		if err := waitRun(t, done); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("queued endpoint in-process = %d, want 204", rec.Code)
		}
	})
	for _, refused := range []struct {
		name, want string
		opts       []framework.AppOption
		queue      bool
	}{
		{name: "unclaimed endpoints", want: "licences", queue: true},
		{name: "invalid OAuth resource", want: "OAuth protected resource", opts: []framework.AppOption{
			framework.WithOAuthProtectedResource(framework.OAuthProtectedResourceConfig{Resource: "http://api.example.com"}),
		}},
	} {
		t.Run(refused.name, func(t *testing.T) {
			r := newLocalRun(t, t.TempDir(), refused.opts...)
			if refused.queue {
				r.app.EntityEndpoints("licences", revoke)
			}
			host := newRecordingHost(r.rec)
			host.runErr = errors.New("host ran")
			err := r.app.Run(context.Background(), host)
			if err == nil || !strings.Contains(err.Error(), refused.want) {
				t.Fatalf("Run = %v, want a refusal naming %q", err, refused.want)
			}
			if len(r.rec.snapshot()) != 0 {
				t.Fatalf("a refused run started work: %v", r.rec.snapshot())
			}
		})
	}
}

// wizardSetup is an interactive first-run setup whose wizard completes
// on a request to /finish.
type wizardSetup struct{ done atomic.Bool }

func (w *wizardSetup) Incomplete(context.Context) (bool, error)   { return !w.done.Load(), nil }
func (*wizardSetup) CanRunHeadless(context.Context) (bool, error) { return false, nil }
func (*wizardSetup) RunSteps(context.Context) error               { return nil }
func (w *wizardSetup) Handler(swap func(), _, _ http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/finish" {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.done.Store(true)
		swap()
		rw.WriteHeader(http.StatusNoContent)
	})
}
func (*wizardSetup) SetupURL(_, addr string) string { return "http://" + addr + "/" }

// Under the HTTP host, interactive setup defers the battery and OnStart
// hooks until the wizard completes, and ServicesReady waits with them:
// the transport is ready (OnReady fired) before the services are.
func TestStartInteractiveSetupDefersServicesReady(t *testing.T) {
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(&wizardSetup{}))
	base, done := startSetupRun(t, r)
	if readyClosed(r.app) || r.rec.has("battery.start") || r.rec.has("onstart") {
		t.Fatalf("services ready before setup completed: ready %v, events %v", readyClosed(r.app), r.rec.snapshot())
	}
	resp, err := http.Get(base + "/finish")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case <-r.app.ServicesReady():
	case <-time.After(5 * time.Second):
		t.Fatal("ServicesReady did not close once setup completed")
	}
	if !r.rec.has("battery.start") || !r.rec.has("onstart") {
		t.Fatalf("deferred hooks did not run: %v", r.rec.snapshot())
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.app.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatal(err)
	}
}

// startSetupRun starts r's App on a loopback port and returns its base
// URL once the listener is bound, with Start's result channel.
func startSetupRun(t *testing.T, r *localRun) (string, chan error) {
	t.Helper()
	addr := make(chan string, 1)
	r.app.OnReady(func(a string) { addr <- a })
	done := make(chan error, 1)
	go func() { done <- r.app.Start("127.0.0.1:0") }()
	select {
	case a := <-addr:
		return "http://" + a, done
	case err := <-done:
		t.Fatalf("Start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("OnReady never fired")
	}
	return "", nil
}

// externalSwapSetup is an interactive first-run setup whose runner calls
// swap itself, outside any request, as a runner that finishes setup in
// the background would. Handler hands the swap to the test.
type externalSwapSetup struct{ swap chan func() }

func newExternalSwapSetup() *externalSwapSetup {
	return &externalSwapSetup{swap: make(chan func(), 1)}
}

func (*externalSwapSetup) Incomplete(context.Context) (bool, error)     { return true, nil }
func (*externalSwapSetup) CanRunHeadless(context.Context) (bool, error) { return false, nil }
func (*externalSwapSetup) RunSteps(context.Context) error               { return nil }
func (s *externalSwapSetup) Handler(swap func(), _, _ http.HandlerFunc) http.Handler {
	s.swap <- swap
	return http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	})
}
func (*externalSwapSetup) SetupURL(string, string) string { return "" }

// gatedBattery's OnStart blocks until release closes and ignores its
// context, as a battery stuck dialing a dependency does.
type gatedBattery struct {
	rec     *events
	entered chan struct{}
	release chan struct{}
}

func newGatedBattery(rec *events) *gatedBattery {
	return &gatedBattery{rec: rec, entered: make(chan struct{}), release: make(chan struct{})}
}

func (*gatedBattery) Name() string              { return "gated-test" }
func (*gatedBattery) Init(*framework.App) error { return nil }
func (b *gatedBattery) OnStart(context.Context) error {
	close(b.entered)
	<-b.release
	b.rec.add("gated.started")
	return nil
}
func (b *gatedBattery) OnStop(context.Context) error { b.rec.add("gated.stop"); return nil }

// finishTeardown waits for the teardown under way: a later Shutdown
// returns nil once it has finished.
func finishTeardown(t *testing.T, app *framework.App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("waiting for the teardown: %v", err)
	}
}

// before reports whether a was recorded, and recorded before b.
func before(got []string, a, b string) bool {
	i, j := slices.Index(got, a), slices.Index(got, b)
	return i >= 0 && j >= 0 && i < j
}

// A Shutdown while the OnStart hooks interactive setup deferred are
// running treats them as a boot phase: it cancels the lifecycle context
// and waits for them, bounded by its own deadline, and the teardown (the
// HTTP drain included) runs once they return, never under them. The
// swap here runs inside the wizard's request, so a teardown begun at
// once would wait out the HTTP drain on that request, give up, and stop
// the batteries under the running hook.
func TestShutdownDuringDeferredStartWaitsForTheHooks(t *testing.T) {
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(&wizardSetup{}))
	entered, release := make(chan struct{}), make(chan struct{})
	r.app.OnStart(func(context.Context) error {
		close(entered)
		<-release // ignores the cancelled context
		r.rec.add("hook.exit")
		return nil
	})
	r.app.OnStart(func(context.Context) error { r.rec.add("late.start"); return nil })
	base, done := startSetupRun(t, r)
	finished := make(chan int, 1)
	go func() {
		resp, err := http.Get(base + "/finish")
		if err != nil {
			finished <- 0
			return
		}
		resp.Body.Close()
		finished <- resp.StatusCode
	}()
	<-entered

	sctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := r.app.Shutdown(sctx)
	for _, step := range []string{"battery.stop", "onstop"} {
		if r.rec.has(step) {
			t.Fatalf("%s ran while a deferred OnStart hook was still running: %v", step, r.rec.snapshot())
		}
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("Shutdown under a blocked deferred hook = %v, want its own deadline", err)
	}
	// The teardown waits, but the stop is public at once: a balancer
	// stops routing here while the hook unwinds.
	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d during a pending Shutdown, want 503", resp.StatusCode)
	}
	close(release)
	finishTeardown(t, r.app)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if code := <-finished; code != http.StatusNoContent {
		t.Fatalf("the wizard's request ended with %d, want 204: the drain cut it", code)
	}
	got := r.rec.snapshot()
	if !before(got, "hook.exit", "battery.stop") || r.rec.count("battery.stop") != 1 || r.rec.count("onstop") != 1 {
		t.Fatalf("teardown ran under the deferred hook or more than once: %v", got)
	}
	if r.rec.has("late.start") || readyClosed(r.app) {
		t.Fatalf("work started after the stop (events %v, ready %v)", got, readyClosed(r.app))
	}
}

// A SetupRunner may call swap from a goroutine of its own, outside any
// request the HTTP drain waits for. A Shutdown while that swap's battery
// startup blocks still stops nothing under it, returns at its deadline,
// and leaves the teardown to run once the battery's OnStart returns.
func TestShutdownDuringDeferredBatteryStartFromExternalSwap(t *testing.T) {
	setup := newExternalSwapSetup()
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(setup))
	bat := newGatedBattery(r.rec)
	r.app.RegisterBattery(bat)
	_, done := startSetupRun(t, r)
	swap := <-setup.swap
	go swap()
	<-bat.entered

	sctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := r.app.Shutdown(sctx)
	for _, step := range []string{"gated.stop", "battery.stop", "onstop"} {
		if r.rec.has(step) {
			t.Fatalf("%s ran while a battery was still starting: %v", step, r.rec.snapshot())
		}
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("Shutdown under a blocked battery start = %v, want its own deadline", err)
	}
	close(bat.release)
	finishTeardown(t, r.app)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := r.rec.snapshot()
	if !before(got, "gated.started", "gated.stop") || r.rec.count("gated.stop") != 1 || r.rec.count("onstop") != 1 {
		t.Fatalf("teardown ran under the battery start or more than once: %v", got)
	}
	if r.rec.has("onstart") || readyClosed(r.app) {
		t.Fatalf("work started after the stop (events %v, ready %v)", got, readyClosed(r.app))
	}
}

// A swap that arrives after Shutdown began, from a wizard request the
// drain is finishing or a runner's own goroutine, starts nothing: no
// battery, no OnStart hook, no ServicesReady, and no second teardown.
func TestLateSetupSwapAfterShutdownStartsNothing(t *testing.T) {
	setup := newExternalSwapSetup()
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(setup))
	_, done := startSetupRun(t, r)
	swap := <-setup.swap
	finishTeardown(t, r.app)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	swap()
	if r.rec.has("battery.start") || r.rec.has("onstart") || readyClosed(r.app) {
		t.Fatalf("a swap after Shutdown started work (events %v, ready %v)", r.rec.snapshot(), readyClosed(r.app))
	}
	if r.rec.count("battery.stop") != 1 || r.rec.count("onstop") != 1 {
		t.Fatalf("the teardown did not run exactly once: %v", r.rec.snapshot())
	}
}

// A deferred OnStart hook that calls Shutdown synchronously gets the
// same answer as a boot callback: Shutdown returns at its own deadline
// with nothing torn down under the hook, and the teardown runs once the
// hook returns.
func TestDeferredHookReentrantShutdownIsBounded(t *testing.T) {
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(&wizardSetup{}))
	inner := make(chan error, 1)
	tornDown := make(chan bool, 1)
	r.app.OnStart(func(context.Context) error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		inner <- r.app.Shutdown(ctx)
		tornDown <- r.rec.has("battery.stop") || r.rec.has("onstop")
		return nil
	})
	base, done := startSetupRun(t, r)
	resp, err := http.Get(base + "/finish")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := <-inner; err != context.DeadlineExceeded {
		t.Fatalf("the re-entrant Shutdown returned %v, want its deadline", err)
	}
	if <-tornDown {
		t.Fatalf("the teardown ran under the hook that called Shutdown: %v", r.rec.snapshot())
	}
	finishTeardown(t, r.app)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if r.rec.count("battery.stop") != 1 || r.rec.count("onstop") != 1 || readyClosed(r.app) {
		t.Fatalf("events %v, ready %v", r.rec.snapshot(), readyClosed(r.app))
	}
}

// breakingListener accepts nothing until broken; then Accept fails the
// way a listener whose socket died does, which ends Serve with an error.
// accepting closes on the first Accept: the host is serving.
type breakingListener struct {
	broken, closed, accepting       chan struct{}
	breakOnce, closeOnce, serveOnce sync.Once
}

var errListenerBroke = errors.New("listener broke")

func newBreakingListener() *breakingListener {
	return &breakingListener{broken: make(chan struct{}), closed: make(chan struct{}), accepting: make(chan struct{})}
}

func (l *breakingListener) Accept() (net.Conn, error) {
	l.serveOnce.Do(func() { close(l.accepting) })
	select {
	case <-l.broken:
		return nil, errListenerBroke
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *breakingListener) Close() error   { l.closeOnce.Do(func() { close(l.closed) }); return nil }
func (l *breakingListener) Addr() net.Addr { return memAddr{} }
func (l *breakingListener) breakNow()      { l.breakOnce.Do(func() { close(l.broken) }) }

// A host that fails while the deferred start runs ends the run with its
// error, but its rollback does not tear down under the battery still
// starting: the teardown a pending Shutdown left to the deferred start
// stays with it, and runs once the battery's OnStart returns.
func TestHostFailureDuringDeferredStartWaitsForIt(t *testing.T) {
	setup := newExternalSwapSetup()
	r := newLocalRun(t, t.TempDir(), framework.WithSetup(setup))
	bat := newGatedBattery(r.rec)
	r.app.RegisterBattery(bat)
	ln := newBreakingListener()
	done := make(chan error, 1)
	go func() { done <- r.app.Serve(ln) }()
	swap := <-setup.swap
	go swap()
	<-bat.entered
	// The swap reaches the test while the host builds its handler, before
	// it adopts the server. A Shutdown in that window makes Run return nil
	// without serving, so wait until the host accepts.
	<-ln.accepting

	sctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = r.app.Shutdown(sctx) // leaves the teardown to the deferred start
	ln.breakNow()
	<-ln.closed // the host returned
	time.Sleep(100 * time.Millisecond)
	for _, step := range []string{"gated.stop", "battery.stop", "onstop"} {
		if r.rec.has(step) {
			t.Fatalf("%s ran while a battery was still starting: %v", step, r.rec.snapshot())
		}
	}
	close(bat.release)
	if err := waitRun(t, done); !errors.Is(err, errListenerBroke) {
		t.Fatalf("Serve = %v, want the host's failure", err)
	}
	finishTeardown(t, r.app)
	got := r.rec.snapshot()
	if !before(got, "gated.started", "gated.stop") || r.rec.count("gated.stop") != 1 || r.rec.count("onstop") != 1 {
		t.Fatalf("teardown ran under the battery start or more than once: %v", got)
	}
}

// Shutdown racing Run at arbitrary points keeps the lifecycle's
// invariants: Run returns nil, the teardown runs once, a started battery
// is stopped, an admitted host is stopped once whether or not it ran, and
// a host runs only after ServicesReady closed.
func TestRunShutdownRaceInvariants(t *testing.T) {
	for i := range 40 {
		r := newLocalRun(t, t.TempDir())
		host := newRecordingHost(r.rec)
		var readyBeforeRun atomic.Bool
		ready := r.app.ServicesReady()
		go func() {
			<-host.ran
			select {
			case <-ready:
				readyBeforeRun.Store(true)
			default:
			}
		}()
		done := r.run(context.Background(), host)
		time.Sleep(time.Duration(i%8) * 500 * time.Microsecond)
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.app.Shutdown(sctx); err != nil {
			t.Fatalf("iteration %d: Shutdown: %v", i, err)
		}
		cancel()
		if err := waitRun(t, done); err != nil {
			t.Fatalf("iteration %d: Run: %v", i, err)
		}
		ev := r.rec.snapshot()
		// A Shutdown that wins admission leaves the host uncalled; once a
		// boot phase or the host ran, the run was admitted.
		stops := r.rec.count("host.stop")
		admitted := r.rec.has("seed.hook") || r.rec.has("host.run")
		if r.rec.count("onstop") != 1 || stops > 1 || admitted && stops != 1 {
			t.Fatalf("iteration %d: events %v", i, ev)
		}
		if r.rec.has("battery.start") && !r.rec.has("battery.stop") {
			t.Fatalf("iteration %d: started battery never stopped: %v", i, ev)
		}
		if r.rec.has("host.run") {
			<-host.ran
			deadline := time.Now().Add(time.Second)
			for !readyBeforeRun.Load() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !readyBeforeRun.Load() {
				t.Fatalf("iteration %d: the host ran before ServicesReady closed", i)
			}
		}
		if !dbClosed(t, r.data) {
			t.Fatalf("iteration %d: database left open", i)
		}
	}
}
