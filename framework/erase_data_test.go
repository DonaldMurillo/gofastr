package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// newEraseTestApp builds an App with two owner-scoped entities (documents,
// notes) and one plain entity (tags, no owner — must be untouched by erasure),
// then AutoMigrates. The caller creates the raw battery tables and seeds rows.
func newEraseTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db))

	// documents: owner-scoped + soft-delete + multi-tenant. Erasure must reach
	// the soft-deleted row too.
	app.Registry.Register(entity.Define("documents", entity.EntityConfig{
		Table: "documents",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id", SoftDelete: true, MultiTenant: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
		},
	}))
	// notes: owner-scoped, plain.
	app.Registry.Register(entity.Define("notes", entity.EntityConfig{
		Table: "notes",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
		},
	}))
	// tags: NOT owner-scoped. Erasure must leave every row alone.
	app.Registry.Register(entity.Define("tags", entity.EntityConfig{
		Table:  "tags",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}))

	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app, db
}

// createEraseRawTables makes the battery tables the entity registry does not
// know: auth_users, auth_sessions (canonical auth shapes), a custom
// user_prefs table (exercises the anonymize plane), and the framework audit
// table.
func createEraseRawTables(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, ddl := range []string{
		`CREATE TABLE auth_users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL)`,
		`CREATE TABLE auth_sessions (id TEXT PRIMARY KEY, token TEXT NOT NULL, user_id TEXT NOT NULL, expires_at TEXT)`,
		`CREATE TABLE user_prefs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, pref_value TEXT NOT NULL)`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("create raw table: %v", err)
		}
	}
	if err := EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
}

// seedEraseRows plants two users (u1, u2) across every surface so an erasure of
// u1 can be checked for completeness and for u2 being left intact.
func seedEraseRows(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v\nquery: %s", err, q)
		}
	}
	// documents: u1 owns two (one soft-deleted), u2 owns one.
	exec(`INSERT INTO documents (id, title, owner_id, tenant_id, created_at, updated_at, deleted_at) VALUES
		('d1','Doc One','u1','t1','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z',NULL),
		('d1b','Doc One softdel','u1','t1','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z','2024-01-05T00:00:00Z'),
		('d2','Doc Two','u2','t2','2024-01-03T00:00:00Z','2024-01-04T00:00:00Z',NULL)`)
	// notes: u1 one, u2 one.
	exec(`INSERT INTO notes (id, body, owner_id, created_at, updated_at) VALUES
		('n1','hello','u1','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z'),
		('n2','world','u2','2024-01-03T00:00:00Z','2024-01-04T00:00:00Z')`)
	// tags: one row, no owner — must survive erasure untouched.
	exec(`INSERT INTO tags (id, name, created_at, updated_at) VALUES
		('g1','go','2024-02-01T00:00:00Z','2024-02-02T00:00:00Z')`)
	// auth_users: u1, u2.
	exec(`INSERT INTO auth_users (id, email) VALUES ('u1','u1@example.com'),('u2','u2@example.com')`)
	// auth_sessions: u1 two, u2 one.
	exec(`INSERT INTO auth_sessions (id, token, user_id, expires_at) VALUES
		('s1','tok1','u1','2024-01-01T00:00:00Z'),
		('s1b','tok1b','u1','2024-01-01T00:00:00Z'),
		('s2','tok2','u2','2024-01-01T00:00:00Z')`)
	// user_prefs: u1 one, u2 one (anonymize plane).
	exec(`INSERT INTO user_prefs (id, user_id, pref_value) VALUES
		('p1','u1','secret-pref-1'),('p2','u2','secret-pref-2')`)
	// audit_log: u1 actor twice, u2 actor once, one system (NULL actor) row.
	// record_id carries heterogeneous values (a user id, a resource id) — it
	// must be left intact (only actor_id is anonymized).
	exec(`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at) VALUES
		('a1','auth','login.succeeded','u1','u1','2024-01-01T00:00:00Z'),
		('a2','auth','logout','u1','u1','2024-01-01T00:00:00Z'),
		('a3','documents','create','d2','u2','2024-01-03T00:00:00Z'),
		('a4','system','startup','-','','2024-01-01T00:00:00Z')`)
}

