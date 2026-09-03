//go:build red

package main

// RED TEST — open finding, 2026-09-03 adversarial pass round 8 (tests-only; no fix applied).
// TIER: EXAMPLE-APP POSTURE — this pins examples/site's demo handlers (the
// deployed product site for the framework), not framework code. The
// framework/body-auth tier of the same property family is separately pinned
// (battery/setup/body_limit_red_test.go, battery/auth caps); respected, not
// re-derived.
// Property: a handler that buffers its request body must cap it — an
// uncapped urlencoded POST is a memory-amplification lever on a public
// origin (CWE-400 uncontrolled resource consumption). net/http's
// r.ParseForm() reads the ENTIRE urlencoded body into memory with no
// default limit; the go1.27 default-form floor is 10 MiB, so a 4 MiB body
// sails under it and is fully buffered.
// Surface: two public POST routes in this app, while their /__site siblings
// cap the same shape and name the threat in comments:
//   - main.go servePaletteSearch (POST /__site/palette, mounted :264,
//     handler :776): `_ = r.ParseForm()` — no MaxBytesReader, error
//     discarded, 200 written. Compare the sibling /__site/interactive/submit
//     (:408-422, comment: "an uncapped POST is a memory lever on a public
//     origin") and /__site/sortable/move (:432-440), both capped at 4 KiB.
//   - screen_wizard.go WizardDemoHandler (POST /forms/wizard, mounted
//     :632-633, handler :76): r.ParseForm() with no cap on a sitemap-listed
//     public form. The value echoes themselves are escaped (render.Tag/
//     html.Input escape attributes — verified); the finding is buffering,
//     not XSS.
// Vulnerable path, driven below through the site's real router: a 4 MiB
// well-formed urlencoded body to each route. Today both buffer it whole and
// answer 200; each request burns its body size in server memory per
// concurrent request.
// Severity: LOW — DoS-class memory amplification on a public demo origin,
// consistent with the codebase's own sibling contract (which is what makes
// it a defect here rather than a demo tradeoff).
// Fix direction: wrap both with the siblings' exact shape —
// r.Body = http.MaxBytesReader(w, r.Body, 4<<10) before ParseForm, mapping
// the overrun to 413 (palette should also stop discarding the error).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postForm drives one urlencoded POST through the site's real router
// (same entry the deployed server uses) and returns the response code.
func postForm(t *testing.T, target, body string) int {
	t.Helper()
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	return rec.Code
}

// TestSiteDemoRedCapsFormBodies pins the sibling contract: every public
// POST handler that parses a form caps its body. 4 MiB is deliberately
// below go1.27's 10 MiB stdlib urlencoded floor so the floor cannot mask
// the missing per-handler cap. RED today: both routes answer 200 with the
// full body buffered.
func TestSiteDemoRedCapsFormBodies(t *testing.T) {
	// Stay under the 10 MiB stdlib default-form floor; 4 MiB still proves
	// the handler buffers an unbounded attacker-chosen body.
	big := strings.Repeat("x", 4<<20)

	if code := postForm(t, "/__site/palette", "q="+big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /__site/palette with 4 MiB urlencoded body = %d, want 413: servePaletteSearch (main.go:776-777) calls r.ParseForm() with no MaxBytesReader and discards the error, while sibling /__site handlers cap the identical shape at 4 KiB and name the threat (\"an uncapped POST is a memory lever on a public origin\", main.go:409-412). The full body is buffered per request", code)
	}

	if code := postForm(t, "/forms/wizard", "wizard_action=next&_step=2&wd_name="+big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /forms/wizard with 4 MiB urlencoded body = %d, want 413: WizardDemoHandler (screen_wizard.go:81-85) calls r.ParseForm() with no MaxBytesReader on a sitemap-listed public form. The full body is buffered per request", code)
	}
}
