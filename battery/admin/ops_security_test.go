package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A form RPC body with a repeated or case-folded key reads two ways: the
// JSON surface keeps the last "role", the form surface the first. The
// ops post refuses the body rather than pick one.
func TestOpsRPCRefusesAmbiguousKeys(t *testing.T) {
	r := newRBACEnv(t, Config{})
	for _, body := range []string{
		`{"role":"viewer","permission":"posts:write","role":"editor"}`,
		`{"role":"viewer","ROLE":"editor","permission":"posts:write"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/admin/rbac/_grant", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if rr := serve(r.as(theAdmin), req); rr.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, rr.Code)
		}
	}
	for _, role := range []string{"viewer", "editor"} {
		if canAs(r.policy, role, "posts:write") {
			t.Errorf("an ambiguous post granted %s posts:write", role)
		}
	}
}
