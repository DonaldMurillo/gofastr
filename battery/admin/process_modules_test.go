package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

var errBoom = errors.New("boom")

// fakeModuleController is a canned processModuleController for tests. It
// never spawns a child process — it records the calls and returns whatever
// the test scripted. The real *framework.ProcessModuleSupervisor satisfies
// the same interface.
type fakeModuleController struct {
	list []framework.ProcessModuleInfo

	enableErr  error
	disableErr error
	bumpGen    uint64
	bumpErr    error
	revokeGen  uint64
	revokeErr  error

	enabled  []string
	disabled []string
	bumped   []string
	revoked  []revokedGrant
}

type revokedGrant struct {
	name   string
	grants []access.Permission
}

func (f *fakeModuleController) List() []framework.ProcessModuleInfo { return f.list }

func (f *fakeModuleController) Enable(_ context.Context, name string) error {
	f.enabled = append(f.enabled, name)
	return f.enableErr
}

func (f *fakeModuleController) Disable(_ context.Context, name string) error {
	f.disabled = append(f.disabled, name)
	return f.disableErr
}

func (f *fakeModuleController) BumpGeneration(_ context.Context, name string) (uint64, error) {
	f.bumped = append(f.bumped, name)
	return f.bumpGen, f.bumpErr
}

func (f *fakeModuleController) RevokeGrants(_ context.Context, name string, grants []access.Permission) (uint64, error) {
	f.revoked = append(f.revoked, revokedGrant{name: name, grants: append([]access.Permission(nil), grants...)})
	return f.revokeGen, f.revokeErr
}

// moduleTestEnv wires a SQLite DB (with audit table) + a fake controller.
// Mirrors rbacTestEnv's shape: bare-mounted admin battery, direct routes.
func moduleTestEnv(t *testing.T, fake *fakeModuleController) (*Battery, *router.Router, *sql.DB) {
	t.Helper()
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
	b := New(Config{DB: db, ProcessModules: fake})
	r := router.New()
	b.RegisterRoutes(r)
	return b, r, db
}

// moduleInfo builds a ProcessModuleInfo with sane defaults for tests.
func moduleInfo(name string, state framework.ProcessState, opts ...func(*framework.ProcessModuleInfo)) framework.ProcessModuleInfo {
	info := framework.ProcessModuleInfo{
		Name:               name,
		State:              state,
		TrustTier:          framework.TrustTrusted,
		DesiredGeneration:  1,
		ObservedGeneration: 1,
		RouteCount:         2,
		ToolCount:          1,
	}
	for _, o := range opts {
		o(&info)
	}
	return info
}

// ----- list rendering ------------------------------------------------------

// TestModules_ListRendersEveryState pins that the list screen renders every
// introspection field and — critically — that the 404-vs-503 distinction is
// visible in copy (a disabled module reads differently from a crashed one),
// with circuit-open and lease-failing shown prominently.
func TestModules_ListRendersEveryState(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{
		moduleInfo("billing", framework.StateInstalledDisabled),
		moduleInfo("search", framework.StateReady),
		moduleInfo("ingest", framework.StateBackoff,
			func(i *framework.ProcessModuleInfo) { i.CircuitOpen = true }),
		moduleInfo("reports", framework.StateStarting,
			func(i *framework.ProcessModuleInfo) { i.LeaseFailing = true }),
	}}
	_, h, _ := moduleTestEnv(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"billing", "search", "ingest", "reports",
		"trusted", // trust tier (TrustTier.String() is lowercase)
		"404",     // disabled → 404 surfaced in copy
		"503",     // enabled-but-down → 503 surfaced in copy
		"Circuit open",
		"Lease failing",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

// TestModules_EmptyStateNoPanic pins the generated-app rule: an empty list
// renders a friendly empty state and never panics.
func TestModules_EmptyStateNoPanic(t *testing.T) {
	fake := &fakeModuleController{list: nil}
	_, h, _ := moduleTestEnv(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty list got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No process modules") {
		t.Errorf("empty state missing friendly copy; got %q", rr.Body.String())
	}
}

// ----- access gating -------------------------------------------------------

// TestModules_NonAdminDenied pins the default-deny gate: an authenticated
// non-admin gets 403 on the modules screen, anonymous gets 401.
func TestModules_NonAdminDenied(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{moduleInfo("a", framework.StateReady)}}
	_, h, _ := moduleTestEnv(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"reader"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("non-admin got %d, want 403", rr.Code)
	}
}

func TestModules_AnonymousDenied(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{moduleInfo("a", framework.StateReady)}}
	_, h, _ := moduleTestEnv(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", rr.Code)
	}
}

// TestModules_NilControllerNotMounted pins that a nil ProcessModules config
// leaves the screen unmounted — the route 404s rather than panicking.
func TestModules_NilControllerNotMounted(t *testing.T) {
	db := newDB(t)
	b := New(Config{DB: db})
	r := router.New()
	b.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/admin/modules", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("nil controller got %d, want 404", rr.Code)
	}
}

// ----- lifecycle actions ---------------------------------------------------