// registerEraseTestErasers wires the battery-plane erasers this scenario
// exercises: auth_sessions + auth_users (delete) and a custom user_prefs
// (anonymize). Mirrors what battery/auth/erase.go registers in production.
func registerEraseTestErasers(t *testing.T) {
	t.Helper()
	datexport.Reset(t)
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_sessions", Source: "auth", Table: "auth_sessions",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "auth_users", Source: "auth", Table: "auth_users",
		Column: "id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "user_prefs", Source: "test", Table: "user_prefs",
		Column: "user_id", Mode: datexport.EraseAnonymize,
		ScrubColumns: []string{"pref_value"}, Tombstone: "[scrubbed]",
	})
}

// countWhere returns the number of rows in table where col equals val.
func countWhere(t *testing.T, db *sql.DB, table, col, val string) int {
	t.Helper()
	var n int
	q := "SELECT COUNT(*) FROM " + quoteForCount(table) + " WHERE " + quoteForCount(col) + " = ?"
	if err := db.QueryRow(q, val).Scan(&n); err != nil {
		t.Fatalf("count %s.%s=%q: %v", table, col, val, err)
	}
	return n
}

func quoteForCount(s string) string { return "\"" + s + "\"" }

func countAll(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM \"" + table + "\"").Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// actorIDs returns the actor_id of every audit row ordered by id, so the test
// can assert which actors were anonymized and which were preserved.
func actorIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT actor_id FROM audit_log ORDER BY id")
	if err != nil {
		t.Fatalf("query audit actor_id: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan audit actor_id: %v", err)
		}
		if v.Valid {
			out = append(out, v.String)
		} else {
			out = append(out, "")
		}
	}
	return out
}

