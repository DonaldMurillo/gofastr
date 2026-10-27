package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The delete correlates the target table with its staging table. A table
// whose name is the staging alias must not resolve to the staging table
// inside the subquery: that compares staged keys with themselves, and
// every row of the table matches.
func TestEraseTableNamedLikeAlias(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("d", entity.EntityConfig{
			Table:  "d",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db, `INSERT INTO d (id, owner_id) VALUES ('a','u1'), ('b','u2')`)

		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "d", "b")
	})
}

// Erasure names a mixed-case owner-scoped table the way AutoMigrate's
// unquoted DDL stored it, so Postgres finds it.
func TestEraseMixedCaseEntity(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("Notes", entity.EntityConfig{
			Table:  "Notes",
			Scope:  &entity.ScopeConfig{OwnerField: "OwnerID"},
			Fields: []schema.Field{{Name: "OwnerID", Type: schema.String}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db, `INSERT INTO Notes (id, OwnerID) VALUES ('a','u1'), ('b','u2')`)
		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "Notes", "b")
	})
}

// The audit log stores a record id with its control bytes stripped, so
// erasure matches the id in that spelling: an admin's audit copy of a row
// whose key holds a newline is scrubbed with the row.
func TestEraseAuditMatchesStrippedKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("notes", entity.EntityConfig{
			Table:  "notes",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{})
		if _, err := db.Exec(`INSERT INTO notes (id, owner_id) VALUES ($1, 'u1')`, "a\nb"); err != nil {
			t.Fatal(err)
		}
		cascadeExec(t, db, `INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff)
			VALUES ('adm1', 'notes', 'update', '`+sanitizeAuditField("a\nb")+`', 'admin', '2024-01-01T00:00:00Z',
			'{"new":{"body":"secret"}}')`)

		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		var diff string
		if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = 'adm1'`).Scan(&diff); err != nil {
			t.Fatal(err)
		}
		if diff != erasedAuditDiff {
			t.Errorf("admin audit diff = %q, want the erasure marker", diff)
		}
	})
}

// EnsureAuditTable folds a custom table's name, and erasure names it the
// same way: AuditTrail is audittrail on Postgres.
func TestEraseMixedCaseAuditTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		if err := EnsureAuditTable(db, "AuditTrail"); err != nil {
			t.Fatalf("EnsureAuditTable: %v", err)
		}
		cascadeExec(t, db, `INSERT INTO audittrail (id, entity, op, record_id, actor_id, created_at, diff)
			VALUES ('r1', 'notes', 'update', 'a', 'u1', '2024-01-01T00:00:00Z', '{"new":{"body":"secret"}}')`)

		if _, err := app.EraseUserData(context.Background(), "u1", WithEraseAuditTable("AuditTrail")); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		var actor, diff string
		if err := db.QueryRow(`SELECT actor_id, diff FROM audittrail WHERE id = 'r1'`).Scan(&actor, &diff); err != nil {
			t.Fatal(err)
		}
		if actor == "u1" || diff != erasedAuditDiff {
			t.Errorf("audit row = (%q, %q), want the actor and diff scrubbed", actor, diff)
		}
	})
}

// Two comments that name each other: no insert order satisfies the
// constraint, so the restore inserts one with its reference empty and sets
// it once both rows exist.
func TestImportRestoresRowCycle(t *testing.T) {
	forEachRestore(t, newBlogApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db,
			`INSERT INTO posts (id, title) VALUES ('p1','hello')`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c1','p1',NULL)`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c2','p1','c1')`,
			`UPDATE comments SET parent_id = 'c2' WHERE id = 'c1'`,
		)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		for id, want := range map[string]string{"c1": "c2", "c2": "c1"} {
			var parent sql.NullString
			if err := dst.QueryRow(`SELECT parent_id FROM comments WHERE id = $1`, id).Scan(&parent); err != nil {
				t.Fatal(err)
			}
			if parent.String != want {
				t.Errorf("%s.parent_id = %q, want %q", id, parent.String, want)
			}
		}
	})
}

// A raw table with no primary key that references an owned row: nothing
// can reference it back, so its rows go by their foreign key, and the
// erasure neither fails nor touches another user's rows.
func TestEraseKeylessRawChild(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("docs", entity.EntityConfig{
			Table:  "docs",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		app.Table(migrate.Table{
			Name: "doc_events",
			Columns: []migrate.Column{
				{Name: "doc_id", Type: schema.String},
				{Name: "kind", Type: schema.String},
			},
			ForeignKeys: []migrate.ForeignKey{{Columns: []string{"doc_id"}, RefTable: "docs"}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO docs (id, owner_id) VALUES ('d1','u1'), ('d2','u2')`,
			`INSERT INTO doc_events (doc_id, kind) VALUES ('d1','open'), ('d1','edit'), ('d2','open')`,
		)
		dry, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun())
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if r, _ := cascadeResult(dry, "doc_events"); r.RowsAffected != 2 {
			t.Errorf("dry-run Cascaded[doc_events] = %+v, want 2 rows", r)
		}
		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		if r, _ := cascadeResult(rep, "doc_events"); r.RowsAffected != 2 {
			t.Errorf("Cascaded[doc_events] = %+v, want 2 rows", r)
		}
		var left []string
		rows, err := db.Query(`SELECT doc_id FROM doc_events ORDER BY doc_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			left = append(left, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(left) != 1 || left[0] != "d2" {
			t.Errorf("doc_events left = %v, want [d2]", left)
		}
		wantIDs(t, db, "docs", "d2")
	})
}

// A staging table hides an app table of the same name from every
// unqualified reference, so a staging name never reuses a graph table's.
func TestEraseStagingNameAvoidsTables(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("gofastr_erase_0", entity.EntityConfig{
			Table:  "gofastr_erase_0",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db, `INSERT INTO gofastr_erase_0 (id, owner_id) VALUES ('a','u1'), ('b','u2')`)
		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "gofastr_erase_0", "b")
	})
}

// An entity whose table and column names carry capitals: AutoMigrate's
// DDL leaves them unquoted, so Postgres folds them, and the export and
// import must name them the same way.
func TestExportMixedCaseEntity(t *testing.T) {
	build := func(t *testing.T, db *sql.DB) *App {
		t.Helper()
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("Widgets", entity.EntityConfig{
			Table:  "Widgets",
			Fields: []schema.Field{{Name: "UserName", Type: schema.String}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		return app
	}
	forEachRestore(t, build, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db, `INSERT INTO Widgets (id, UserName) VALUES ('w1', 'alice')`)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var name string
		if err := dst.QueryRow(`SELECT UserName FROM Widgets WHERE id = 'w1'`).Scan(&name); err != nil || name != "alice" {
			t.Errorf("restored UserName = %q, %v; want alice", name, err)
		}
	})
}
