//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: a state-changing JSON endpoint that decodes a {Key,Value} pair must
// reject duplicate keys; stdlib encoding/json silently keeps the LAST value,
// so a reviewer (or an intercepting proxy log) reading the first occurrence
// sees a different theme edit than the one applied and written to disk.
// Surface: cmd/gofastr/theme_edit.go handleApply (:466) — bearer+origin-guarded
// loopback endpoint; Decode error is checked (400 on malformed) but the plain
// json.Decoder accepts {"key":K,"value":V1,"key":K,"value":V2} with last-wins.
// Finding: POST /__theme/apply with duplicate key/value pairs returns 200 and
// mutates the working theme with the smuggled second value; writeBack then
// persists that value into the operator's theme.go. core/handler/bind.go's
// validateBodyKeys (duplicate + unknown key rejection) is the repo's pinned
// convention for exactly this shape (bind_strict_keys_security_test.go).
// Severity: operator-local dev tool (loopback bind, per-process bearer,
// Origin gate), so the vector needs a compromised local client or a
// body-mangling proxy; still a last-wins write to a source file.
// Fix direction: pre-validate top-level keys (duplicate rejection) before the
// Decode, e.g. reuse core/handler's validateBodyKeys shape.
// Round-6 mechanism split: exact duplicates and case-folded spellings are
// separate top-level tests below (independently fixable mechanisms).
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGofastrThemeRedRejectsDuplicateKeys: exact duplicate key/value pairs
// — wire-level last-wins.
func TestGofastrThemeRedRejectsDuplicateKeys(t *testing.T) {
	srv := newTestServer(t)
	before := srv.working.Colors.Primary.Value

	// First occurrence says #00FF00, the smuggled last value says #FF0000.
	// A plain json.Decoder applies the last pair and 200s.
	body := `{"key":"color-primary","value":"#00FF00","key":"color-primary","value":"#FF0000"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [theme-apply-strict-keys] POST /__theme/apply with duplicate key/value pairs returned %d %q — last-wins smuggled the second value; want 400", rec.Code, rec.Body.String())
	}
	if after := srv.working.Colors.Primary.Value; after != before {
		t.Errorf("SECURITY: [theme-apply-strict-keys] duplicate-key body mutated color-primary %q → %q; an unparseable-trust body must not touch the working theme that writeBack persists", before, after)
	}
}

// TestGofastrThemeRedRejectsCaseFoldedKeys: handleApply's request struct
// has untagged Key/Value fields, so the folded spellings "Key"/"Value"
// bind the same fields case-insensitively — the last-wins #FF0000 is
// applied and 200s. Survives a dedup-only fix.
func TestGofastrThemeRedRejectsCaseFoldedKeys(t *testing.T) {
	srv := newTestServer(t)
	before := srv.working.Colors.Primary.Value

	body := `{"key":"color-primary","value":"#00FF00","Key":"color-primary","Value":"#FF0000"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [theme-apply-strict-keys] POST /__theme/apply with case-folded Key/Value keys returned %d %q — the folded spelling still binds the field last-wins; want 400", rec.Code, rec.Body.String())
	}
	if after := srv.working.Colors.Primary.Value; after != before {
		t.Errorf("SECURITY: [theme-apply-strict-keys] case-folded body mutated color-primary %q → %q; an unparseable-trust body must not touch the working theme that writeBack persists", before, after)
	}
}
