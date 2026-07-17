package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// capturingDecider records the last capability + Ref it was asked about so a
// test can assert that the CRUD permission gate consulted the decider with the
// resource the handler was operating on.
type capturingDecider struct {
	cap  access.Permission
	ref  access.Ref
	seen bool
	ret  access.Decision
}

func (c *capturingDecider) fn(_ context.Context, _ []string, capability access.Permission, resource access.Ref) access.Decision {
	c.cap, c.ref, c.seen = capability, resource, true
	return c.ret
}

// reqWithDecider installs the decider into a request that already carries the
// role policy + roles (the shape grantReq produces), so CanResource can consult
// it. Mirrors how DeciderMiddleware layers on top of access.Middleware.
func reqWithDecider(r *http.Request, d access.Decider) *http.Request {
	return r.WithContext(access.WithDecider(r.Context(), d))
}

// TestDeciderConsult_GetSeesEntityAndID pins issue #80's CRUD consult: when a
// resource-aware check runs on Get, the decider is handed Ref{Type: entity
// name, ID: the path id}. Before the consult, requirePermission called access.Can
// with no resource at all.
func TestDeciderConsult_GetSeesEntityAndID(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	cd := &capturingDecider{ret: access.DecisionAbstain} // delegate to the role policy

	req := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs/42", nil), "docs:read")
	req.SetPathValue("id", "42")
	req = reqWithDecider(req, cd.fn)

	ch.Get()(httptest.NewRecorder(), req)

	if !cd.seen {
		t.Fatalf("decider was not consulted for Get (issue #80 consult missing)")
	}
	if cd.cap != "docs:read" {
		t.Fatalf("decider capability = %q, want docs:read", cd.cap)
	}
	if cd.ref.Type != "docs" || cd.ref.ID != "42" {
		t.Fatalf("decider ref = %+v, want {docs 42}", cd.ref)
	}
}

// TestDeciderConsult_ListSeesEmptyID confirms collection-level ops pass an empty
// record id — the decider sees Ref{Type: "docs", ID: ""}, not a fabricated id.
func TestDeciderConsult_ListSeesEmptyID(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	cd := &capturingDecider{ret: access.DecisionAbstain}

	req := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs", nil), "docs:read")
	req = reqWithDecider(req, cd.fn)
	ch.List()(httptest.NewRecorder(), req)

	if !cd.seen {
		t.Fatalf("decider was not consulted for List")
	}
	if cd.ref.Type != "docs" {
		t.Fatalf("decider ref.Type = %q, want docs", cd.ref.Type)
	}
	if cd.ref.ID != "" {
		t.Fatalf("decider ref.ID = %q, want empty for collection-level List", cd.ref.ID)
	}
}

// TestDeciderConsult_DenyBlocksGrantedUpdate confirms the decider can TIGHTEN
// below the role policy: the caller holds docs:write (role policy allows), but
// the decider returns Deny → 403. This is the whole point of the seam.
func TestDeciderConsult_DenyBlocksGrantedUpdate(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	cd := &capturingDecider{ret: access.DecisionDeny}

	req := grantReq(httptest.NewRequest(http.MethodPatch, "/api/docs/42",
		nil), "docs:write") // role policy would allow
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "42")
	req = reqWithDecider(req, cd.fn)

	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Update with Deny decider = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
}

// TestDeciderConsult_AllowPermitsUngatedRead confirms the decider can LOOSEN
// beyond the role policy: the caller has NO docs:read grant (role policy
// denies), but the decider returns Allow → the gate passes (not 403).
func TestDeciderConsult_AllowPermitsUngatedRead(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','ok')`); err != nil {
		t.Fatal(err)
	}
	cd := &capturingDecider{ret: access.DecisionAllow}

	// grantReq with NO docs:read — only a dummy permission so policy is present.
	req := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs/d1", nil), "docs:nopenope")
	req.SetPathValue("id", "d1")
	req = reqWithDecider(req, cd.fn)

	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("Get with Allow decider (no docs:read grant) = 403, want gate to pass. body=%s", rec.Body.String())
	}
}

// TestDeciderConsult_NoDeciderUnchanged confirms byte-identical behaviour with
// no decider installed: the existing crud permission-gate semantics (the suite
// in permission_gate_test.go) must hold — a granted role is allowed, an
// ungranted one is denied, with no resource-aware path involved.
func TestDeciderConsult_NoDeciderUnchanged(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)

	// Granted → allowed (200, not 403).
	reqOK := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs", nil), "docs:read")
	recOK := httptest.NewRecorder()
	ch.List()(recOK, reqOK)
	if recOK.Code == http.StatusForbidden {
		t.Fatalf("List with docs:read and no decider = 403, want gate to pass (unchanged)")
	}

	// Not granted → 403, exactly as before the seam.
	reqNo := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs", nil), "docs:other")
	recNo := httptest.NewRecorder()
	ch.List()(recNo, reqNo)
	if recNo.Code != http.StatusForbidden {
		t.Fatalf("List without docs:read and no decider = %d, want 403 (unchanged)", recNo.Code)
	}
}
