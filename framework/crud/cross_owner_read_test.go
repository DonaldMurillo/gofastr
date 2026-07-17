package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// setupCrossOwnerReadHandler builds an OwnerField entity with
// CrossOwnerRead set, seeded with two owners' rows.
func setupCrossOwnerReadHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE ctickets (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		subject TEXT
	)`)
	ent := entity.Define("ctickets", entity.EntityConfig{
		Table:          "ctickets",
		Fields:         []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "subject", Type: schema.String}},
		OwnerField:     "user_id",
		CrossOwnerRead: "tickets:read:all",
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "ctickets", []map[string]any{
		{"id": "t-a", "user_id": "alice", "subject": "Alpha"},
		{"id": "t-b", "user_id": "bob", "subject": "Beta"},
	})
	return ch, db
}

// setupCrossOwnerReadTenantHandler seeds rows in two tenants so the
// tenant scope can be proven to hold even under a cross-owner-read grant.
func setupCrossOwnerReadTenantHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE cdocs (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		body TEXT,
		tenant_id TEXT
	)`)
	ent := entity.Define("cdocs", entity.EntityConfig{
		Table:          "cdocs",
		Fields:         []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}, {Name: "tenant_id", Type: schema.String}},
		OwnerField:     "user_id",
		CrossOwnerRead: "docs:read:all",
		MultiTenant:    true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "cdocs", []map[string]any{
		{"id": "d-a1", "user_id": "alice", "body": "A1", "tenant_id": "ta"},
		{"id": "d-b1", "user_id": "bob", "body": "B1", "tenant_id": "ta"},
		{"id": "d-a2", "user_id": "alice", "body": "A2", "tenant_id": "tb"},
	})
	return ch, db
}

// ctxWithGrant returns ctx carrying a RolePolicy that grants perm to
// "staff", plus the "staff" role — the shape access.Middleware installs.
func ctxWithGrant(ctx context.Context, perm string) context.Context {
	policy := access.NewRolePolicy()
	policy.Grant("staff", access.Permission(perm))
	ctx = access.WithPolicy(ctx, policy)
	return access.WithRoles(ctx, []string{"staff"})
}

// reqWithGrant sets a signed-in user + the cross-owner-read grant on r.
func reqWithGrant(r *http.Request, uid, perm string) *http.Request {
	r = withTestUser(r, uid)
	ctx := ctxWithGrant(r.Context(), perm)
	return r.WithContext(ctx)
}

// reqWithRolesOnly puts roles+policy in context but grants NO permission.
func reqWithRolesOnly(r *http.Request, uid string) *http.Request {
	r = withTestUser(r, uid)
	policy := access.NewRolePolicy()
	ctx := access.WithPolicy(r.Context(), policy)
	ctx = access.WithRoles(ctx, []string{"staff"})
	return r.WithContext(ctx)
}

// TestCrossOwnerReadHTTPCannotSpoof proves client-supplied headers/params
// naming the permission never widen the scope without a server-side grant.
func TestCrossOwnerReadHTTPCannotSpoof(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/ctickets?cross_owner_read=tickets:read:all&role=staff", nil)
	req.Header.Set("X-Cross-Owner-Read", "tickets:read:all")
	req.Header.Set("X-Role", "staff")
	req = withTestUser(req, "alice") // no policy/roles in context
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("List status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "t-b") || strings.Contains(body, "Beta") {
		t.Fatalf("spoofed grant leaked bob's row: %s", body)
	}
	if !strings.Contains(body, "t-a") {
		t.Fatalf("alice's own row missing: %s", body)
	}
}

// TestCrossOwnerReadNoPolicyFailsClosed: knob set, but ctx has roles
// without a matching grant, or a policy with no roles ⇒ stays scoped.
func TestCrossOwnerReadNoPolicyFailsClosed(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)

	// Case 1: policy + roles present, but the permission is NOT granted.
	req := reqWithRolesOnly(httptest.NewRequest(http.MethodGet, "/api/ctickets", nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Beta") {
		t.Fatalf("unguaranteed role leaked bob's row: %s", rec.Body.String())
	}

	// Case 2: policy present, no roles at all.
	req2 := withTestUser(httptest.NewRequest(http.MethodGet, "/api/ctickets", nil), "alice")
	policy := access.NewRolePolicy()
	ctx := access.WithPolicy(req2.Context(), policy)
	req2 = req2.WithContext(ctx)
	rec2 := httptest.NewRecorder()
	ch.List()(rec2, req2)
	if strings.Contains(rec2.Body.String(), "Beta") {
		t.Fatalf("policy-without-roles leaked bob's row: %s", rec2.Body.String())
	}
}

