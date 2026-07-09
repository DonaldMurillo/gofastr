package framework

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/cron"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/outbox"
)

// ──────────────────────────────────────────────────────────────────────
// Role resolution
// ──────────────────────────────────────────────────────────────────────

// A freshly built app with no role option and no GOFASTR_ROLE env defaults
// to RoleAll — today's behavior, so existing apps are untouched.
func TestRole_DefaultIsAll(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "") // force unset regardless of runner env
	app := NewApp(WithoutDefaultMiddleware())
	if app.Role() != RoleAll {
		t.Fatalf("default role: got %q want %q", app.Role(), RoleAll)
	}
}

// GOFASTR_ROLE selects the role at deploy time (case-insensitive).
func TestRole_EnvSetsRole(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "SERVE")
	app := NewApp(WithoutDefaultMiddleware())
	if app.Role() != RoleServe {
		t.Fatalf("env role: got %q want %q", app.Role(), RoleServe)
	}
}

// An explicit WithRole wins over GOFASTR_ROLE.
func TestRole_WithRoleBeatsEnv(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "serve")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleWorker))
	if app.Role() != RoleWorker {
		t.Fatalf("WithRole should beat env: got %q want %q", app.Role(), RoleWorker)
	}
}

// An unknown WithRole value panics in NewApp — fail loud, never silently
// fall back to all (a typo'd role would run the wrong workload).
func TestRole_BadOptionPanics(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("WithRole(bogus) should panic in NewApp")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "invalid role") {
			t.Fatalf("panic message should mention invalid role, got: %v", r)
		}
	}()
	NewApp(WithoutDefaultMiddleware(), WithRole(Role("bogus")))
}

// A bogus GOFASTR_ROLE value fails loudly too — never a silent fallback.
func TestRole_BadEnvPanics(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "supercomputer")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("bogus GOFASTR_ROLE should panic in NewApp")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "GOFASTR_ROLE") {
			t.Fatalf("panic message should mention GOFASTR_ROLE, got: %v", r)
		}
	}()
	NewApp(WithoutDefaultMiddleware())
}

// ──────────────────────────────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────────────────────────────

// fakeStartStop satisfies the schedulerStartStop interface AddQueue takes.
// It records whether Start/Close ran so tests can assert the worker-scoped
// gate fired (or didn't) for a given role.
type fakeStartStop struct {
	starts atomic.Int32
	closes atomic.Int32
}

func (f *fakeStartStop) Start(context.Context) { f.starts.Add(1) }
func (f *fakeStartStop) Close() error          { f.closes.Add(1); return nil }

// startOnRandomPort runs app.Start on :0, waits for OnReady, and returns the
// bound address plus a stop function. Signal handling is disabled so
// concurrent tests don't fight over the process signal mask. stop is
// idempotent (sync.Once) and registered via t.Cleanup; tests that assert
// post-shutdown state call it explicitly first.
func startOnRandomPort(t *testing.T, app *App) (addr string, stop func()) {
	t.Helper()
	app.Config.DisableSignalHandling = true

	ready := make(chan string, 1)
	app.OnReady(func(a string) { ready <- a })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()

	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	var once sync.Once
	stop = func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := app.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Start did not return after Shutdown")
			}
		})
	}
	t.Cleanup(stop)
	return addr, stop
}

func sqliteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// postsEntity builds an EntityConfig matching the outbox wiring tests.
func postsEntity() entity.EntityConfig {
	return entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false)
}

// get is a tiny helper for HTTP assertions against a live server.
func get(t *testing.T, addr, path string) *http.Response {
	t.Helper()
	resp, err := http.Get("http://" + addr + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// ──────────────────────────────────────────────────────────────────────
// Serve role — background consumers do NOT start
// ──────────────────────────────────────────────────────────────────────

// In RoleServe, AddQueue's worker does not start, and Close is never called
// on shutdown — a serve-only process never owns a queue it would drain.
func TestServeRole_QueueDoesNotStart(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleServe))
	f := &fakeStartStop{}
	app.AddQueue(f)

	_, stop := startOnRandomPort(t, app)
	stop() // explicit so the Close assertion sees post-shutdown state

	if f.starts.Load() != 0 {
		t.Fatalf("serve role: queue Start called %d times, want 0", f.starts.Load())
	}
	if f.closes.Load() != 0 {
		t.Fatalf("serve role: queue Close called %d times, want 0", f.closes.Load())
	}
}

