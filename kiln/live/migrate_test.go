package live_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Live's rebuild auto-migrates so a freshly-added entity is queryable
// without a manual AutoMigrate call.
func TestLiveAutoMigratesOnEntityAdd(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-mig")
	if err != nil {
		t.Fatalf("ephemeral: %v", err)
	}
	defer cleanup()

	factory := func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	}
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}

	posts := &world.Entity{
		Name: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
		},
	}
	entry, _ := journal.NewEntry("1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})
	if err := l.Apply(entry); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Table should exist; GET /api/posts should return 200 (empty list).
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

// After adding a field to an existing entity, the column shows up via the
// schema (queries that reference it succeed).
func TestLiveAddsFieldColumn(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-field")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	factory := func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	}
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}

	posts := &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
	}
	entry, _ := journal.NewEntry("1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: posts})
	if err := l.Apply(entry); err != nil {
		t.Fatal(err)
	}

	addField, _ := journal.NewEntry("2", time.Now(), journal.KindWorldEdit, journal.OpAddField,
		journal.AddFieldPayload{Entity: "posts", Field: world.Field{Name: "body", Type: "text"}})
	if err := l.Apply(addField); err != nil {
		t.Fatalf("add field: %v", err)
	}

	// Schema-level check: PRAGMA table_info should now include 'body'.
	rows, err := d.Query(`PRAGMA table_info(posts)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	cols := []string{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, name)
	}
	if !contains(cols, "body") {
		t.Errorf("expected 'body' column after add_field, got %v", cols)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestLiveStartsWithoutEntitiesIsHealthy(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	factory := func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	}
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatalf("live.New on empty world: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	// 404 is expected (no entities, no openapi).
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 with no entities, got %d body=%q", rec.Code, rec.Body.String())
	}
	_ = strings.Contains
}
