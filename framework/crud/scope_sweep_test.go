package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A process-module broker re-dispatch never exercises CrossOwnerRead, even
// for a caller whose roles grant it: the list stays on the caller's rows.
func TestBrokeredCallKeepsOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t)

	req := reqWithGrant(httptest.NewRequest(http.MethodGet, "/api/ctickets", nil), "alice", "tickets:read:all")
	req = req.WithContext(WithBrokeredCall(req.Context()))
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List status=%d body=%s", rec.Code, rec.Body.String())
	}
	if lr := decodeListResponse(t, rec.Body.String()); len(lr.Data) != 1 || lr.Total != 1 {
		t.Fatalf("brokered List returned %d rows (total %d), want alice's 1: %s", len(lr.Data), lr.Total, rec.Body.String())
	}
}

// The gates answer in a fixed order: owner, tenant, the baseline session,
// then the permission. A caller with no owner on an owner-scoped,
// Access-gated entity is told to sign in (401), not that a permission is
// missing (403).
func TestScopeGateOrderOwnerBeforePermission(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE gnotes (id TEXT PRIMARY KEY, user_id TEXT, body TEXT)`)
	ent := entity.Define("gnotes", entity.EntityConfig{Table: "gnotes",
		Fields:   []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "body", Type: schema.String}},
		Scope:    &entity.ScopeConfig{OwnerField: "user_id"},
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "gnotes:read"}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	rec := httptest.NewRecorder()
	ch.List()(rec, httptest.NewRequest(http.MethodGet, "/api/gnotes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous List = %d (%s), want 401 from the owner gate", rec.Code, rec.Body.String())
	}
	if ch.CanReadScoped(httptest.NewRequest(http.MethodGet, "/", nil).Context()) {
		t.Fatal("CanReadScoped admitted an anonymous caller")
	}
}

// The predicates refuse a MultiTenant entity to a signed-in caller with no
// tenant, as the routes do.
func TestCanReadScopedRefusesMissingTenant(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	ctx := withTestUser(httptest.NewRequest(http.MethodGet, "/items", nil), "alice").Context()
	if ch.CanReadScoped(ctx) || ch.CanCreateScoped(ctx) {
		t.Fatal("a caller with no tenant passed the scoped predicates of a MultiTenant entity")
	}
}

// A ReadScope value binds as the column's type: "true" on a Bool column
// matches the rows SQLite stores as 1.
func TestReadScopeEqBindsBoolTyped(t *testing.T) {
	cfg := makeEntityConfig("flags", "flags", "", []schema.Field{{Name: "live", Type: schema.Bool}})
	cfg.Exposure = &entity.ExposureConfig{
		Public:    true,
		ReadScope: &entity.ReadScopeConfig{Filter: []entity.RowPredicate{{Field: "live", Op: "eq", Value: "true"}}},
	}
	ch, db := setupSecurityTestHandler(t, cfg, `CREATE TABLE flags (id TEXT PRIMARY KEY, live BOOLEAN);`)
	seedRows(t, db, "flags", []map[string]any{{"id": "on", "live": true}, {"id": "off", "live": false}})

	rr := httptest.NewRecorder()
	ch.List()(rr, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/flags"}))
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
	if len(env.Data) != 1 || env.Data[0]["id"] != "on" {
		t.Fatalf("anonymous list = %s, want only the live row", rr.Body.String())
	}
}

// An entity with no Exposure block has no read scope, and asking for one
// neither panics nor filters.
func TestReadScopeNilExposure(t *testing.T) {
	ent := entity.Define("bare", entity.EntityConfig{Table: "bare", Fields: []schema.Field{{Name: "x", Type: schema.String}}})
	ent.Config.Exposure = nil
	ch := &CrudHandler{Entity: ent}
	if preds := readScopeFilters(context.Background(), ent); preds != nil {
		t.Fatalf("readScopeFilters = %v, want nil", preds)
	}
	if !ch.readScopeAllowsRecord(map[string]any{"x": "a"}) {
		t.Fatal("readScopeAllowsRecord refused a record of an entity with no scope")
	}
}

// A read-scope column or qualifier that fails identifier validation renders
// a fragment that matches no row.
func TestRenderReadScopeUnsafeIdentifierMatchesNothing(t *testing.T) {
	pred := []filter.ParsedFilter{{Field: "status", Op: filter.OpEq, Value: "x"}}
	if got, _ := renderReadScope([]filter.ParsedFilter{{Field: "a;drop", Op: filter.OpEq, Value: "x"}}, "", 1); got != "(1 = 0)" {
		t.Fatalf("unsafe column rendered %q", got)
	}
	if got, _ := renderReadScope(pred, "t; drop", 1); got != "(1 = 0)" {
		t.Fatalf("unsafe qualifier rendered %q", got)
	}
}

// Two declared in predicates on one column stay two ANDed lists, never one
// union, even when entity.Define's refusal of that declaration is bypassed.
func TestReadScopeTwoInPredicatesStayApart(t *testing.T) {
	ent := entity.Define("twoin", entity.EntityConfig{Table: "twoin", Fields: []schema.Field{{Name: "status", Type: schema.String}}})
	ent.Config.Exposure = &entity.ExposureConfig{ReadScope: &entity.ReadScopeConfig{Filter: []entity.RowPredicate{
		{Field: "status", Op: "in", Values: []string{"a", "b"}},
		{Field: "status", Op: "in", Values: []string{"b", "c"}},
	}}}
	sql, args := renderReadScope(readScopeFilters(context.Background(), ent), "", 1)
	if strings.Count(sql, " IN (") != 2 || len(args) != 4 {
		t.Fatalf("rendered %q %v, want two IN lists of two", sql, args)
	}
}
