package crud

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

const staleStamp = "2000-01-01T00:00:00.000000Z"

// forStampedNotes runs fn on SQLite and Postgres against a migrated
// entity with auto timestamps, seeded with one row stamped long ago. The
// handler writes through an insertRecorder so the test reads the SQL.
func forStampedNotes(t *testing.T, fn func(t *testing.T, ch *CrudHandler, rec *insertRecorder, conn *sql.DB)) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := entity.Define("tsnotes", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(true))
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		if _, err := conn.Exec(`INSERT INTO tsnotes (id, title, created_at, updated_at) VALUES ('n1', 'orig', $1, $1)`, staleStamp); err != nil {
			t.Fatalf("seed: %v", err)
		}
		rec := &insertRecorder{db: conn}
		fn(t, NewCrudHandler(ent, rec).WithJSONCase(CaseSnake), rec, conn)
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

func stampMoved(t *testing.T, conn *sql.DB, col string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM tsnotes WHERE id = 'n1' AND `+col+` > $1`, staleStamp).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// TestUpsertConflictRestampsUpdatedAt: the conflict arm is an update,
// and every other update path re-stamps updated_at. The upsert skipped
// every auto field in its SET, so the timestamp stayed at the row's
// creation and updated_at sorts, cursors and changed-since syncs missed
// the edit. A caller-sent updated_at is ignored there, as on update.
func TestUpsertConflictRestampsUpdatedAt(t *testing.T) {
	forStampedNotes(t, func(t *testing.T, ch *CrudHandler, rec *insertRecorder, conn *sql.DB) {
		if _, err := ch.UpsertOne(context.Background(), map[string]any{
			"id": "n1", "title": "edited", "updated_at": "1999-01-01T00:00:00.000000Z",
		}); err != nil {
			t.Fatalf("upsert onto n1: %v", err)
		}
		if len(rec.inserts) != 1 {
			t.Fatalf("recorded %d INSERTs, want 1: %q", len(rec.inserts), rec.inserts)
		}
		_, set, ok := strings.Cut(rec.inserts[0], "DO UPDATE SET ")
		if !ok {
			t.Fatalf("no DO UPDATE SET in %q", rec.inserts[0])
		}
		set, _, _ = strings.Cut(set, " RETURNING ")
		if !strings.Contains(set, "updated_at = $") {
			t.Errorf("DO UPDATE SET %q does not re-stamp updated_at", set)
		}
		if strings.Contains(set, "created_at") {
			t.Errorf("DO UPDATE SET %q rewrites created_at", set)
		}
		if !stampMoved(t, conn, "updated_at") {
			t.Error("updated_at did not move on the conflict arm")
		}
		if stampMoved(t, conn, "created_at") {
			t.Error("created_at moved on the conflict arm")
		}
	})
}

// TestUpsertNoopKeepsUpdatedAt: an upsert that sets nothing takes the
// DO NOTHING arm, and a re-stamp alone is not a write, as on update.
func TestUpsertNoopKeepsUpdatedAt(t *testing.T) {
	forStampedNotes(t, func(t *testing.T, ch *CrudHandler, rec *insertRecorder, conn *sql.DB) {
		if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n1"}); err != nil {
			t.Fatalf("upsert onto n1: %v", err)
		}
		if len(rec.inserts) != 1 || !strings.Contains(rec.inserts[0], "DO NOTHING") {
			t.Fatalf("want one INSERT … DO NOTHING, got %q", rec.inserts)
		}
		if stampMoved(t, conn, "updated_at") {
			t.Error("updated_at moved on an upsert that changed nothing")
		}
	})
}

// TestUpsertScopedNoopKeepsUpdatedAt: on an owner- or tenant-scoped
// entity the conflict arm's SET always carries the scope column, so a
// key-only upsert re-stamped updated_at there while the same call on an
// unscoped entity did not (2026-11-01 review). The scope column is not a
// write: only a sent data column re-stamps.
func TestUpsertScopedNoopKeepsUpdatedAt(t *testing.T) {
	installSecurityOwnerExtractor(t)
	cases := map[string]struct {
		cfg entity.EntityConfig
		ctx context.Context
	}{
		"owner": {makeEntityConfig("posts", "posts", "owner_id", []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		}).WithTimestamps(true), upsertSecurityContext("alice", "")},
		"tenant": {entity.EntityConfig{Table: "posts", Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		}, Scope: &entity.ScopeConfig{MultiTenant: true}}.WithTimestamps(true), upsertSecurityContext("", "tenant-a")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ch, db := setupSecurityTestHandler(t, c.cfg, `CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, tenant_id TEXT, title TEXT, created_at TEXT, updated_at TEXT)`)
			if _, err := db.Exec(`INSERT INTO posts (id, owner_id, tenant_id, title, created_at, updated_at) VALUES ('p1', 'alice', 'tenant-a', 'orig', $1, $1)`, staleStamp); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if _, err := ch.UpsertOne(c.ctx, map[string]any{"id": "p1"}); err != nil {
				t.Fatalf("key-only upsert: %v", err)
			}
			var got string
			if err := db.QueryRow(`SELECT updated_at FROM posts WHERE id = 'p1'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != staleStamp {
				t.Errorf("updated_at = %q after a key-only upsert, want %q", got, staleStamp)
			}
			if _, err := ch.UpsertOne(c.ctx, map[string]any{"id": "p1", "title": "new"}); err != nil {
				t.Fatalf("data upsert: %v", err)
			}
			if err := db.QueryRow(`SELECT updated_at FROM posts WHERE id = 'p1'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got == staleStamp {
				t.Errorf("updated_at unchanged after an upsert that sent title")
			}
		})
	}
}
