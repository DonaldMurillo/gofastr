package middleware_test

import (
	"bufio"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Erasing a user deletes the responses the idempotency store cached
// for them and keeps everyone else's (S28). A data export carries the
// table under its configured name.
func TestEraseRemovesCachedReplays(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := middleware.NewSQLIdempotencyStore(db, middleware.WithSQLIdempotencyTable("replays"))
	if err != nil {
		t.Fatal(err)
	}
	mw := middleware.Idempotency(middleware.IdempotencyConfig{
		Store:     store,
		Principal: func(r *http.Request) string { return r.Header.Get("X-User") },
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("order for " + r.Header.Get("X-User")))
	}))
	for _, user := range []string{"victim", "bystander"} {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("{}"))
		req.Header.Set("Idempotency-Key", "k1")
		req.Header.Set("X-User", user)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", user, rec.Code)
		}
	}
	ctx := context.Background()
	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if f, err := os.Open(filepath.Join(dir, "idempotency_keys.ndjson")); err != nil {
		t.Errorf("the export has no idempotency_keys: %v", err)
	} else {
		n := 0
		for sc := bufio.NewScanner(f); sc.Scan(); {
			n++
		}
		_ = f.Close()
		if n != 2 {
			t.Errorf("export holds %d replay(s), want 2", n)
		}
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	var bodies []string
	rows, err := db.Query(`SELECT body FROM replays`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(b))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if len(bodies) != 1 || bodies[0] != "order for bystander" {
		t.Errorf("replays after erase = %q, want only the bystander's", bodies)
	}
}

// A table an earlier release created gains the principal column, and
// opening the store again over it does not fail.
func TestIdemStoreAddsPrincipalColumn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE idempotency_keys (key TEXT PRIMARY KEY, fingerprint TEXT NOT NULL,
		status INTEGER, headers TEXT, body BLOB, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := middleware.NewSQLIdempotencyStore(db); err != nil {
			t.Fatalf("NewSQLIdempotencyStore over an old table: %v", err)
		}
	}
	if _, err := db.Exec(`SELECT principal FROM idempotency_keys`); err != nil {
		t.Errorf("principal column missing: %v", err)
	}
}