// In RoleServe the outbox relay never starts: a staged row is never
// delivered. This is the cheapest observable that the relay block in Start
// is gated.
func TestServeRole_OutboxRelaySkipped(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	db := sqliteDB(t)
	var delivered atomic.Int32
	app := NewApp(
		WithDB(db),
		WithoutDefaultMiddleware(),
		WithRole(RoleServe),
		WithOutbox(outbox.WithHandlerGrace(0), outbox.WithPollInterval(50*time.Millisecond)),
		WithOutboxConsumer("witness", "test.created", func(_ context.Context, _ event.Event) error {
			delivered.Add(1)
			return nil
		}),
	)

	startOnRandomPort(t, app)

	// Stage a pending row directly. If the relay were running it would claim
	// and deliver within ~pollInterval; here it must stay pending.
	ctx := context.Background()
	if _, err := app.Outbox().Append(ctx, db, "test.created", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // 6x the poll interval — enough to be sure

	if got := delivered.Load(); got != 0 {
		t.Fatalf("serve role: outbox delivered %d rows, want 0 (relay should not start)", got)
	}
}

// In RoleServe, a cron scheduler is not started. Cron shares the same
// onWorkerStart gate as AddQueue (proven by TestServeRole_QueueDoesNotStart);
// this test confirms AddCron + serve Start+Shutdown is clean — no hang from
// a drain waiting on a scheduler that was never started.
func TestServeRole_CronCleanLifecycle(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleServe))
	s := cron.NewScheduler()
	if err := s.Register(cron.CronJob{Name: "noop", Spec: "* * * * *", Run: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("register cron job: %v", err)
	}
	app.AddCron(s)

	startOnRandomPort(t, app) // cleanup Shutdown must not hang
}

// A serve-only process still serves the full app router (health + entity
// CRUD). Guards against an accidental over-narrowing of the serve surface.
func TestServeRole_ServesFullRouter(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	db := sqliteDB(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithRole(RoleServe))
	app.Entity("posts", postsEntity())
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, "/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz: got %d want 200", resp.StatusCode)
	}
	// The distinguishing route: /healthz is served by BOTH surfaces, so only
	// an entity route proves the serve role got the full router and not the
	// worker's health-only mux.
	resp2 := get(t, addr, "/posts")
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusNotFound {
		t.Fatal("/posts on serve role: got 404 — serve surface over-narrowed to the worker mux")
	}
}

// ──────────────────────────────────────────────────────────────────────
// Worker role — health surface only, consumers run
// ──────────────────────────────────────────────────────────────────────

// In RoleWorker, /healthz and /readyz respond on the bound address.
func TestWorkerRole_HealthResponds(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleWorker))
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, "/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz: got %d want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("/healthz body: got %q want %q", string(body), "ok")
	}

	resp2 := get(t, addr, "/readyz")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/readyz: got %d want 200 (no checks → ready)", resp2.StatusCode)
	}
}

// In RoleWorker, an entity CRUD route is NOT served — the worker binds addr
// but serves only the health surface, never the app router.
func TestWorkerRole_EntityRouteNotServed(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	db := sqliteDB(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithRole(RoleWorker))
	app.Entity("posts", postsEntity())
	addr, _ := startOnRandomPort(t, app)

	resp := get(t, addr, "/posts")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/posts on worker: got %d want 404 (entity routes must not be served)", resp.StatusCode)
	}
	// OpenAPI and well-known discovery must also be absent.
	for _, p := range []string{"/openapi.json", "/.well-known/agent-skills/index.json"} {
		r := get(t, addr, p)
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("%s on worker: got %d want 404", p, r.StatusCode)
		}
	}
}

