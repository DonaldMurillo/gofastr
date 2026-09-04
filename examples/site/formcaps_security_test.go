package main

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

// TestDemoFormBodiesCapped pins the sibling contract the /__site handlers
// already carry ("an uncapped POST is a memory lever on a public origin"):
// every public POST handler that parses a form caps its body. 4 MiB is
// deliberately below go1.27's 10 MiB stdlib urlencoded floor so the floor
// cannot mask the missing per-handler cap.
func TestDemoFormBodiesCapped(t *testing.T) {
	big := strings.Repeat("x", 4<<20)

	if code := postForm(t, "/__site/palette", "q="+big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /__site/palette with 4 MiB urlencoded body = %d, want 413: servePaletteSearch must wrap the body in http.MaxBytesReader before ParseForm (like the sibling /__site handlers) so each request cannot buffer its full body", code)
	}

	if code := postForm(t, "/forms/wizard", "wizard_action=next&_step=2&wd_name="+big); code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /forms/wizard with 4 MiB urlencoded body = %d, want 413: WizardDemoHandler must wrap the body in http.MaxBytesReader before ParseForm — it is a sitemap-listed public form", code)
	}

	// Legitimate small bodies still answer 200 (the cap must not refuse
	// the demo's own traffic).
	if code := postForm(t, "/__site/palette", "q=docs"); code != http.StatusOK {
		t.Errorf("POST /__site/palette with a small body = %d, want 200", code)
	}
	if code := postForm(t, "/forms/wizard", "wizard_action=next&_step=0&wd_name=abc"); code != http.StatusOK {
		t.Errorf("POST /forms/wizard with a small body = %d, want 200", code)
	}
}

// TestDemoSubmitDecodesStrictly pins the interactive/submit JSON decode:
// the body is client bytes, so it goes through handler.DecodeStrict — a
// duplicate or case-folded top-level key ({"message":"a","Message":"b"})
// is ambiguous input and must be refused, not silently resolved by the
// decoder's last-key-wins rule.
func TestDemoSubmitDecodesStrictly(t *testing.T) {
	app := newTestApp(t)
	post := func(body, ctype string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/__site/interactive/submit", strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(`{"message":"hello"}`, "application/json"); code != http.StatusOK {
		t.Errorf("POST /__site/interactive/submit with one message key = %d, want 200", code)
	}
	if code := post(`{"message":"a","message":"b"}`, "application/json"); code != http.StatusBadRequest {
		t.Errorf("POST /__site/interactive/submit with a duplicate message key = %d, want 400: the decode must go through handler.DecodeStrict so key ambiguity is refused", code)
	}
	if code := post(`{"message":"a","Message":"b"}`, "application/json"); code != http.StatusBadRequest {
		t.Errorf("POST /__site/interactive/submit with a case-folded duplicate key = %d, want 400: encoding/json matches struct tags case-insensitively, so DecodeStrict must refuse the ambiguity", code)
	}
}
