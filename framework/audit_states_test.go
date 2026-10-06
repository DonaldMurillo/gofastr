package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// statesAuditFields is the invoices field set the states audit fixture is
// written against.
func statesAuditFields() []schema.Field {
	return []schema.Field{
		{Name: "number", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "void"}},
		{Name: "paid_on", Type: schema.Date},
	}
}

func statesAuditStates() *entity.StatesConfig {
	return &entity.StatesConfig{
		Field:   "status",
		Initial: []string{"draft", "open"},
		Transitions: []entity.Transition{
			{Key: "pay", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
			{Key: "void", From: []string{"draft", "open", "paid"}, To: "void"},
		},
	}
}

// statesAuditApp builds the invoices states app with the audit helper on,
// over the caller's database.
func statesAuditApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("invoices", entity.EntityConfig{
		Table:    "invoices",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   statesAuditFields(),
		States:   statesAuditStates(),
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{Actor: func(context.Context) string { return "alice" }})
	return app
}

// readAuditStateRows pulls every audit row in insertion order, reason
// included.
func readAuditStateRows(t *testing.T, db *sql.DB) []map[string]any {
	t.Helper()
	order := "created_at, id"
	if migrate.DetectDialect(db) == migrate.DialectSQLite {
		order = "rowid"
	}
	rows, err := db.Query("SELECT entity, op, record_id, reason, diff FROM audit_log ORDER BY " + order)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var ent, op, recordID string
		var reason, diff sql.NullString
		if err := rows.Scan(&ent, &op, &recordID, &reason, &diff); err != nil {
			t.Fatalf("scan: %v", err)
		}
		row := map[string]any{"entity": ent, "op": op, "record_id": recordID}
		if reason.Valid {
			row["reason"] = reason.String
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

// A move writes an audit row of its own: op "transition:<key>", with the
// old and the new status in the diff.
func TestAuditTransitionOp(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, statesAuditApp(t, db))

		resp := ta.Post("/invoices", map[string]any{"number": "INV-1", "status": "open"})
		resp.AssertStatus(t, 201)
		var created struct {
			Data map[string]any `json:"data"`
		}
		if err := resp.JSON(&created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		id, _ := created.Data["id"].(string)

		move := ta.Request(http.MethodPost, "/invoices/"+id+"/transitions/pay", nil).
			WithBody(map[string]any{}).Execute()
		move.AssertStatus(t, 200)

		rows := readAuditStateRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows (create + move), got %d (%+v)", len(rows), rows)
		}
		if rows[0]["op"] != "create" {
			t.Fatalf("first row op = %v, want create", rows[0]["op"])
		}
		if rows[1]["op"] != "transition:pay" {
			t.Fatalf("move row op = %v, want transition:pay", rows[1]["op"])
		}
		if rows[1]["record_id"] != id {
			t.Fatalf("move row record_id = %v, want %q", rows[1]["record_id"], id)
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[1]["diff"].(string)), &diff); err != nil {
			t.Fatalf("decode move diff: %v", err)
		}
		old, _ := diff["old"].(map[string]any)
		new, _ := diff["new"].(map[string]any)
		if old["status"] != "open" || new["status"] != "paid" {
			t.Fatalf("move diff statuses: old=%v new=%v, want open → paid", old["status"], new["status"])
		}
	})
}

// An override write is audited with its reason: op "state_override" on an
// update, op "create" on a create, the reason in the reason column both
// times.
func TestAuditStateOverrideOpAndReason(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := statesAuditApp(t, db)
		handler, err := app.CrudHandler("invoices")
		if err != nil {
			t.Fatal(err)
		}
		ctx := crud.WithStateOverride(context.Background(), "ledger import backfill")

		created, err := handler.CreateOne(ctx, map[string]any{"number": "INV-1", "status": "paid"})
		if err != nil {
			t.Fatalf("override create: %v", err)
		}
		if _, err := handler.UpdateOne(ctx, created["id"].(string), map[string]any{"status": "void"}); err != nil {
			t.Fatalf("override update: %v", err)
		}

		rows := readAuditStateRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d (%+v)", len(rows), rows)
		}
		if rows[0]["op"] != "create" {
			t.Fatalf("override create row op = %v, want create", rows[0]["op"])
		}
		if rows[0]["reason"] != "ledger import backfill" {
			t.Fatalf("override create row reason = %v", rows[0]["reason"])
		}
		if rows[1]["op"] != "state_override" {
			t.Fatalf("override update row op = %v, want state_override", rows[1]["op"])
		}
		if rows[1]["reason"] != "ledger import backfill" {
			t.Fatalf("override update row reason = %v", rows[1]["reason"])
		}
	})
}