// TestCrossOwnerReadGrantSpansOwners: policy + granted role ⇒ List/Get/
// Count span owners; count envelope matches; cursor + stream paths too.
func TestCrossOwnerReadGrantSpansOwners(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)

	// Buffered List.
	req := reqWithGrant(httptest.NewRequest(http.MethodGet, "/api/ctickets", nil), "alice", "tickets:read:all")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List status=%d body=%s", rec.Code, rec.Body.String())
	}
	lr := decodeListResponse(t, rec.Body.String())
	if len(lr.Data) != 2 {
		t.Fatalf("granted List returned %d rows, want 2", len(lr.Data))
	}
	if lr.Total != 2 {
		t.Fatalf("granted List total=%d, want 2", lr.Total)
	}

	// Get another owner's row.
	reqGet := reqWithGrant(httptest.NewRequest(http.MethodGet, "/api/ctickets/t-b", nil), "alice", "tickets:read:all")
	reqGet.SetPathValue("id", "t-b")
	recGet := httptest.NewRecorder()
	ch.Get()(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("Get status=%d body=%s", recGet.Code, recGet.Body.String())
	}

	// Cursor path: ?cursor= switches to keyset mode; granted ctx spans owners.
	reqCur := reqWithGrant(httptest.NewRequest(http.MethodGet, "/api/ctickets?cursor=&limit=10", nil), "alice", "tickets:read:all")
	recCur := httptest.NewRecorder()
	ch.List()(recCur, reqCur)
	if recCur.Code != http.StatusOK {
		t.Fatalf("cursor List status=%d body=%s", recCur.Code, recCur.Body.String())
	}
	if !strings.Contains(recCur.Body.String(), "cursor") {
		t.Fatalf("cursor envelope missing: %s", recCur.Body.String())
	}
	if !(strings.Contains(recCur.Body.String(), "t-a") && strings.Contains(recCur.Body.String(), "t-b")) {
		t.Fatalf("granted cursor did not span owners: %s", recCur.Body.String())
	}

	// Stream path: ?stream=true.
	reqStream := reqWithGrant(httptest.NewRequest(http.MethodGet, "/api/ctickets?stream=true&limit=10", nil), "alice", "tickets:read:all")
	recStream := httptest.NewRecorder()
	ch.List()(recStream, reqStream)
	if recStream.Code != http.StatusOK {
		t.Fatalf("stream List status=%d body=%s", recStream.Code, recStream.Body.String())
	}
	body := recStream.Body.String()
	if !(strings.Contains(body, "t-a") && strings.Contains(body, "t-b")) {
		t.Fatalf("granted stream did not span owners: %s", body)
	}

	// In-process parity: ListAll / CountAll / GetOne.
	ctx := ctxWithGrant(signedIn("alice"), "tickets:read:all")
	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("granted ListAll = %d rows, want 2", len(rows))
	}
	n, err := ch.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if n != 2 {
		t.Fatalf("granted CountAll = %d, want 2", n)
	}
	row, err := ch.GetOne(ctx, "t-b", nil)
	if err != nil {
		t.Fatalf("granted GetOne: %v", err)
	}
	if row["subject"] != "Beta" {
		t.Fatalf("granted GetOne subject=%v, want Beta", row["subject"])
	}
}