func TestModules_EnableCallsControllerAndAudit(t *testing.T) {
	fake := &fakeModuleController{
		list:      []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateInstalledDisabled)},
		bumpGen:   7,
		revokeGen: 9,
	}
	_, h, db := moduleTestEnv(t, fake)

	form := url.Values{"module": {"billing"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("enable got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.enabled) != 1 || fake.enabled[0] != "billing" {
		t.Errorf("controller Enable not called correctly: %v", fake.enabled)
	}
	assertModuleAuditRow(t, db, "module_enable", "billing")
}

func TestModules_DisableCallsControllerAndAudit(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{moduleInfo("search", framework.StateReady)}}
	_, h, db := moduleTestEnv(t, fake)

	form := url.Values{"module": {"search"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_disable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("disable got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.disabled) != 1 || fake.disabled[0] != "search" {
		t.Errorf("controller Disable not called: %v", fake.disabled)
	}
	assertModuleAuditRow(t, db, "module_disable", "search")
}

func TestModules_BumpCallsControllerAndAudit(t *testing.T) {
	fake := &fakeModuleController{
		list:    []framework.ProcessModuleInfo{moduleInfo("ingest", framework.StateBackoff)},
		bumpGen: 5,
	}
	_, h, db := moduleTestEnv(t, fake)

	form := url.Values{"module": {"ingest"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_bump", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("bump got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.bumped) != 1 || fake.bumped[0] != "ingest" {
		t.Errorf("controller Bump not called: %v", fake.bumped)
	}
	assertModuleAuditRow(t, db, "module_bump", "ingest")
}

func TestModules_RevokeCallsControllerAndAudit(t *testing.T) {
	fake := &fakeModuleController{
		list:      []framework.ProcessModuleInfo{moduleInfo("reports", framework.StateReady)},
		revokeGen: 11,
	}
	_, h, db := moduleTestEnv(t, fake)

	form := url.Values{"module": {"reports"}, "grant": {"posts:read"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("revoke got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	if len(fake.revoked) != 1 || fake.revoked[0].name != "reports" {
		t.Errorf("controller Revoke not called: %+v", fake.revoked)
	}
	if len(fake.revoked[0].grants) != 1 || string(fake.revoked[0].grants[0]) != "posts:read" {
		t.Errorf("revoke grant mismatch: %+v", fake.revoked[0].grants)
	}
	assertModuleAuditRow(t, db, "module_revoke", "reports")
}

// TestModules_ControllerErrorShowsMessage pins the generated-app rule: a
// controller error surfaces as a flash message on the list page — never a
// raw 500 or JSON leak. The POST redirects (303) to the list with ?err=.
func TestModules_ControllerErrorShowsMessage(t *testing.T) {
	fake := &fakeModuleController{
		list:      []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)},
		enableErr: errBoom,
	}
	_, h, _ := moduleTestEnv(t, fake)

	form := url.Values{"module": {"billing"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("error enable got %d, want 303; body=%s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "/admin/modules") || !strings.Contains(loc, "err=") {
		t.Errorf("error redirect missing err= in %q", loc)
	}
	// Follow the redirect and confirm the message renders in the list.
	getReq := httptest.NewRequest(http.MethodGet, loc, nil)
	getReq = getReq.WithContext(handler.SetUser(getReq.Context(), roleUser{roles: []string{"admin"}}))
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("error list got %d, want 200", getRR.Code)
	}
	body := getRR.Body.String()
	if !strings.Contains(body, "class=\"err\"") || !strings.Contains(body, "boom") {
		t.Errorf("error flash not rendered in list: %q", body)
	}
	if strings.Contains(body, "application/json") {
		t.Errorf("JSON leaked into error response")
	}
}

// TestModules_UnknownModuleShowsError pins that an action against a name the
// controller does not supervise surfaces a message, not a 500.
func TestModules_UnknownModuleShowsError(t *testing.T) {
	fake := &fakeModuleController{
		list:      []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)},
		enableErr: framework.ErrNoDesiredRow,
	}
	_, h, _ := moduleTestEnv(t, fake)

	form := url.Values{"module": {"ghost"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", rr.Code)
	}
}

// TestModules_MissingNameIsBounced pins that a POST with no module name does
// not reach the controller — it bounces back with a validation flash.
func TestModules_MissingNameIsBounced(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)}}
	_, h, _ := moduleTestEnv(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", rr.Code)
	}
	if len(fake.enabled) != 0 {
		t.Errorf("controller should not have been called: %v", fake.enabled)
	}
}

// ----- helpers -------------------------------------------------------------

func assertModuleAuditRow(t *testing.T, db *sql.DB, wantOp, wantRecordID string) {
	t.Helper()
	var entity, op, recordID string
	err := db.QueryRowContext(context.Background(),
		"SELECT entity, op, record_id FROM audit_log WHERE op = ? ORDER BY created_at DESC LIMIT 1",
		wantOp,
	).Scan(&entity, &op, &recordID)
	if err != nil {
		t.Fatalf("audit row for op=%q missing: %v", wantOp, err)
	}
	if entity != "module" || op != wantOp || recordID != wantRecordID {
		t.Errorf("audit = %q/%q/%q, want module/%s/%s", entity, op, recordID, wantOp, wantRecordID)
	}
}
