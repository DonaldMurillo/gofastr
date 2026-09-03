//go:build red

package admin

// RED TESTS — open finding, 2026-09-03 adversarial pass round 6 (tests-only; no fix applied).
// Property: every authenticated request-body surface enforces a size cap, so a
// single request cannot park unbounded state in process memory. The auth
// battery pins this via http.MaxBytesReader before ParseForm
// (form_decode.go:71-78, 1 MiB) and before every JSON decode
// (json_limit.go:54); crud's Bind caps at 1 MiB too (core/handler/bind.go:95).
// entitySave is the one authenticated surface that calls r.ParseForm() bare.
// Surface: entity_admin.go entitySave:559 — r.ParseForm() with no
// MaxBytesReader wrapper, no Content-Length guard. Sibling admin form
// mutations (rbac, modules, queue replay) take bounded enumerated fields;
// entitySave accepts arbitrary attacker-sized field values.
// Finding (observed, not inferred): a 4 MiB urlencoded edit form
// (deliberately BELOW go1.27's 10 MiB stdlib urlencoded floor, above which
// net/http itself 400s and the test would be a false red — floor verified
// in battery/setup, R4AdminSetup) is parsed in full by ParseForm, then —
// because the downstream crud Bind cap rejects the oversized JSON — the
// error-flash path stashes the submitted values, 4 MiB string included,
// into the in-memory flashStore until flashTTL (entity_admin.go:570-615,
// 772-775) and answers 303. The row write is incidentally stopped by the
// crud cap; the surface itself never refuses, and each concurrent request
// buffers its full body plus parks it in the flash map.
// Severity: P3 — authenticated surface (b.gate): resource exhaustion and
// unbounded flash parking, not row persistence and not privilege.
// Fix direction: wrap r.Body = http.MaxBytesReader(w, r.Body, cap) before
// ParseForm and map *http.MaxBytesError to 413, mirroring
// decodeAuthCredentials (form_decode.go:71-78). Cap can be generous (entity
// forms legitimately carry descriptions, not megabytes).

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestEntitySaveRedCapsBody(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	if _, err := db.Exec(`INSERT INTO posts (id, title, body, published, status) VALUES ('p1', 'Before', 'b', 0, 'draft')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// save posts an authenticated same-origin urlencoded edit form of the
	// given total body size; the filler is a real editable "body" field
	// value, not an unknown key ParseForm would skip past.
	save := func(title string, total int) *httptest.ResponseRecorder {
		vals := url.Values{}
		vals.Set("title", title)
		vals.Set("status", "draft")
		filler := total - len(vals.Encode())
		if filler > 0 {
			vals.Set("body", strings.Repeat("A", filler))
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/e/posts/_update/p1", strings.NewReader(vals.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Host = "app.example.com"
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "https://app.example.com")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	// Positive control: a normal-size save is processed end to end —
	// otherwise the harness, not the cap, is what refuses.
	if rr := save("Legit", 0); rr.Code != http.StatusSeeOther {
		t.Fatalf("setup: normal save got %d (body=%s), want 303 — harness broken, not the seam", rr.Code, rr.Body.String())
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title); err != nil || title != "Legit" {
		t.Fatalf("setup: normal save not persisted (title=%q err=%v) — harness broken, not the seam", title, err)
	}
	// The oversized request: 4 MiB urlencoded body, under the stdlib floor
	// so any refusal has to come from the handler's own cap.
	rr := save("PWNED", 4<<20)
	var bodyLen int
	_ = db.QueryRow(`SELECT length(body) FROM posts WHERE id='p1'`).Scan(&bodyLen)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("SECURITY: [admin-bodycap-entity] POST /admin/e/posts/_update/p1 with a 4 MiB urlencoded body returned %d (row body length still %d) — "+
			"entitySave calls r.ParseForm() with no MaxBytesReader (entity_admin.go:559), the only authenticated body surface in the batteries without its own cap "+
			"(auth siblings cap at 1 MiB via form_decode.go:71-78; the row is only saved by crud's downstream Bind cap): each request buffers its full body "+
			"and the error-flash path parks the submitted values, megabytes included, in the in-memory flashStore until flashTTL",
			rr.Code, bodyLen)
	}
}
