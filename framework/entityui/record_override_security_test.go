package entityui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// noCapCtx is u1 signed in with a policy that grants nothing relevant.
func noCapCtx() context.Context {
	policy := access.NewRolePolicy()
	policy.Register("invoices:read", "invoices:update")
	return bulkCtx("u1", policy, "clerk")
}

// A caller without the capability is refused before anything is read or
// written, and no audit row appears.
func TestOverrideNoCapabilityRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	rec := postOverride(t, w, noCapCtx(), "application/json", `{"state":"paid","reason":"nope"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// The Wildcard grant does not satisfy the exact check: a superuser role
// is not an override grant.
func TestOverrideWildcardRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	policy := access.NewRolePolicy()
	policy.Register("invoices:override_state")
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	rec := postOverride(t, w, bulkCtx("u1", policy, "root"), "application/json", `{"state":"paid","reason":"nope"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Wildcard role: status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// Elevation is not the capability: a context a back office lifted with
// crud.WithElevation still answers 403 without the exact grant.
func TestOverrideElevationIsNotCapability(t *testing.T) {
	w := newOverrideWorld(t, nil)
	rec := postOverride(t, w, crud.WithElevation(noCapCtx(), "invoices"), "application/json", `{"state":"paid","reason":"nope"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("elevated ctx: status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// Another owner's id answers 404, the same as a missing id: the read
// scope, not a 403, decides, and nothing changes.
func TestOverrideOtherOwnerNotFound(t *testing.T) {
	installOwnerExtractor(t)
	w := newOverrideWorld(t, func(ents map[string]entity.EntityConfig) {
		inv := ents["invoices"]
		inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
		inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
		ents["invoices"] = inv
	})
	// Move inv-1 to u2 after seeding.
	if _, err := w.x.db.Exec(`UPDATE invoices SET owner_id = 'u2' WHERE id = 'inv-1'`); err != nil {
		t.Fatal(err)
	}
	policy := overridePolicy(t, "invoices:override_state")
	rec := postOverride(t, w, overrideCtx(policy, "boss"), "application/json", `{"state":"paid","reason":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another owner's record: status %d, want 404: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// A blank reason (after trimming) changes nothing.
func TestOverrideBlankReasonRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, reason := range []string{"", "   ", "\t\n"} {
		raw, _ := json.Marshal(map[string]string{"state": "paid", "reason": reason})
		rec := postOverride(t, w, ctx, "application/json", string(raw))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("reason %q: status %d, want 400: %s", reason, rec.Code, rec.Body.String())
		}
	}
	assertInvoiceUntouched(t, w)
}

// A reason past the cap changes nothing.
func TestOverrideReasonTooLongRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	rec := postOverride(t, w, ctx, "application/json", `{"state":"paid","reason":"`+strings.Repeat("x", 501)+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// A value the state field does not declare changes nothing.
func TestOverrideUnknownStateRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, state := range []string{"archived", "", "DRAFT"} {
		rec := postOverride(t, w, ctx, "application/json", `{"state":"`+state+`","reason":"why"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("state %q: status %d, want 400: %s", state, rec.Code, rec.Body.String())
		}
	}
	assertInvoiceUntouched(t, w)
}

// An entity no audit log records is refused by crud itself (409): the
// override's only trail is the audit row, so the write never falls back
// to an unaudited one.
func TestOverrideNoAuditLogRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	// Swap in an entity WithAuditLog never marked: the flag is what
	// releases the override, and this app never set it.
	e := mustEntity(t, w.x, "invoices")
	fresh := entity.Define(e.GetName(), e.Config)
	fresh.SetDB(w.x.db)
	w.x.host.reg.ents["invoices"] = fresh
	w.x.host.cruds = map[string]*crud.CrudHandler{}
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	rec := postOverride(t, w, ctx, "application/json", `{"state":"paid","reason":"why"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// A cross-site post is refused before the body is read.
func TestOverrideCrossSiteRefused(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/inv-1/_override",
		strings.NewReader(`{"state":"paid","reason":"nope"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	w.x.ui.OverrideHandler("invoices").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site post: status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// A body past the cap is refused.
func TestOverrideBodyCapped(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	big := `{"state":"paid","reason":"` + strings.Repeat("y", 1<<20) + `"}`
	rec := postOverride(t, w, ctx, "application/json", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", rec.Code, rec.Body.String())
	}
	assertInvoiceUntouched(t, w)
}

// Only POST is served.
func TestOverridePOSTOnly(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/invoices/inv-1/_override", nil)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		w.x.ui.OverrideHandler("invoices").ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", method, rec.Code)
		}
	}
	assertInvoiceUntouched(t, w)
}

// The success path writes exactly one audit row: the operation
// state_override, the reason on the seam the audit writer reads, and the
// record's new value.
func TestOverrideSuccessOneAuditRow(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	rec := postOverride(t, w, ctx, "application/json", `{"state":"paid","reason":"caller paid by wire"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if s := invoiceStatus(t, w.x, "inv-1"); s != "paid" {
		t.Fatalf("status = %q, want paid", s)
	}
	if len(w.ops) != 1 || w.ops[0] != "state_override" {
		t.Fatalf("audit operations = %v, want exactly one state_override", w.ops)
	}
	if len(w.why) != 1 || w.why[0] != "caller paid by wire" {
		t.Fatalf("audit reasons = %v, want the submitted reason", w.why)
	}
	if len(w.from) != 1 || w.from[0]["status"] != "paid" {
		t.Fatalf("the audited row does not carry the new value: %v", w.from)
	}
}

// assertInvoiceUntouched pins the guard tests' other half: nothing
// changed, nothing was audited.
func assertInvoiceUntouched(t *testing.T, w *overrideWorld) {
	t.Helper()
	if s := invoiceStatus(t, w.x, "inv-1"); s != "draft" {
		t.Fatalf("the record changed: status = %q, want draft", s)
	}
	if len(w.ops) != 0 {
		t.Fatalf("a refused override wrote audit rows: %v (reasons %v)", w.ops, w.why)
	}
}
