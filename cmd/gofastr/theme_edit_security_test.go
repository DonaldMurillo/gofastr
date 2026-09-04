package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Property: POST /__theme/apply decodes its {key,value} pair strictly —
// a body carrying duplicate or case-folded keys is refused (400) and
// must not touch the working theme, because stdlib json's last-wins
// resolution would apply (and writeBack would persist) a value a
// reviewer reading the first occurrence never saw.
func TestThemeApplyRejectsDuplicateKeys(t *testing.T) {
	srv := newTestServer(t)
	before := srv.working.Colors.Primary.Value

	// First occurrence says #00FF00, the smuggled last value says #FF0000.
	body := `{"key":"color-primary","value":"#00FF00","key":"color-primary","value":"#FF0000"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /__theme/apply with duplicate key/value pairs returned %d %q — last-wins smuggled the second value; want 400", rec.Code, rec.Body.String())
	}
	if after := srv.working.Colors.Primary.Value; after != before {
		t.Errorf("duplicate-key body mutated color-primary %q → %q; an unparseable-trust body must not touch the working theme that writeBack persists", before, after)
	}
}

// TestThemeApplyRejectsCaseFoldedKeys: the folded spellings "Key"/"Value"
// bind the same fields case-insensitively under stdlib json — the
// smuggled last value must not be applied either.
func TestThemeApplyRejectsCaseFoldedKeys(t *testing.T) {
	srv := newTestServer(t)
	before := srv.working.Colors.Primary.Value

	body := `{"key":"color-primary","value":"#00FF00","Key":"color-primary","Value":"#FF0000"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /__theme/apply with case-folded Key/Value keys returned %d %q — the folded spelling still binds the field last-wins; want 400", rec.Code, rec.Body.String())
	}
	if after := srv.working.Colors.Primary.Value; after != before {
		t.Errorf("case-folded body mutated color-primary %q → %q; an unparseable-trust body must not touch the working theme that writeBack persists", before, after)
	}
}
