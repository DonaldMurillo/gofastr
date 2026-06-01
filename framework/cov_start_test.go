package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// covStartApp boots an app with a DB entity + debug endpoints on an
// ephemeral port and returns a stop func. Exercises the full Start path:
// auto-migrate, seeds, InitPlugins, start hooks, OpenAPI/LLM.md mount,
// debug + health endpoints, banner, and the server bind.
func covStartAndStop(t *testing.T, app *App) func() {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv != nil {
			return func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = app.Shutdown(ctx)
				<-done
			}
		}
		select {
		case err := <-done:
			t.Fatalf("Start returned early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for server")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Full Start with a DB-backed entity and debug endpoints enabled.
func TestCovFullStartWithDBAndDebug(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(
			WithDB(db),
			WithConfig(AppConfig{Name: "cov-start", DebugEndpoints: true}),
		)
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))

		var startHookRan bool
		app.OnStart(func(context.Context) error { startHookRan = true; return nil })

		stop := covStartAndStop(t, app)
		defer stop()

		if !startHookRan {
			t.Fatal("OnStart hook did not run during Start")
		}
		// /openapi.json route is mounted (auth-gated → 401 unauthenticated,
		// not 404). A 404 would mean the spec route was never registered.
		resp := TestHarness(t, app).Get("/openapi.json")
		if resp.Status() == 404 {
			t.Fatalf("openapi.json not mounted (status %d)", resp.Status())
		}
	})
}

// Start aborts when a start hook fails (covers runStartHooks error +
// abort path). No port is bound.
func TestCovStartAbortsOnStartHookError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.OnStart(func(context.Context) error { return errStored })
		if err := app.Start("127.0.0.1:0"); err == nil {
			t.Fatal("expected Start to fail from the OnStart hook")
		}
	})
}

// Start aborts when AutoMigrate fails (app.go line 1110). A closed DB makes
// the migration phase error before any port is bound.
func TestCovStartAutoMigrateError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		_ = db.Close() // force AutoMigrate to fail
		if err := app.Start("127.0.0.1:0"); err == nil {
			t.Fatal("expected Start to fail when AutoMigrate errors on a closed DB")
		}
	})
}

// Start aborts when RunSeeds fails (app.go line 1113). A Seed func that
// errors fails the seed phase after a successful AutoMigrate.
func TestCovStartRunSeedsError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Seed: func(context.Context, *sql.DB) error {
				return errStored
			},
		}.WithTimestamps(false))
		if err := app.Start("127.0.0.1:0"); err == nil {
			t.Fatal("expected Start to fail from a failing Seed func")
		}
	})
}

// runStartHooks returns the battery StartAll error (app.go line 1061), and
// Start propagates it as a start-hooks abort.
func TestCovStartBatteryStartError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterBattery(&mockBattery{name: "bad-start", startErr: errStored})
	if err := app.Start("127.0.0.1:0"); err == nil {
		t.Fatal("expected Start to fail from battery OnStart error")
	}
}

// Shutdown surfaces the battery StopAll error (app.go line 1039) and an
// OnStop drainer error (line 1046).
func TestCovShutdownBatteryStopError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterBattery(&mockBattery{name: "bad-stop", stopErr: errStored})
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	// StopAll iterates sorted batteries; ensure it's resolved.
	if err := app.Batteries.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(context.Background()); err == nil {
		t.Fatal("expected Shutdown to surface the battery stop error")
	}
}

func TestCovShutdownDrainerError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.OnStop(func() error { return errStored })
	if err := app.Shutdown(context.Background()); err == nil {
		t.Fatal("expected Shutdown to surface the OnStop drainer error")
	}
}

// ============================================================================
// app.go — Events / HookRegistry lazy creation on a bare App
// ============================================================================

func TestCovEventsLazyCreate(t *testing.T) {
	a := &App{}
	if a.Events() == nil {
		t.Fatal("Events() should lazily create a bus")
	}
	// second call returns the same (covers the non-nil short-circuit too)
	if a.Events() == nil {
		t.Fatal("Events() second call nil")
	}
}

func TestCovHookRegistryLazyCreate(t *testing.T) {
	a := &App{}
	hr := a.HookRegistry("x")
	if hr == nil {
		t.Fatal("HookRegistry should lazily create")
	}
	hr.RegisterHook(hook.BeforeCreate, func(context.Context, any) error { return nil })
	if a.HookRegistry("x") != hr {
		t.Fatal("HookRegistry should return the same registry for the same name")
	}
}
