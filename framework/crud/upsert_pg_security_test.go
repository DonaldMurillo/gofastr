package crud

// Postgres halves of the UpsertOne scope tests (upsert_scope_security_test.go).
// Skips when Postgres is unreachable (see internal/pgtest).

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func pgPagesUpsert(t *testing.T, table string) (*CrudHandler, *sql.DB) {
	t.Helper()
	return pgCrudSetup(t, makeEntityConfig(table, table, "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
		{Name: "body", Type: schema.String},
	}), `CREATE TABLE `+table+` (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT, body TEXT)`)
}

func pgPageOwner(t *testing.T, db *sql.DB, table, id string) (string, string) {
	t.Helper()
	var o, b string
	if err := db.QueryRow("SELECT owner_id, body FROM "+table+" WHERE id = $1", id).Scan(&o, &b); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return o, b
}

func TestPGUpsertNormalizedPKCannotHijack(t *testing.T) {
	ch, db := pgPagesUpsert(t, "upsert_hook_pages")
	pgSeed(t, db, "upsert_hook_pages", []map[string]any{{"id": "pricing", "owner_id": "alice", "title": "Pricing", "body": "alice original"}})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, lowerPKHook)
	_, err := ch.UpsertOne(upsertSecurityContext("bob", ""),
		map[string]any{"id": "PRICING", "title": "Pricing", "body": "bob overwrote"})
	if !errors.Is(err, errUpsertForeignRow) {
		t.Fatalf("err = %v, want errUpsertForeignRow", err)
	}
	if o, b := pgPageOwner(t, db, "upsert_hook_pages", "pricing"); o != "alice" || b != "alice original" {
		t.Fatalf("alice's row taken over: owner_id=%q body=%q", o, b)
	}
}

// A row another session commits after UpsertOne's preflight read and before
// its INSERT resolves the conflict: under READ COMMITTED the preflight saw
// nothing, and ON CONFLICT DO UPDATE then acts on the committed row. Only a
// scope predicate on the DO UPDATE itself refuses it.
//
// Session B inserts alice's race-1 and holds the transaction open. Bob's
// upsert reads nothing in its preflight, then blocks in the INSERT on B's
// uncommitted key. B commits; bob's ON CONFLICT now sees alice's row.
func TestPGUpsertRaceCannotHijack(t *testing.T) {
	ch, db := pgPagesUpsert(t, "upsert_race_pages")
	db.SetMaxOpenConns(4)
	ctx := context.Background()

	other, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback()
	if _, err := other.ExecContext(ctx,
		"INSERT INTO upsert_race_pages (id, owner_id, title, body) VALUES ('race-1','alice','A','alice concurrent')"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ch.UpsertOne(upsertSecurityContext("bob", ""),
			map[string]any{"id": "race-1", "title": "B", "body": "bob overwrote"})
		done <- err
	}()

	// Wait until bob's INSERT is parked on B's row lock, so the commit
	// below lands after his preflight read.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE 'INSERT INTO upsert_race_pages%' AND datname = current_database()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("upsert finished before the concurrent commit: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("bob's upsert never blocked on the concurrent insert")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := other.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, errUpsertForeignRow) {
			t.Fatalf("err = %v, want errUpsertForeignRow", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("upsert did not finish after the concurrent commit")
	}
	if o, b := pgPageOwner(t, db, "upsert_race_pages", "race-1"); o != "alice" || b != "alice concurrent" {
		t.Fatalf("alice's concurrently committed row taken over: owner_id=%q body=%q", o, b)
	}
}
