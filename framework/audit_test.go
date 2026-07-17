package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// auditApp builds a single-entity app with the audit helper enabled.
// The Actor callback returns a fixed id so tests can assert it round-trips.
func auditApp(t *testing.T, db *sql.DB, actor string) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		// Public: these tests exercise the audit trail's anonymous-actor
		// path (TestAudit_AnonymousActor posts with no session and
		// expects a NULL actor_id) — the secure-by-default session gate
		// (issue #65) would otherwise 401 every request in this suite.
		Public: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{
		Actor: func(_ context.Context) string { return actor },
	})
	return app
}

// readAuditRows pulls every audit row in insertion order. Returned as
// a slice of maps so tests can index by column without scan boilerplate.
func readAuditRows(t *testing.T, db *sql.DB) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT entity, op, record_id, actor_id, diff FROM audit_log ORDER BY created_at, id`)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var ent, op, recordID string
		var actor sql.NullString
		var diff sql.NullString
		if err := rows.Scan(&ent, &op, &recordID, &actor, &diff); err != nil {
			t.Fatalf("scan: %v", err)
		}
		row := map[string]any{
			"entity":    ent,
			"op":        op,
			"record_id": recordID,
		}
		if actor.Valid {
			row["actor_id"] = actor.String
		}
		if diff.Valid {
			row["diff"] = diff.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return out
}

// ============================================================================
// Create — AfterCreate hook fires once with op=create and the new row in diff.
// ============================================================================

func TestAudit_CreateFiresOnce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditApp(t, db, "alice"))

		resp := ta.Post("/posts", map[string]any{"title": "hello"})
		resp.AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d (%+v)", len(rows), rows)
		}
		if rows[0]["op"] != "create" {
			t.Fatalf("op: got %v", rows[0]["op"])
		}
		if rows[0]["entity"] != "posts" {
			t.Fatalf("entity: got %v", rows[0]["entity"])
		}
		if rows[0]["actor_id"] != "alice" {
			t.Fatalf("actor_id: got %v", rows[0]["actor_id"])
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[0]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow, ok := diff["new"].(map[string]any)
		if !ok {
			t.Fatalf("diff.new missing: %+v", diff)
		}
		if newRow["title"] != "hello" {
			t.Fatalf("diff.new.title: got %v", newRow["title"])
		}
	})
}

// ============================================================================
// Update — AfterUpdate produces op=update with the new state.
// ============================================================================

func TestAudit_UpdateCapturesNewState(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditApp(t, db, "bob"))

		create := ta.Post("/posts", map[string]any{"title": "v1"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		id := created["id"].(string)

		upd := ta.Put("/posts/"+id, map[string]any{"title": "v2"})
		upd.AssertStatus(t, http.StatusOK)

		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d", len(rows))
		}
		if rows[1]["op"] != "update" {
			t.Fatalf("op[1]: got %v", rows[1]["op"])
		}
		if rows[1]["record_id"] != id {
			t.Fatalf("record_id[1]: got %v want %s", rows[1]["record_id"], id)
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[1]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow := diff["new"].(map[string]any)
		if newRow["title"] != "v2" {
			t.Fatalf("update diff.new.title: %v", newRow["title"])
		}
	})
}

// ============================================================================
// Delete — AfterDelete writes op=delete with the deleted id; diff is null.
// ============================================================================

func TestAudit_DeleteRecordsID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditApp(t, db, "carol"))

		create := ta.Post("/posts", map[string]any{"title": "gone soon"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		json.Unmarshal([]byte(create.Body()), singleMap(&created))
		id := created["id"].(string)

		del := ta.Delete("/posts/" + id)
		del.AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d", len(rows))
		}
		if rows[1]["op"] != "delete" {
			t.Fatalf("op[1]: got %v", rows[1]["op"])
		}
		if rows[1]["record_id"] != id {
			t.Fatalf("record_id[1]: got %v want %s", rows[1]["record_id"], id)
		}
		// The delete audit row now carries an "old" snapshot + meta
		// for forensics (see TestAudit_DeleteIncludesDeletedRecordSnapshot).
		// Just sanity-check the diff parses and references the deleted row.
		raw, hasDiff := rows[1]["diff"].(string)
		if !hasDiff || raw == "" {
			t.Fatalf("expected non-empty diff on delete, got %v", rows[1]["diff"])
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(raw), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		if _, ok := diff["old"]; !ok {
			t.Fatalf("delete diff missing 'old' snapshot: %s", raw)
		}
	})
}

// ============================================================================
// Anonymous actor — Actor func returning "" yields actor_id NULL (not "").
// ============================================================================

func TestAudit_AnonymousActor(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditApp(t, db, ""))

		resp := ta.Post("/posts", map[string]any{"title": "anon"})
		resp.AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if _, ok := rows[0]["actor_id"]; ok {
			t.Fatalf("expected NULL actor_id, got %v", rows[0]["actor_id"])
		}
	})
}

// ============================================================================
// Entities filter — only the named entity is audited.
// ============================================================================

func TestAudit_EntityFilter(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		app.Entity("notes", entity.EntityConfig{
			Table: "notes",
			Fields: []schema.Field{
				{Name: "body", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{Entities: []string{"posts"}})

		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		ta.Post("/posts", map[string]any{"title": "tracked"}).AssertStatus(t, http.StatusCreated)
		ta.Post("/notes", map[string]any{"body": "untracked"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row (posts only), got %d (%+v)", len(rows), rows)
		}
		if rows[0]["entity"] != "posts" {
			t.Fatalf("entity: %v", rows[0]["entity"])
		}
	})
}
