package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// stubJobs is a JobRunner that never runs anything: these tests only
// need EntityUI to see that the app queues bulk runs.
type stubJobs struct{}

func (stubJobs) Enqueue(context.Context, entityui.BulkJob) error { return nil }
func (stubJobs) Principal(ctx context.Context, _ entityui.BulkJob) (context.Context, error) {
	return ctx, nil
}

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func panicText(t *testing.T, f func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("did not panic")
		}
		msg, _ = r.(string)
	}()
	f()
	return ""
}

// Queued runs keep their selection in the database, so Jobs on an app
// with none is a boot error, not a 500 on the first big selection.
func TestEntityUIJobsNeedDatabase(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	msg := panicText(t, func() { app.EntityUI(entityui.Extensions{Jobs: stubJobs{}}) })
	if !strings.Contains(msg, "needs a database") {
		t.Fatalf("panic = %q, want the needs-a-database refusal", msg)
	}
}

// With Jobs, EntityUI creates the snapshot tables; without, it creates
// none and the host offers no store.
func TestEntityUIJobsCreateSnapshotTables(t *testing.T) {
	db := memDB(t)
	app := entityUIAuditApp(t, db, "notes", false)
	if (entityUIHost{a: app}).BulkStore() != nil {
		t.Fatal("a host with no store offered one")
	}
	app.EntityUI(entityui.Extensions{Jobs: stubJobs{}})
	for _, table := range []string{bulkJobsTable, bulkItemsTable} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Errorf("%s missing after EntityUI with Jobs: %v", table, err)
		}
	}
}

// A bad extension name is a boot panic that names the framework call.
func TestEntityUIBadExtensionPanics(t *testing.T) {
	app := entityUIAuditApp(t, memDB(t), "notes", false)
	msg := panicText(t, func() {
		app.EntityUI(entityui.Extensions{Entities: map[string]entityui.Extension{"ghost": {}}})
	})
	if !strings.Contains(msg, "framework: EntityUI") || !strings.Contains(msg, "ghost") {
		t.Fatalf("panic = %q, want it to name EntityUI and the bad entity", msg)
	}
}

