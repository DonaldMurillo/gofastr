package framework

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

//go:embed testdata/seed_widgets.json
var seedFixtures embed.FS

func newSeedRegistry(t *testing.T, ents ...*entity.Entity) entity.Registry {
	t.Helper()
	reg := NewRegistry()
	for _, e := range ents {
		if err := reg.Register(e); err != nil {
			t.Fatalf("register %s: %v", e.GetName(), err)
		}
	}
	return reg
}

// TestRunSeeds_FiresOnceAndShortCircuits exercises the core contract: the
// Seed callback runs exactly once across multiple RunSeeds calls because the
// _gofastr_seeded ledger records its name on success.
func TestRunSeeds_FiresOnceAndShortCircuits(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	calls := 0
	cfg := entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			calls++
			_, err := db.ExecContext(ctx, "INSERT INTO seeded_things (id, name) VALUES ('1', 'ok')")
			return err
		},
	}
	ent := entity.Define("seeded_things", cfg)
	reg := newSeedRegistry(t, ent)

	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := migrate.RunSeeds(context.Background(), db, reg); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("Seed called %d times, want 1", calls)
	}

	var ledgered int
	if err := db.QueryRow("SELECT COUNT(*) FROM _gofastr_seeded WHERE entity_name='seeded_things'").Scan(&ledgered); err != nil {
		t.Fatal(err)
	}
	if ledgered != 1 {
		t.Errorf("ledger row count = %d, want 1", ledgered)
	}
}

// TestRunSeeds_ErrorAbortsAndLeavesLedgerEmpty: if Seed returns an error,
// the ledger row must NOT be inserted, so the next RunSeeds call retries.
func TestRunSeeds_ErrorAbortsAndLeavesLedgerEmpty(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	attempts := 0
	cfg := entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			attempts++
			if attempts < 2 {
				return context.Canceled
			}
			return nil
		},
	}
	ent := entity.Define("retry_things", cfg)
	reg := newSeedRegistry(t, ent)

	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}
	if err := migrate.RunSeeds(context.Background(), db, reg); err == nil {
		t.Fatal("expected first RunSeeds to fail")
	}

	// Ledger should be empty after the failed attempt.
	var ledgered int
	_ = db.QueryRow("SELECT COUNT(*) FROM _gofastr_seeded WHERE entity_name='retry_things'").Scan(&ledgered)
	if ledgered != 0 {
		t.Errorf("ledger should be empty after failed seed, got %d rows", ledgered)
	}

	// Second call succeeds and records.
	if err := migrate.RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

// TestRunSeeds_SeedDataFromContext exercises the SeedFS/SeedPath flow end-to-end:
// the embedded JSON is reachable inside the Seed callback via
// entity.SeedDataFromContext.
func TestRunSeeds_SeedDataFromContext(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	cfg := entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "label", Type: schema.String, Required: true},
		},
		SeedFS:   seedFixtures,
		SeedPath: "testdata/seed_widgets.json",
		Seed: func(ctx context.Context, db *sql.DB) error {
			raw, err := entity.SeedDataFromContext(ctx)
			if err != nil {
				return err
			}
			var rows []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			}
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			for _, r := range rows {
				if _, err := db.ExecContext(ctx,
					"INSERT INTO widgets_seedfs (id, label) VALUES (?, ?)",
					r.ID, r.Label); err != nil {
					return err
				}
			}
			return nil
		},
	}
	ent := entity.Define("widgets_seedfs", cfg)
	reg := newSeedRegistry(t, ent)

	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}
	if err := migrate.RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatalf("run seeds: %v", err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets_seedfs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("inserted %d rows, want 2", n)
	}
}

// TestRunSeeds_NoSeedNoOp verifies that registries with no Seed-bearing
// entities don't even create the ledger table — the framework stays out
// of the way when seeding is unused.
func TestRunSeeds_NoSeedNoOp(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	cfg := entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
	}
	ent := entity.Define("plain_things", cfg)
	reg := newSeedRegistry(t, ent)

	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}
	if err := migrate.RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatal(err)
	}

	var tableExists int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='_gofastr_seeded'").Scan(&tableExists)
	if tableExists != 0 {
		t.Errorf("_gofastr_seeded created unexpectedly when no Seed configured")
	}
}