// TestEraseUserData is the red-first scenario: erase one user across all owned
// entities + battery tables, leave the other user and the audit trail intact,
// stay idempotent, and report dry-run counts that match a real run.
func TestEraseUserData(t *testing.T) {
	app, db := newEraseTestApp(t)
	defer db.Close()
	createEraseRawTables(t, db)
	seedEraseRows(t, db)
	registerEraseTestErasers(t)

	report, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatalf("EraseUserData u1: %v", err)
	}

	// ---- Entity plane: owner-scoped rows for u1 are gone (soft-deleted too),
	// u2 is intact, and the unscoped tags table is untouched.
	if got := countWhere(t, db, "documents", "owner_id", "u1"); got != 0 {
		t.Errorf("documents owner=u1 after erase = %d, want 0 (soft-deleted must go too)", got)
	}
	if got := countWhere(t, db, "notes", "owner_id", "u1"); got != 0 {
		t.Errorf("notes owner=u1 after erase = %d, want 0", got)
	}
	if got := countWhere(t, db, "documents", "owner_id", "u2"); got != 1 {
		t.Errorf("documents owner=u2 after erase = %d, want 1 (other user intact)", got)
	}
	if got := countWhere(t, db, "notes", "owner_id", "u2"); got != 1 {
		t.Errorf("notes owner=u2 after erase = %d, want 1 (other user intact)", got)
	}
	if got := countAll(t, db, "tags"); got != 1 {
		t.Errorf("tags rows after erase = %d, want 1 (unscoped table untouched)", got)
	}

	// ---- Battery plane (delete): auth_sessions + auth_users for u1 gone.
	if got := countWhere(t, db, "auth_sessions", "user_id", "u1"); got != 0 {
		t.Errorf("auth_sessions user=u1 after erase = %d, want 0", got)
	}
	if got := countWhere(t, db, "auth_users", "id", "u1"); got != 0 {
		t.Errorf("auth_users id=u1 after erase = %d, want 0", got)
	}
	if got := countWhere(t, db, "auth_sessions", "user_id", "u2"); got != 1 {
		t.Errorf("auth_sessions user=u2 after erase = %d, want 1", got)
	}
	if got := countWhere(t, db, "auth_users", "id", "u2"); got != 1 {
		t.Errorf("auth_users id=u2 after erase = %d, want 1", got)
	}

	// ---- Battery plane (anonymize): user_prefs rows are RETAINED but u1's
	// scrub column is the tombstone.
	if got := countAll(t, db, "user_prefs"); got != 2 {
		t.Errorf("user_prefs rows after erase = %d, want 2 (anonymize keeps rows)", got)
	}
	if got := countWhere(t, db, "user_prefs", "pref_value", "[scrubbed]"); got != 1 {
		t.Errorf("user_prefs scrubbed rows = %d, want 1 (only u1)", got)
	}
	if got := countWhere(t, db, "user_prefs", "pref_value", "secret-pref-2"); got != 1 {
		t.Errorf("user_prefs u2 value = %d, want 1 intact", got)
	}

	// ---- Audit plane: rows RETAINED (count unchanged), only u1's actor_id is
	// the tombstone. record_id and other actors are untouched.
	if got := countAll(t, db, "audit_log"); got != 4 {
		t.Errorf("audit_log rows after erase = %d, want 4 (audit is retained)", got)
	}
	gotActors := actorIDs(t, db)
	wantActors := []string{"[erased]", "[erased]", "u2", ""}
	if !equalStringSlices(gotActors, wantActors) {
		t.Errorf("audit actor_ids after erase = %v, want %v (u1 anonymized, u2 + system intact)", gotActors, wantActors)
	}
	var rec string
	if err := db.QueryRow("SELECT record_id FROM audit_log WHERE id = 'a1'").Scan(&rec); err != nil || rec != "u1" {
		t.Errorf("audit record_id for a1 = %q, want 'u1' (record_id is retained)", rec)
	}

	// ---- Report: the summary is faithful and non-dry-run.
	if report.DryRun {
		t.Errorf("report.DryRun = true, want false")
	}
	// deletes: 2 documents + 1 note + 2 sessions + 1 user = 6
	// anonymize: 2 audit + 1 prefs = 3
	// total = 9
	if got := report.TotalErased(); got != 9 {
		t.Errorf("report.TotalErased = %d, want 9", got)
	}
	if len(report.Entities) != 2 {
		t.Errorf("report.Entities = %d entries, want 2 (documents, notes)", len(report.Entities))
	}
	if len(report.Batteries) != 3 {
		t.Errorf("report.Batteries = %d entries, want 3 (auth_sessions, auth_users, user_prefs)", len(report.Batteries))
	}
	if report.Audit == nil || report.Audit.RowsAffected != 2 {
		t.Errorf("report.Audit = %+v, want RowsAffected=2", report.Audit)
	}

	// ---- Idempotent: a second erase of u1 affects nothing and errors nothing.
	report2, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatalf("EraseUserData u1 (2nd): %v", err)
	}
	if got := report2.TotalErased(); got != 0 {
		t.Errorf("idempotent re-erase TotalErased = %d, want 0", got)
	}

	// ---- Dry-run of u2: counts match a real erase but the DB is unchanged.
	beforeDocs := countWhere(t, db, "documents", "owner_id", "u2")
	beforeAudit := countWhere(t, db, "audit_log", "actor_id", "u2")
	dry, err := app.EraseUserData(context.Background(), "u2", WithEraseDryRun())
	if err != nil {
		t.Fatalf("EraseUserData u2 dry-run: %v", err)
	}
	if !dry.DryRun {
		t.Errorf("dry report.DryRun = false, want true")
	}
	// deletes: 1 doc + 1 note + 1 session + 1 user = 4; anonymize: 1 audit + 1 prefs = 2 → 6
	if got := dry.TotalErased(); got != 6 {
		t.Errorf("dry report.TotalErased = %d, want 6", got)
	}
	// DB untouched: u2's rows still there, actor_id still u2.
	if got := countWhere(t, db, "documents", "owner_id", "u2"); got != beforeDocs {
		t.Errorf("dry-run changed documents owner=u2: %d → %d (must be unchanged)", beforeDocs, got)
	}
	if got := countWhere(t, db, "audit_log", "actor_id", "u2"); got != beforeAudit {
		t.Errorf("dry-run changed audit actor=u2: %d → %d (must be unchanged)", beforeAudit, got)
	}
	if got := countWhere(t, db, "user_prefs", "pref_value", "secret-pref-2"); got != 1 {
		t.Errorf("dry-run scrubbed u2 prefs: want 1 intact, got %d", got)
	}
}