// An override upsert that lands on an existing row is audited as the
// override it is, with its reason, not as a create.
func TestAuditStateOverrideUpsertOp(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := statesAuditApp(t, db)
		handler, err := app.CrudHandler("invoices")
		if err != nil {
			t.Fatal(err)
		}
		created, err := handler.CreateOne(context.Background(), map[string]any{"number": "INV-1", "status": "open"})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		id := created["id"].(string)
		ctx := crud.WithStateOverride(context.Background(), "ledger repair")
		if _, err := handler.UpsertOne(ctx, map[string]any{"id": id, "number": "INV-1", "status": "void"}); err != nil {
			t.Fatalf("override upsert: %v", err)
		}

		rows := readAuditStateRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d (%+v)", len(rows), rows)
		}
		if rows[1]["op"] != "state_override" || rows[1]["record_id"] != id {
			t.Fatalf("override upsert row = %+v, want op state_override on %s", rows[1], id)
		}
		if rows[1]["reason"] != "ledger repair" {
			t.Fatalf("override upsert row reason = %v", rows[1]["reason"])
		}
	})
}

// EnsureAuditTable widens a table created with the old column set (no
// tenant_id, no reason) so an existing audit table needs no manual
// migration.
func TestEnsureAuditTableAddsReasonAndTenant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ts := "DATETIME"
		if dialect == DialectPostgres {
			ts = "TIMESTAMPTZ"
		}
		if _, err := db.Exec(`CREATE TABLE audit_log (
			id TEXT PRIMARY KEY,
			entity TEXT NOT NULL,
			op TEXT NOT NULL,
			record_id TEXT NOT NULL,
			actor_id TEXT,
			created_at ` + ts + ` NOT NULL,
			diff TEXT
		)`); err != nil {
			t.Fatal(err)
		}
		// Twice: the second run finds both columns and adds nothing.
		for range 2 {
			if err := EnsureAuditTable(db, "audit_log"); err != nil {
				t.Fatalf("EnsureAuditTable: %v", err)
			}
		}
		live, err := migrate.ReadLiveColumns(context.Background(), db, "audit_log", dialect)
		if err != nil {
			t.Fatal(err)
		}
		for _, col := range []string{"tenant_id", "reason"} {
			if _, ok := live[col]; !ok {
				t.Fatalf("EnsureAuditTable did not add %s (live columns %v)", col, live)
			}
		}
	})
}