// TestRegisterEntities_SortsByName pins that bulk registration iterates
// the map in alphabetical-by-name order. Order matters because Entity()
// has order-sensitive side effects (router registration, MCP tool list,
// OpenAPI tag emission); random map iteration would mean non-deterministic
// /openapi.json bytes across restarts (breaking ETag caching) and
// non-deterministic MCP tools/list responses.
func TestRegisterEntities_SortsByName(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db))
	app.RegisterEntities(map[string]entity.EntityConfig{
		"foxtrot": {Fields: []schema.Field{{Name: "n", Type: schema.String, Required: true}}},
		"alpha":   {Fields: []schema.Field{{Name: "n", Type: schema.String, Required: true}}},
		"echo":    {Fields: []schema.Field{{Name: "n", Type: schema.String, Required: true}}},
		"bravo":   {Fields: []schema.Field{{Name: "n", Type: schema.String, Required: true}}},
	})

	// Probe registration-order observable via router.Routes() — each
	// entity registers its CRUD routes in a single batch, and the
	// listing reflects insertion order. The first GET route for each
	// entity is the list endpoint at /<table>.
	routes := app.router.Routes()
	var seen []string
	for _, r := range routes {
		if r.Method != "GET" {
			continue
		}
		switch r.Pattern {
		case "/alpha", "/bravo", "/echo", "/foxtrot":
			seen = append(seen, r.Pattern[1:])
		}
	}
	want := []string{"alpha", "bravo", "echo", "foxtrot"}
	if !equalSlice(seen, want) {
		t.Errorf("RegisterEntities did not register in alphabetical order:\n  got  %v\n  want %v", seen, want)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRegisterEntities pins the bulk-registration sugar: each map entry
// should land in the registry, with the same auto-CRUD/MCP side effects
// as individual Entity() calls.
func TestRegisterEntities(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db))
	app.RegisterEntities(map[string]entity.EntityConfig{
		"alpha": {Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}}},
		"beta":  {Fields: []schema.Field{{Name: "label", Type: schema.String, Required: true}}},
		"gamma": {Fields: []schema.Field{{Name: "code", Type: schema.String, Required: true}}},
	})

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := app.Registry.Get(name); err != nil {
			t.Errorf("entity %q not registered: %v", name, err)
		}
	}
}

// TestAppStart_CancelsAppCtxOnStartHookFailure pins the cleanup
// contract on Start failure: a startHook that returns an error must
// cause the app's lifecycle context to be cancelled, so any goroutine a
// previous startHook spawned (cron workers, queue consumers, SSE
// pumps) tears down instead of leaking past Start returning.
func TestAppStart_CancelsAppCtxOnStartHookFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithDB(db))
	var seenCtx context.Context
	// First hook captures the app's appCtx and starts a fake worker.
	app.OnStart(func(ctx context.Context) error {
		seenCtx = ctx
		return nil
	})
	// Second hook fails — Start must abort and cancel appCtx so the
	// "worker" the first hook would have spawned doesn't outlive Start.
	app.OnStart(func(ctx context.Context) error {
		return errBoomStartHook
	})

	startErr := app.Start(":0")
	if startErr == nil {
		t.Fatal("expected Start to return an error from the second OnStart hook")
	}
	if seenCtx == nil {
		t.Fatal("first OnStart never observed appCtx — wiring is wrong")
	}
	if seenCtx.Err() == nil {
		t.Fatal("appCtx not cancelled after Start failure — leaked-goroutine class bug")
	}
}

var errBoomStartHook = stringError("boom from start hook")

type stringError string

func (e stringError) Error() string { return string(e) }

// TestEntity_PanicsWhenSeedFSWithoutSeedPath pins the registration-time
// validation contract: SeedFS without SeedPath is a misconfiguration
// that would silently record the entity as seeded with empty data. The
// framework refuses to register the entity in that shape.
func TestEntity_PanicsWhenSeedFSWithoutSeedPath(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when SeedFS is set but SeedPath is empty")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "SeedFS") || !strings.Contains(msg, "SeedPath") {
			t.Errorf("panic message should name SeedFS and SeedPath, got: %v", r)
		}
	}()

	app := NewApp(WithDB(db))
	app.Entity("bad_seed", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			return nil
		},
		SeedFS:   seedFixtures, // set
		SeedPath: "",           // empty — misconfiguration
	})
}