// TestCrossOwnerReadWildcardOptInOnly: a wildcard/admin-shaped ctx spans
// owners on the opted-in entity AND stays scoped on a sibling entity
// without the knob.
func TestCrossOwnerReadWildcardOptInOnly(t *testing.T) {
	installOwnerExtractor(t)
	chOptedIn, _ := setupCrossOwnerReadHandler(t)

	// Sibling: same table shape, OwnerField, but NO CrossOwnerRead.
	db2 := setupDB(t, `CREATE TABLE snote (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		body TEXT
	)`)
	entSib := entity.Define("snote", entity.EntityConfig{
		Table:      "snote",
		Fields:     []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	entSib.SetDB(db2)
	chSib := NewCrudHandler(entSib, db2).WithJSONCase(CaseSnake)
	seedRows(t, db2, "snote", []map[string]any{
		{"id": "s-a", "user_id": "alice", "body": "SA"},
		{"id": "s-b", "user_id": "bob", "body": "SB"},
	})

	// Wildcard grant: passes any permission check.
	wildcardCtx := func(uid string) context.Context {
		policy := access.NewRolePolicy()
		policy.Grant("admin", access.Wildcard)
		ctx := access.WithPolicy(signedIn(uid), policy)
		return access.WithRoles(ctx, []string{"admin"})
	}

	// Opted-in entity: wildcard spans owners.
	rows, err := chOptedIn.ListAll(wildcardCtx("alice"), ListOptions{})
	if err != nil {
		t.Fatalf("opted-in ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("opted-in wildcard ListAll = %d rows, want 2", len(rows))
	}

	// Sibling without the knob: stays scoped to alice even under wildcard.
	rowsSib, err := chSib.ListAll(wildcardCtx("alice"), ListOptions{})
	if err != nil {
		t.Fatalf("sibling ListAll: %v", err)
	}
	if len(rowsSib) != 1 {
		t.Fatalf("sibling wildcard ListAll = %d rows, want 1 (scoped)", len(rowsSib))
	}
}

// TestCrossOwnerReadWritesStayScoped: granted ctx can read across owners
// but PUT/PATCH/DELETE on another owner's row ⇒ 404; Create still stamps caller.
func TestCrossOwnerReadWritesStayScoped(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)
	ctx := ctxWithGrant(signedIn("alice"), "tickets:read:all")

	// PUT bob's row: should 404 (owner scope stays on for writes).
	body := strings.NewReader(`{"subject":"pwned"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/ctickets/t-b", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "t-b")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("granted PUT of other owner's row = %d, want 404. body=%s", rec.Code, rec.Body.String())
	}

	// DELETE bob's row: should 404.
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/ctickets/t-b", nil)
	reqDel.SetPathValue("id", "t-b")
	reqDel = reqDel.WithContext(ctx)
	recDel := httptest.NewRecorder()
	ch.Delete()(recDel, reqDel)
	if recDel.Code != http.StatusNotFound {
		t.Fatalf("granted DELETE of other owner's row = %d, want 404. body=%s", recDel.Code, recDel.Body.String())
	}

	// Create: owner is still stamped from the caller's context (alice).
	createBody := strings.NewReader(`{"subject":"new"}`)
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/ctickets", createBody)
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate = reqCreate.WithContext(ctx)
	recCreate := httptest.NewRecorder()
	ch.Create()(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated && recCreate.Code != http.StatusOK {
		t.Fatalf("Create status=%d body=%s", recCreate.Code, recCreate.Body.String())
	}
	created := decodeSingleResponse(t, recCreate.Body.Bytes())
	if created["user_id"] != "alice" {
		t.Fatalf("Create stamped user_id=%v, want alice", created["user_id"])
	}
}

// TestCrossOwnerReadTenantScopeHolds: granted ctx in tenant A never sees
// tenant B rows, even though owner scoping is lifted.
func TestCrossOwnerReadTenantScopeHolds(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadTenantHandler(t)

	ctx := ctxWithGrant(signedIn("alice"), "docs:read:all")
	ctx = tenant.SetTenantID(ctx, "ta")

	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	// Tenant ta has d-a1 (alice) + d-b1 (bob). Cross-owner-read lifts owner
	// scope so both appear, but d-a2 (tenant tb) must NOT.
	if len(rows) != 2 {
		t.Fatalf("granted tenant-A ListAll = %d rows, want 2 (owner lifted, tenant holds)", len(rows))
	}
	for _, r := range rows {
		if r["tenant_id"] == "tb" {
			t.Fatalf("tenant-B row leaked into tenant-A scope: %+v", r)
		}
	}
}

// TestCrossOwnerReadAnonymousStill401: an anonymous request (no owner in
// context) on a CrossOwnerRead entity is still refused with 401 — the
// RequireOwner gate fires before the scope helper. A bare permission grant
// in context does NOT replace authentication.
func TestCrossOwnerReadAnonymousStill401(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)

	// No user in context, but a policy+role grant is present.
	policy := access.NewRolePolicy()
	policy.Grant("staff", access.Permission("tickets:read:all"))
	ctx := access.WithPolicy(context.Background(), policy)
	ctx = access.WithRoles(ctx, []string{"staff"})

	req := httptest.NewRequest(http.MethodGet, "/api/ctickets", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous List = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}

// TestDefineCrossOwnerReadRequiresOwnerField pins the Define panic when
// CrossOwnerRead is set without OwnerField.
func TestDefineCrossOwnerReadRequiresOwnerField(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define did not panic when CrossOwnerRead set without OwnerField")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "CrossOwnerRead") || !strings.Contains(msg, "OwnerField") {
			t.Fatalf("panic message wrong: %v", r)
		}
	}()
	entity.Define("bad", entity.EntityConfig{
		Fields:         []schema.Field{{Name: "n", Type: schema.String}},
		CrossOwnerRead: "x:read:all",
	}.WithTimestamps(false))
}