// TestEraseUserData_NilSafe guards the nil-App / nil-DB entry guards.
func TestEraseUserData_NilSafe(t *testing.T) {
	var app *App
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("nil App EraseUserData should error")
	}
	app2 := &App{}
	if _, err := app2.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("nil DB EraseUserData should error")
	}
}

func equalStringSlices(a, b []string) bool {
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

// An empty user id must be refused before any statement runs: WHERE
// owner = ” matches every unowned row, so a host that calls this after
// failing to resolve the caller would wipe them.
func TestEraseUserData_RejectsEmptyUserID(t *testing.T) {
	app, _ := newEraseTestApp(t)
	for _, id := range []string{"", "   "} {
		if _, err := app.EraseUserData(context.Background(), id); err == nil {
			t.Fatalf("empty user id %q was accepted", id)
		}
		if _, err := app.EraseUserData(context.Background(), id, WithEraseDryRun()); err == nil {
			t.Fatalf("empty user id %q was accepted in dry-run", id)
		}
	}
}

// Erasure must delete children before parents: the generated foreign keys
// carry no ON DELETE CASCADE, so a lexical order that hits the parent first
// makes the whole transaction roll back and erases nothing.
func TestEraseUserData_ChildBeforeParentFKOrder(t *testing.T) {
	db := openSQLiteMem(t)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db))
	// "accounts" sorts before "posts": lexical order would delete the parent
	// first and trip the constraint.
	app.Registry.Register(entity.Define("accounts", entity.EntityConfig{
		Table:  "accounts",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}))
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table:     "posts",
		Scope:     &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields:    []schema.Field{{Name: "owner_id", Type: schema.String}, {Name: "account_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("account", "accounts", "account_id")},
	}))

	if _, err := db.Exec(`CREATE TABLE accounts (id TEXT PRIMARY KEY, owner_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, account_id TEXT REFERENCES accounts(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts VALUES ('a1','u1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO posts VALUES ('p1','u1','a1')`); err != nil {
		t.Fatal(err)
	}

	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("erase failed on a parent/child graph: %v", err)
	}
	for _, table := range []string{"accounts", "posts"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) after erasure", table, n)
		}
	}
}

// The audit-plane options are part of the public erasure surface: a host
// that renamed audit_log via AuditConfig.Table must be able to point the
// erasure at it, and to choose the tombstone written into actor_id.
func TestEraseUserData_AuditTableAndTombstoneOptions(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`CREATE TABLE compliance_trail (actor_id TEXT, record_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO compliance_trail VALUES ('u1','r1'), ('u2','r2')`); err != nil {
		t.Fatal(err)
	}

	rep, err := app.EraseUserData(context.Background(), "u1",
		WithEraseAuditTable("compliance_trail"),
		WithEraseAuditTombstone("<gone>"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Audit == nil || rep.Audit.RowsAffected != 1 {
		t.Fatalf("audit plane did not anonymize the renamed table: %+v", rep.Audit)
	}

	var actor string
	if err := db.QueryRow(`SELECT actor_id FROM compliance_trail WHERE record_id = 'r1'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != "<gone>" {
		t.Errorf("actor_id = %q, want the configured tombstone", actor)
	}
	// The other user's row is untouched.
	if err := db.QueryRow(`SELECT actor_id FROM compliance_trail WHERE record_id = 'r2'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != "u2" {
		t.Errorf("unrelated actor_id = %q, want u2", actor)
	}
}

// Erasure failures must surface, not silently report zero: a missing owner
// column (a schema/registry mismatch) has to abort the transaction so a
// compliance caller never records a "successful" erasure that erased nothing.
func TestEraseUserData_ErrorsOnMissingOwnerColumn(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`DROP TABLE documents`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE documents (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("erasure reported success against a table with no owner column")
	}
	// Dry-run takes the same read path and must fail the same way.
	if _, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun()); err == nil {
		t.Fatal("dry-run reported success against a table with no owner column")
	}
}

// A registered eraser pointing at a column the table does not have must
// fail loudly for the same reason the entity plane does: on SQLite the
// quoted unknown identifier degrades to a string literal, so the erasure
// would report success having matched nothing.
func TestEraseUserData_ErrorsOnMissingEraserColumn(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`CREATE TABLE plugin_rows (id TEXT PRIMARY KEY, note TEXT)`); err != nil {
		t.Fatal(err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "plugin_rows", Source: "test", Table: "plugin_rows",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("plugin_rows") })

	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("erasure succeeded against an eraser whose column is absent")
	}
	if _, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun()); err == nil {
		t.Fatal("dry-run succeeded against an eraser whose column is absent")
	}
}