// A write with no override never names the reason column, so a table that
// predates only reason still takes every ordinary row; a write under an
// override puts its reason in the column when the table has one.
func TestAuditWriteNeverNamesReason(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ts := "DATETIME"
		if dialect == DialectPostgres {
			ts = "TIMESTAMPTZ"
		}
		// The pre-reason shape: tenant_id present, reason absent.
		if _, err := db.Exec(`CREATE TABLE audit_mid (
			id TEXT PRIMARY KEY,
			entity TEXT NOT NULL,
			op TEXT NOT NULL,
			record_id TEXT NOT NULL,
			actor_id TEXT,
			tenant_id TEXT,
			created_at ` + ts + ` NOT NULL,
			diff TEXT
		)`); err != nil {
			t.Fatal(err)
		}
		if err := writeAuditRow(context.Background(), db, "audit_mid", "invoices", auditOpCreate, "i1", "alice", nil); err != nil {
			t.Fatalf("write into a reason-less table: %v", err)
		}

		// The widened shape: an override's reason lands in the column,
		// and a write with no override still leaves it NULL.
		if _, err := db.Exec(`CREATE TABLE audit_new (
			id TEXT PRIMARY KEY,
			entity TEXT NOT NULL,
			op TEXT NOT NULL,
			record_id TEXT NOT NULL,
			actor_id TEXT,
			tenant_id TEXT,
			created_at ` + ts + ` NOT NULL,
			diff TEXT,
			reason TEXT
		)`); err != nil {
			t.Fatal(err)
		}
		ctx := crud.WithStateOverride(context.Background(), "repair job")
		if err := writeAuditRow(ctx, db, "audit_new", "invoices", "state_override", "i1", "alice", nil); err != nil {
			t.Fatalf("write with reason: %v", err)
		}
		var reason string
		if err := db.QueryRow(`SELECT reason FROM audit_new WHERE op = 'state_override'`).Scan(&reason); err != nil {
			t.Fatal(err)
		}
		if reason != "repair job" {
			t.Fatalf("reason = %q, want the override's reason", reason)
		}
		if err := writeAuditRow(context.Background(), db, "audit_new", "invoices", auditOpCreate, "i2", "alice", nil); err != nil {
			t.Fatal(err)
		}
		var nullReason sql.NullString
		if err := db.QueryRow(`SELECT reason FROM audit_new WHERE record_id = 'i2'`).Scan(&nullReason); err != nil {
			t.Fatal(err)
		}
		if nullReason.Valid {
			t.Fatalf("no-override write named reason: %q", nullReason.String)
		}
	})
}

// WithAuditLog marks each entity it records, the flag a state override
// requires.
func TestWithAuditLogMarksAudited(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("invoices", entity.EntityConfig{
			Table:    "invoices",
			Exposure: &entity.ExposureConfig{Public: true},
			Fields:   statesAuditFields(),
			States:   statesAuditStates(),
		}.WithTimestamps(false))
		ent, err := app.Registry.Get("invoices")
		if err != nil {
			t.Fatal(err)
		}
		if ent.Audited() {
			t.Fatal("entity reports audited before WithAuditLog")
		}
		app.WithAuditLog(AuditConfig{})
		if !ent.Audited() {
			t.Fatal("WithAuditLog did not mark the entity audited")
		}
	})
}

// Every version of a grouped entity shares the name-keyed audit hooks, so
// WithAuditLog marks every version audited, not only the one Registry.All
// picks: an override on /v2 lands in the same trail as one on /v1.
func TestWithAuditLogMarksEveryVersion(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		for _, v := range []string{"/v1", "/v2"} {
			app.GroupEntity(app.Group(v), "invoices", entity.EntityConfig{
				Table:    "invoices",
				Exposure: &entity.ExposureConfig{Public: true},
				Fields:   statesAuditFields(),
				States:   statesAuditStates(),
			}.WithTimestamps(false))
		}
		app.WithAuditLog(AuditConfig{})
		n := 0
		for _, ent := range app.Registry.AllSorted() {
			if ent.GetName() != "invoices" {
				continue
			}
			n++
			if !ent.Audited() {
				t.Fatalf("version %q not marked audited", ent.Version)
			}
		}
		if n != 2 {
			t.Fatalf("registered %d invoices versions, want 2", n)
		}
	})
}

// Two replicas booting on one old audit table both read reason as missing
// and both add it. The second add meets a column that is already there and
// must pass: a stale catalog read is not a failed migration.
func TestAuditColumnsStaleCatalog(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if err := EnsureAuditTable(db, "audit_race"); err != nil {
			t.Fatal(err)
		}
		dialect := migrate.DetectDialect(db)
		stale := map[string]string{"id": "TEXT", "entity": "TEXT"}
		if err := addAuditColumns(db, "audit_race", dialect, stale); err != nil {
			t.Fatalf("second replica's add on a stale catalog: %v", err)
		}
		if err := EnsureAuditTable(db, "audit_race"); err != nil {
			t.Fatalf("re-run: %v", err)
		}
	})
}