// An entity with CRUD turned off has no write routes, so EntityUI mounts
// no bulk or export route for it.
func TestEntityUISkipsEntityWithoutCRUD(t *testing.T) {
	db := memDB(t)
	off := false
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("logs", entity.EntityConfig{
		Table:    "logs",
		Fields:   []schema.Field{{Name: "line", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true, CRUD: &off},
	}.WithTimestamps(false))
	app.EntityUI(entityui.Extensions{})
	logs, err := app.Registry.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	if app.entityUIMounted(logs) {
		t.Fatal("entityUIMounted reports a CRUD-off entity")
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/logs/_export.csv", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("export of a CRUD-off entity answered 200: %s", rec.Body.String())
	}
}

// A view mounts read routes only, so it gets no bulk or export route.
func TestEntityUISkipsReadOnlyView(t *testing.T) {
	app := NewApp(WithDB(memDB(t)), WithoutDefaultMiddleware())
	app.View(migrate.View{
		Name:   "events_view",
		Select: "SELECT 'a' AS id",
		Columns: []migrate.Column{
			{Name: "id", Type: schema.String, PrimaryKey: true},
		},
	})
	app.EntityUI(entityui.Extensions{})
	view, err := app.Registry.Get("events_view")
	if err != nil {
		t.Fatal(err)
	}
	if app.entityUIMounted(view) {
		t.Fatal("entityUIMounted reports a read-only view")
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events_view/_export.csv", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("export of a read-only view answered 200: %s", rec.Body.String())
	}
}

// With no audit log the host reads no trail and writes no bulk row.
func TestEntityUIHostWithoutAuditLog(t *testing.T) {
	app := NewApp(WithDB(memDB(t)), WithoutDefaultMiddleware())
	h := entityUIHost{a: app}
	if h.Audit() != nil {
		t.Fatal("an app with no audit log exposed an AuditReader")
	}
	if err := h.AuditEvent(context.Background(), "notes", "bulk", "", map[string]any{"done": 1}); err != nil {
		t.Fatalf("AuditEvent with no audit log: %v", err)
	}
}

// The trail refuses an unsafe table name, clamps a limit outside 1..200,
// and reports a diff it cannot parse instead of dropping it.
func TestAuditTrailEdges(t *testing.T) {
	db := memDB(t)
	app := entityUIAuditApp(t, db, "notes", false)
	ctx := context.Background()
	id := createNote(t, app, "notes", ctx, map[string]any{"title": "first"})

	if _, err := (auditTrail{db: db, table: "audit log; --"}).Trail(ctx, "notes", id, 10); err == nil {
		t.Fatal("an unsafe audit table name was queried")
	}
	trail := entityUIHost{a: app}.Audit()
	if got, err := trail.Trail(ctx, "notes", id, 0); err != nil || len(got) != 1 {
		t.Fatalf("limit 0 = %d entries, %v; want the one row under the default limit", len(got), err)
	}
	if _, err := db.Exec(`UPDATE audit_log SET diff = 'not json' WHERE record_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := trail.Trail(ctx, "notes", id, 10); err == nil || !strings.Contains(err.Error(), "diff") {
		t.Fatalf("malformed diff = %v, want an audit row diff error", err)
	}
}

// TestAuditTrailKeepsBigIntegers: a diff number past 2^53 reaches the
// record's activity with its stored digits, not a rounded float64.
func TestAuditTrailKeepsBigIntegers(t *testing.T) {
	db := memDB(t)
	app := entityUIAuditApp(t, db, "notes", false)
	ctx := context.Background()
	id := createNote(t, app, "notes", ctx, map[string]any{"title": "first"})
	diff := `{"old":{"n":9007199254740993},"new":{"n":3}}`
	if _, err := db.Exec(`UPDATE audit_log SET diff = $1 WHERE record_id = $2`, diff, id); err != nil {
		t.Fatal(err)
	}
	got, err := entityUIHost{a: app}.Audit().Trail(ctx, "notes", id, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("Trail = %d entries, %v; want 1", len(got), err)
	}
	b, err := json.Marshal(map[string]any{"old": got[0].Before, "new": got[0].After})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"new":{"n":3},"old":{"n":9007199254740993}}`; string(b) != want {
		t.Fatalf("trail diff = %s, want %s", b, want)
	}
}

// AppendAuditEvent writes to audit_log when no table is named, and
// refuses a detail map it cannot encode instead of writing a NULL diff.
func TestAppendAuditEventDefaultsAndRefusals(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	if err := EnsureAuditTable(db, "audit_log"); err != nil {
		t.Fatal(err)
	}
	if err := AppendAuditEvent(ctx, db, "", "notes", "bulk", "", "alice", map[string]any{"done": 1}); err != nil {
		t.Fatalf("default table: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE op = 'bulk'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit_log bulk rows = %d, %v; want 1", n, err)
	}
	if err := AppendAuditEvent(ctx, db, "", "notes", "bulk", "", "alice", map[string]any{"bad": make(chan int)}); err == nil || !strings.Contains(err.Error(), "marshal diff") {
		t.Fatalf("unencodable diff = %v, want a marshal error", err)
	}
}

// putAuditRow writes one audit row at a fixed time, as the hooks would.
func putAuditRow(t *testing.T, db *sql.DB, id, ent, op, recordID string, at time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO audit_log (id, entity, op, record_id, created_at) VALUES ($1, $2, $3, $4, $5)`,
		id, ent, op, recordID, at.UTC()); err != nil {
		t.Fatal(err)
	}
}

// A key reused after the first record's hard delete starts a new
// trail: the Activity tab does not show the earlier record's rows.
// The bound is the last end a later row follows, so a deleted key
// still reads its own last life.
func TestAuditTrailStartsAfterDelete(t *testing.T) {
	db := memDB(t)
	app := entityUIAuditApp(t, db, "notes", false)
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for i, op := range []string{"create", "update", "delete", "create", "update"} {
		putAuditRow(t, db, fmt.Sprintf("aud_%d", i), "notes", op, "x", t0.Add(time.Duration(i)*time.Minute))
	}
	got, err := entityUIHost{a: app}.Audit().Trail(context.Background(), "notes", "x", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Operation != "update" || got[1].Operation != "create" {
		t.Fatalf("trail = %+v, want the second record's update and create", got)
	}
	// Once the second record is deleted too, its own life stays readable.
	putAuditRow(t, db, "aud_5", "notes", "delete", "x", t0.Add(5*time.Minute))
	got, err = entityUIHost{a: app}.Audit().Trail(context.Background(), "notes", "x", 10)
	if err != nil || len(got) != 3 || got[0].Operation != "delete" || got[2].Operation != "create" {
		t.Fatalf("trail after the second delete = %+v, %v; want its delete, update and create", got, err)
	}
}

// On a soft-delete entity a delete is undone by restore, so the trail
// keeps the rows before it; a purge ends the record.
func TestAuditTrailSoftDeleteKeepsHistory(t *testing.T) {
	db := memDB(t)
	cfg := entity.EntityConfig{
		Table:    "notes",
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		Exposure: &entity.ExposureConfig{Public: true},
		Scope:    &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("notes", cfg)
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.WithAuditLog(AuditConfig{})
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for i, op := range []string{"create", "delete", "restore", "update"} {
		putAuditRow(t, db, fmt.Sprintf("aud_%d", i), "notes", op, "x", t0.Add(time.Duration(i)*time.Minute))
	}
	trail := entityUIHost{a: app}.Audit()
	if got, err := trail.Trail(context.Background(), "notes", "x", 10); err != nil || len(got) != 4 {
		t.Fatalf("trail = %d entries, %v; want all 4 across the restore", len(got), err)
	}
	putAuditRow(t, db, "aud_p", "notes", "purge", "x", t0.Add(10*time.Minute))
	putAuditRow(t, db, "aud_c", "notes", "create", "x", t0.Add(11*time.Minute))
	if got, err := trail.Trail(context.Background(), "notes", "x", 10); err != nil || len(got) != 1 || got[0].Operation != "create" {
		t.Fatalf("trail after purge = %+v, %v; want only the new record's create", got, err)
	}
}