// An anonymizing eraser with no ScrubColumns is a misconfiguration, not a
// no-op: it would report rows "erased" while leaving them intact.
func TestEraseUserData_AnonymizeRequiresScrubColumns(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`CREATE TABLE trace_rows (user_id TEXT, detail TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO trace_rows VALUES ('u1','d')`); err != nil {
		t.Fatal(err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "trace_rows", Source: "test", Table: "trace_rows",
		Column: "user_id", Mode: datexport.EraseAnonymize,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("trace_rows") })

	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("anonymize with no ScrubColumns silently succeeded")
	}
	// The row survives — the transaction rolled back.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM trace_rows`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("row count = %d after a failed erasure, want 1 (rollback)", n)
	}
}

// A relation cycle must not hang the ordering pass — it degrades to the
// input (lexical) order, which is the best available answer.
func TestOrderChildrenFirst_CycleDegradesToInputOrder(t *testing.T) {
	a := entity.Define("a", entity.EntityConfig{
		Table:     "a",
		Scope:     &entity.ScopeConfig{OwnerField: "owner_id"},
		Relations: []entity.Relation{entity.BelongsTo("b", "b", "b_id")},
	})
	b := entity.Define("b", entity.EntityConfig{
		Table:     "b",
		Scope:     &entity.ScopeConfig{OwnerField: "owner_id"},
		Relations: []entity.Relation{entity.BelongsTo("a", "a", "a_id")},
	})
	merged := map[string]*entity.Entity{"a": a, "b": b}

	got := orderChildrenFirst([]string{"a", "b"}, merged, []string{"a", "b"})
	if len(got) != 2 {
		t.Fatalf("cycle lost a table: got %v", got)
	}
	seen := map[string]bool{}
	for _, tbl := range got {
		if seen[tbl] {
			t.Fatalf("table %q emitted twice: %v", tbl, got)
		}
		seen[tbl] = true
	}
}

// The battery plane's anonymize path, end to end: scrub columns are set to
// the tombstone AND the match column is cut, which is what makes a re-run
// idempotent by count.
func TestEraseUserData_AnonymizeEraserScrubsAndIsIdempotent(t *testing.T) {
	app, db := newEraseTestApp(t)
	if _, err := db.Exec(`CREATE TABLE tickets (user_id TEXT, email TEXT, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tickets VALUES ('u1','a@x.test','hello'), ('u2','b@x.test','hi')`); err != nil {
		t.Fatal(err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "tickets", Source: "test", Table: "tickets",
		Column: "user_id", ScrubColumns: []string{"email"}, Mode: datexport.EraseAnonymize,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("tickets") })

	rep, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rep.Batteries {
		if r.Table == "tickets" {
			found = true
			if r.Mode != "anonymize" || r.RowsAffected != 1 {
				t.Errorf("tickets result = %+v, want anonymize/1", r)
			}
		}
	}
	if !found {
		t.Fatal("tickets missing from the battery plane of the report")
	}

	var email, user string
	if err := db.QueryRow(`SELECT user_id, email FROM tickets WHERE body = 'hello'`).Scan(&user, &email); err != nil {
		t.Fatal(err)
	}
	if email == "a@x.test" {
		t.Error("scrub column was not anonymized")
	}
	if user == "u1" {
		t.Error("match column still links the row to the erased user")
	}

	// Idempotent by count: the link is cut, so a re-run matches nothing.
	rep2, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep2.Batteries {
		if r.Table == "tickets" && r.RowsAffected != 0 {
			t.Errorf("re-run matched %d row(s), want 0", r.RowsAffected)
		}
	}

	// The other user is untouched.
	if err := db.QueryRow(`SELECT email FROM tickets WHERE body = 'hi'`).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "b@x.test" {
		t.Errorf("unrelated row was scrubbed: %q", email)
	}
}

