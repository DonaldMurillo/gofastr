package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// newTourApp drives main.go's real entity declarations (buildApp) minus
// the listener, so the tour's actual route table answers in-process and
// a posture regression in main.go itself is what fails here.
func newTourApp(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "api-tour.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	app := buildApp(db, t.TempDir())

	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	seedDemoData(db)
	return app
}

// TestUsersWritesNotWorldWritable pins the file's own stated rule for
// per-user data ("per-user data must never be world-writable", the
// profiles declaration): the users account graph — names plus avatar
// uploads — must refuse anonymous create/update/delete. With no RBAC
// policy installed the Access permissions fail closed; only reads stay
// open for the anonymous tour.
func TestUsersWritesNotWorldWritable(t *testing.T) {
	app := newTourApp(t)
	do := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Reads stay open: the tour is anonymous by design.
	if code, _ := do(http.MethodGet, "/users", ""); code != http.StatusOK {
		t.Errorf("GET /users = %d, want 200: the tour keeps anonymous reads", code)
	}

	// Anonymous create: an account row minted with no authentication. The
	// created id feeds the delete leg — the seeded users carry FK references
	// (profile, posts), and this test must fail on authorization, not on an
	// accidental FK guard.
	code, body := do(http.MethodPost, "/users", `{"name":"anonymous takeover"}`)
	if code < 400 {
		t.Errorf("anonymous POST /users = %d: the account graph is world-writable — users must gate writes with Access permissions like profiles in the same file. Want 401/403", code)
	}
	freshID := ""
	if i := strings.Index(body, `"id":"`); i >= 0 {
		rest := body[i+6:]
		if j := strings.Index(rest, `"`); j >= 0 {
			freshID = rest[:j]
		}
	}

	// Anonymous update of the seeded Alice account.
	if code, _ = do(http.MethodPatch, "/users/u1", `{"name":"renamed by stranger"}`); code < 400 {
		t.Errorf("anonymous PATCH /users/u1 = %d: any caller rewrites any account. Want 401/403", code)
	}

	// Anonymous delete of the account this same test just created.
	if freshID != "" {
		if code, _ = do(http.MethodDelete, "/users/"+freshID, ""); code < 400 {
			t.Errorf("anonymous DELETE /users/%s = %d: any caller deletes any account. Want 401/403", freshID, code)
		}
	} else if code < 400 {
		t.Logf("created-row id not found in POST response (%.120s); delete leg skipped", body)
	}
}
