package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Found by the 2026-11-01 battery/auth bug bash. With the documented
// wiring, app.Entity("sessions", SessionEntityConfig()), AutoMigrate made
// the table before EnsureSchema ran, so the store's CREATE TABLE (which
// carried token UNIQUE) did nothing: token had no index and every cookie
// lookup, a random one included, scanned the whole table.
func TestSessionTableIndexedUnderDocumentedWiring(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("users", UserEntityConfig())
	app.Entity("sessions", SessionEntityConfig())
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	sessions := NewEntitySessionStore(db, "sessions")
	if err := sessions.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]string{
		`EXPLAIN QUERY PLAN SELECT 1 FROM sessions WHERE token = 'x'`:   "INDEX",
		`EXPLAIN QUERY PLAN SELECT 1 FROM sessions WHERE user_id = 'x'`: "INDEX",
	} {
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !strings.Contains(plan.String(), want) {
			t.Errorf("%s: plan %q, want an index", q, plan.String())
		}
	}
	// Token uniqueness holds whoever made the table.
	if _, err := db.Exec(`INSERT INTO sessions (id, token, user_id, created_at, expires_at) VALUES ('a','dup','u',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP), ('b','dup','u',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err == nil {
		t.Error("two sessions share a token")
	}
}

// One unique index on token, not two: the inline UNIQUE and the
// EnsureSchema index each built one (2026-11-01 review).
func TestSessionTableHasOneTokenIndex(t *testing.T) {
	for _, viaAutoMigrate := range []bool{false, true} {
		ctx := context.Background()
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		if viaAutoMigrate {
			app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
			app.Entity("sessions", SessionEntityConfig())
			if err := framework.AutoMigrate(db, app.Registry); err != nil {
				t.Fatal(err)
			}
		}
		if err := NewEntitySessionStore(db, "sessions").EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('sessions') l
			JOIN pragma_index_info(l.name) i WHERE l."unique" = 1 AND i.name = 'token'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("automigrate=%v: %d unique indexes on token, want 1", viaAutoMigrate, n)
		}
	}
}