// A registered eraser whose table does not exist is skipped, not fatal: a
// battery may be imported without its schema ever being created. The
// entity plane and audit plane still run.
func TestEraseUserData_AbsentEraserTableIsSkipped(t *testing.T) {
	app, _ := newEraseTestApp(t)
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "never_created", Source: "test", Table: "never_created",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("never_created") })

	rep, err := app.EraseUserData(context.Background(), "u1")
	if err != nil {
		t.Fatalf("an absent battery table made the erasure fail: %v", err)
	}
	for _, r := range rep.Batteries {
		if r.Table == "never_created" {
			t.Error("an absent table appeared in the report")
		}
	}
	if len(rep.Entities) == 0 {
		t.Error("entity plane did not run alongside the skipped table")
	}

	// Dry-run takes the same skip path.
	if _, err := app.EraseUserData(context.Background(), "u2", WithEraseDryRun()); err != nil {
		t.Fatalf("dry-run failed on an absent battery table: %v", err)
	}
}

// The report's TotalErased sums every plane — the number a compliance log
// records.
func TestEraseReport_TotalErasedSumsAllPlanes(t *testing.T) {
	r := EraseReport{
		Entities:  []EraseTableResult{{RowsAffected: 2}, {RowsAffected: 3}},
		Batteries: []EraseTableResult{{RowsAffected: 4}},
		Audit:     &EraseTableResult{RowsAffected: 5},
	}
	if got := r.TotalErased(); got != 14 {
		t.Errorf("TotalErased = %d, want 14", got)
	}
	if got := (EraseReport{}).TotalErased(); got != 0 {
		t.Errorf("empty report TotalErased = %d, want 0", got)
	}
}

// requireColumn's contract, exercised directly: present column passes,
// missing column errors naming both table and column, and an absent table
// defers to the caller's existence probe rather than erroring here.
func TestRequireColumn(t *testing.T) {
	db := openSQLiteMem(t)
	if _, err := db.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY, owner_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := requireColumn(ctx, db, "t", "owner_id", migrate.DialectSQLite); err != nil {
		t.Errorf("present column errored: %v", err)
	}
	// Column matching is case-insensitive: Postgres folds unquoted names.
	if err := requireColumn(ctx, db, "t", "OWNER_ID", migrate.DialectSQLite); err != nil {
		t.Errorf("case-different column errored: %v", err)
	}
	err := requireColumn(ctx, db, "t", "nope", migrate.DialectSQLite)
	if err == nil {
		t.Fatal("missing column did not error")
	}
	for _, want := range []string{`"t"`, `"nope"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if err := requireColumn(ctx, db, "absent_table", "any", migrate.DialectSQLite); err != nil {
		t.Errorf("absent table should defer to the existence probe, got: %v", err)
	}
}

// A failure partway through the erasure must roll the whole thing back:
// a partially-erased user is neither erased nor intact, and the report must
// not claim rows that no longer exist.
func TestEraseUserData_RollsBackOnMidFlightFailure(t *testing.T) {
	app, db := newEraseTestApp(t)
	createEraseRawTables(t, db)
	seedEraseRows(t, db)
	// A battery table whose column exists at probe time but whose DELETE
	// fails: a trigger that raises is the portable way to force it.
	if _, err := db.Exec(`CREATE TABLE locked_rows (user_id TEXT, note TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO locked_rows VALUES ('u1','keep')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER no_delete BEFORE DELETE ON locked_rows
		BEGIN SELECT RAISE(ABORT, 'deletion refused'); END`); err != nil {
		t.Fatal(err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "locked_rows", Source: "test", Table: "locked_rows",
		Column: "user_id", Mode: datexport.EraseDelete,
	})
	t.Cleanup(func() { datexport.UnregisterEraser("locked_rows") })

	before := countRows(t, db, "documents")
	if before == 0 {
		t.Fatal("fixture has no owned documents to roll back")
	}

	if _, err := app.EraseUserData(context.Background(), "u1"); err == nil {
		t.Fatal("erasure succeeded despite a refused delete")
	}
	if got := countRows(t, db, "documents"); got != before {
		t.Errorf("documents = %d after a failed erasure, want %d (rollback)", got, before)
	}
	if got := countRows(t, db, "locked_rows"); got != 1 {
		t.Errorf("locked_rows = %d, want 1", got)
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