// In RoleWorker, AddQueue's worker DOES start and Close runs on shutdown.
func TestWorkerRole_QueueStarts(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleWorker))
	f := &fakeStartStop{}
	app.AddQueue(f)

	_, stop := startOnRandomPort(t, app)
	stop() // explicit so the Close assertion sees post-shutdown state

	if f.starts.Load() != 1 {
		t.Fatalf("worker role: queue Start called %d times, want 1", f.starts.Load())
	}
	if f.closes.Load() != 1 {
		t.Fatalf("worker role: queue Close called %d times, want 1", f.closes.Load())
	}
}

// In RoleWorker the outbox relay runs: a staged row is delivered to the
// declared consumer.
func TestWorkerRole_OutboxRelayRuns(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	db := sqliteDB(t)
	var delivered atomic.Int32
	app := NewApp(
		WithDB(db),
		WithoutDefaultMiddleware(),
		WithRole(RoleWorker),
		WithOutbox(outbox.WithHandlerGrace(0), outbox.WithPollInterval(50*time.Millisecond)),
		WithOutboxConsumer("witness", "test.created", func(_ context.Context, _ event.Event) error {
			delivered.Add(1)
			return nil
		}),
	)

	startOnRandomPort(t, app)

	ctx := context.Background()
	if _, err := app.Outbox().Append(ctx, db, "test.created", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Poll for delivery (relay claims on its poll interval).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && delivered.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := delivered.Load(); got != 1 {
		t.Fatalf("worker role: outbox delivered %d rows, want 1", got)
	}
}

// In RoleWorker, a cron scheduler is started by the app and the job is
// reachable. We confirm wiring via RunOnce (the loop aligns to a minute
// boundary, too slow for a unit test); the start gate itself is the shared
// onWorkerStart mechanism proven by the queue tests.
func TestWorkerRole_CronWired(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleWorker))

	fired := make(chan struct{}, 1)
	s := cron.NewScheduler()
	if err := s.Register(cron.CronJob{
		Name: "ping",
		Spec: "* * * * *",
		Run: func(context.Context) error {
			select {
			case fired <- struct{}{}:
			default:
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("register cron job: %v", err)
	}
	app.AddCron(s)

	startOnRandomPort(t, app)

	// The job is wired on the scheduler (RunOnce fires it); AddCron didn't
	// corrupt registration.
	s.RunOnce(context.Background(), time.Now())
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("cron job did not fire via RunOnce — AddCron wiring broken")
	}
}

// ──────────────────────────────────────────────────────────────────────
// All role — everything starts (regression guard)
// ──────────────────────────────────────────────────────────────────────

// In RoleAll, AddQueue's worker starts AND the full router serves — exactly
// today's behavior, so existing apps are untouched.
func TestAllRole_QueueStartsAndRouterServes(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleAll))
	f := &fakeStartStop{}
	app.AddQueue(f)
	addr, _ := startOnRandomPort(t, app)

	if f.starts.Load() != 1 {
		t.Fatalf("all role: queue Start called %d times, want 1", f.starts.Load())
	}
	resp := get(t, addr, "/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("all role /healthz: got %d want 200", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Shutdown cleanliness per role
// ──────────────────────────────────────────────────────────────────────

// A serve-only Shutdown returns no error and does not call Close on the
// never-started queue (no Start was ever called either).
func TestServeRole_ShutdownClean(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleServe))
	f := &fakeStartStop{}
	app.AddQueue(f)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("serve Shutdown returned error: %v", err)
	}
	if f.starts.Load() != 0 || f.closes.Load() != 0 {
		t.Fatalf("serve shutdown: starts=%d closes=%d, want 0/0", f.starts.Load(), f.closes.Load())
	}
}

// A worker Shutdown closes the started queue cleanly.
func TestWorkerRole_ShutdownClosesQueue(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware(), WithRole(RoleWorker))
	f := &fakeStartStop{}
	app.AddQueue(f)

	startOnRandomPort(t, app) // starts the queue; cleanup Shutdown closes it

	// Force Shutdown now so the assertion sees post-shutdown state. stop() is
	// idempotent, so the cleanup re-call is a safe no-op.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("worker Shutdown returned error: %v", err)
	}
	if f.closes.Load() != 1 {
		t.Fatalf("worker shutdown: queue Close called %d times, want 1", f.closes.Load())
	}
}