// TestRunSeeds_LogsLifecycle pins observability: RunSeeds must emit a
// log line per seed start + completion so operators can debug a slow or
// hung seed. The log line should carry the entity name.
func TestRunSeeds_LogsLifecycle(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, "INSERT INTO observable (id, name) VALUES ('1', 'x')")
			return err
		},
	}
	ent := entity.Define("observable", cfg)
	reg := newSeedRegistry(t, ent)
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}

	ctx := migrate.WithSeedLogger(context.Background(), logger)
	if err := migrate.RunSeeds(ctx, db, reg); err != nil {
		t.Fatal(err)
	}

	got := logBuf.String()
	if !strings.Contains(got, "observable") {
		t.Errorf("expected log to include entity name 'observable', got:\n%s", got)
	}
	if !strings.Contains(got, "seed") {
		t.Errorf("expected log to mention 'seed', got:\n%s", got)
	}
}

// TestRunSeeds_RespectsContextCancellation pins that a context cancel
// during a hung Seed actually unblocks RunSeeds. Without this, App.Start
// can hang forever on a misbehaving Seed (e.g. blocking HTTP fetch).
func TestRunSeeds_RespectsContextCancellation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	cfg := entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	ent := entity.Define("hang_seed", cfg)
	reg := newSeedRegistry(t, ent)
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- migrate.RunSeeds(ctx, db, reg) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected RunSeeds to return error after context cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunSeeds did not return after context cancel — leaked goroutine / hung Seed")
	}
}

// TestRunSeeds_UsesBatchLedgerRead pins the perf contract that
// RunSeeds reads the ledger ONCE, not per entity. With 50 entities and
// network DB latency, the per-entity read was an N+1 hotspot.
func TestRunSeeds_UsesBatchLedgerRead(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	ents := make([]*entity.Entity, 0, 5)
	for i := 0; i < 5; i++ {
		nm := "batched_" + string(rune('a'+i))
		cfg := entity.EntityConfig{
			Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
			Seed: func(ctx context.Context, db *sql.DB) error {
				return nil
			},
		}
		ents = append(ents, entity.Define(nm, cfg))
	}
	reg := newSeedRegistry(t, ents...)
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatal(err)
	}
	if err := migrate.RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatal(err)
	}

	// Second pass: every entity is already in the ledger; the batch
	// read should issue ONE SELECT for the ledger, not five. Log line
	// 'ledger read' must appear exactly once.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := migrate.WithSeedLogger(context.Background(), logger)
	if err := migrate.RunSeeds(ctx, db, reg); err != nil {
		t.Fatal(err)
	}

	out := logBuf.String()
	if c := strings.Count(out, "ledger read"); c != 1 {
		t.Errorf("expected exactly 1 batch 'ledger read' log, got %d in:\n%s", c, out)
	}
}

// TestAppStart_WiresRunSeeds pins the framework App.Start contract: the
// Start path must invoke migrate.RunSeeds after AutoMigrate so apps that
// declare EntityConfig.Seed actually seed at startup. Regression risk if
// the call site in app.go gets moved or deleted.
func TestAppStart_WiresRunSeeds(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	seedCalled := false
	app := NewApp(WithDB(db))
	app.Entity("startup_seeded", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			seedCalled = true
			_, err := db.ExecContext(ctx, "INSERT INTO startup_seeded (id, name) VALUES ('1','startup')")
			return err
		},
	})

	// Drive Start on an ephemeral port; once Seed lands a row we know
	// the wiring is sound and we can tear the server down.
	startErr := make(chan error, 1)
	go func() { startErr <- app.Start(":0") }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var rows int
		if err := db.QueryRow("SELECT COUNT(*) FROM startup_seeded").Scan(&rows); err == nil && rows == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = app.Shutdown(context.Background())
	select {
	case <-startErr:
	case <-time.After(2 * time.Second):
	}

	if !seedCalled {
		t.Fatal("App.Start did not invoke EntityConfig.Seed")
	}
	var rows int
	_ = db.QueryRow("SELECT COUNT(*) FROM startup_seeded WHERE id='1'").Scan(&rows)
	if rows != 1 {
		t.Errorf("seeded row not present after App.Start; got %d rows", rows)
	}
}
