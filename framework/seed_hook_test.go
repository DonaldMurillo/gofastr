package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestWithSeed_RunsAfterMigrate asserts that a func registered via
// App.WithSeed runs AFTER auto-migration, so it can INSERT into a table
// that Start() just created — the "no such table" footgun.
func TestWithSeed_RunsAfterMigrate(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("widgets", entity.EntityConfig{
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	ran := false
	app.WithSeed(func(ctx context.Context) error {
		ran = true
		_, err := db.ExecContext(ctx, "INSERT INTO widgets (id, name) VALUES ('w1', 'gear')")
		return err
	})

	// Drive the post-migrate phase the way Start does, without binding a port.
	if err := runStartPhasesForTest(app); err != nil {
		t.Fatalf("start phases: %v", err)
	}
	if !ran {
		t.Fatal("WithSeed func did not run")
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&n); err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	if n != 1 {
		t.Fatalf("widgets rows: got %d, want 1", n)
	}
}

// runStartPhasesForTest exercises the migrate→seed→start-hooks chain that
// App.Start runs before it binds the HTTP port, so the seed hook can be
// asserted without a live listener.
func runStartPhasesForTest(a *App) error {
	a.ensureLifecycleContext()
	if a.DB != nil {
		if err := AutoMigrate(a.DB, a.Registry); err != nil {
			return err
		}
	}
	if err := a.runSeedHooks(); err != nil {
		return err
	}
	if err := a.InitPlugins(); err != nil {
		return err
	}
	return a.runStartHooks()
}

// TestRunSeedHooksSerialized_SQLiteUnlocked covers the non-Postgres branch of
// runSeedHooksSerialized: with a SQLite (or nil) DB there is no advisory lock,
// so the hooks run unlocked. Fast (no container).
func TestRunSeedHooksSerialized_SQLiteUnlocked(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ran := false
	a := NewApp(WithDB(db))
	a.WithSeed(func(context.Context) error { ran = true; return nil })
	if err := a.runSeedHooksSerialized(); err != nil {
		t.Fatalf("serialized (sqlite): %v", err)
	}
	if !ran {
		t.Fatal("seed hook did not run on SQLite")
	}
}
