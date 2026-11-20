package themecmd

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
	// First occurrence says #00FF00, the smuggled last value says #FF0000.
	rejectSmuggledApply(t,
		`{"key":"color-primary","value":"#00FF00","key":"color-primary","value":"#FF0000"}`,
		"duplicate key/value pairs", "last-wins smuggled the second value")
}

// TestThemeApplyRejectsCaseFoldedKeys: the folded spellings "Key"/"Value"
// bind the same fields case-insensitively under stdlib json — the
// smuggled last value must not be applied either.
func TestThemeApplyRejectsCaseFoldedKeys(t *testing.T) {
	rejectSmuggledApply(t,
		`{"key":"color-primary","value":"#00FF00","Key":"color-primary","Value":"#FF0000"}`,
		"case-folded Key/Value keys", "the folded spelling still binds the field last-wins")
}

// rejectSmuggledApply posts body to /__theme/apply and requires a 400 that
// leaves the working theme untouched.
func rejectSmuggledApply(t *testing.T, body, what, why string) {
	t.Helper()
	srv := newTestServer(t)
	before := srv.Working.Colors.Primary.Value

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /__theme/apply with %s returned %d %q — %s; want 400", what, rec.Code, rec.Body.String(), why)
	}
	if after := srv.Working.Colors.Primary.Value; after != before {
		t.Errorf("%s body mutated color-primary %q → %q; an unparseable-trust body must not touch the working theme that writeBack persists", what, before, after)
	}
}
