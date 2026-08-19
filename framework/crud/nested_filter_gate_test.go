package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// gateEntity builds a target entity with the posture under test. The nested
// filter gate reads only Entity.Config and the context, so no DB is needed.
func gateEntity(t *testing.T, name string, cfg entity.EntityConfig) *entity.Entity {
	t.Helper()
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "body", Type: schema.String})
	return entity.Define(name, cfg.WithTimestamps(false))
}

// The subquery counts rows without selecting them, so it cannot scope them to
// the caller. An owner-scoped or multi-tenant target is therefore refused —
// except for a caller who may already read every row, for whom a hit/miss
// count reveals nothing the target's own list route would not.
func TestCheckNestedFiltersReadable_ScopedTargets(t *testing.T) {
	ownerScoped := gateEntity(t, "notes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true)},
	})
	// Public so the only thing that can refuse is the scoped-target rule under
	// test. Left default-posture, the BASELINE SESSION gate refuses first and
	// the test would pass for the wrong reason.
	multiTenant := gateEntity(t, "tnotes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{MultiTenant: true},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	plain := gateEntity(t, "pnotes", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"notes": ownerScoped, "tnotes": multiTenant, "pnotes": plain,
	}}
	parent := gateEntity(t, "boards", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	ch := &CrudHandler{Entity: parent, Registry: &reg}

	filterOn := func(target string) []nestedFilter {
		return []nestedFilter{{
			Relation: entity.Relation{Type: entity.RelHasMany, Name: target, Entity: target, ForeignKey: "board_id"},
			Field:    "body",
		}}
	}

	// Owner id present, but the rows may not be this owner's — refuse.
	prev := owner.GetExtractor()
	owner.SetExtractor(func(context.Context) (any, bool) { return "u1", true })
	t.Cleanup(func() { owner.SetExtractor(prev) })

	if err := ch.checkNestedFiltersReadable(context.Background(), filterOn("notes")); err == nil {
		t.Error("an owner-scoped target was permitted — the EXISTS clause cannot narrow rows to the caller, so the count is an oracle")
	} else if !strings.Contains(err.Error(), "notes") {
		t.Errorf("the refusal should name the target entity, got %v", err)
	}

	// A cross-owner caller may already list the target wholesale.
	if err := ch.checkNestedFiltersReadable(owner.AllowCrossOwner(context.Background()), filterOn("notes")); err != nil {
		t.Errorf("a cross-owner caller was refused; the count reveals nothing they cannot already list: %v", err)
	}

	// Tenant: present-but-possibly-foreign refuses; cross-tenant is exempt.
	tctx := tenant.SetTenantID(context.Background(), "tenant-a")
	if err := ch.checkNestedFiltersReadable(tctx, filterOn("tnotes")); err == nil {
		t.Error("a multi-tenant target was permitted for an ordinary tenant caller")
	}
	if err := ch.checkNestedFiltersReadable(tenant.AllowCrossTenant(context.Background()), filterOn("tnotes")); err != nil {
		t.Errorf("a cross-tenant caller was refused: %v", err)
	}

	// An unscoped, readable target is unaffected.
	if err := ch.checkNestedFiltersReadable(context.Background(), filterOn("pnotes")); err != nil {
		t.Errorf("an unscoped public target was refused: %v", err)
	}

	// A relation naming an entity the registry does not hold. Refusing is
	// load-bearing twice over: filtering against a table nobody vouched for
	// would be wrong, and the probe built below the branch dereferences the
	// resolved target — so falling through would panic rather than merely
	// permit. Nothing else in this file constructs an unresolvable relation.
	if err := ch.checkNestedFiltersReadable(context.Background(), filterOn("ghosts")); err == nil {
		t.Error("a relation naming an unregistered entity was permitted — the subquery would filter against an unvouched table")
	} else if !strings.Contains(err.Error(), "ghosts") {
		t.Errorf("the refusal should name the unresolvable target, got %v", err)
	}
}

func boolPtrGate(b bool) *bool { return &b }

// Relation.Entity is the registry key; the SQL must name the resolved target's
// table. resolvedTable falls back to the key only when no target resolved,
// matching the eager path's historical no-registry contract.
func TestResolvedTableUsesTargetTable(t *testing.T) {
	rel := entity.Relation{Entity: "authors"}
	renamed := entity.Define("authors", entity.EntityConfig{
		Table:  "acct_authors",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))

	if got := resolvedTable(renamed, rel); got != "acct_authors" {
		t.Errorf("resolvedTable = %q, want the target's TABLE — validation and SQL must agree on one table", got)
	}
	if got := resolvedTable(nil, rel); got != "authors" {
		t.Errorf("resolvedTable with no target = %q, want the relation key as the documented fallback", got)
	}
}

// The soft-delete predicate must reach every relation shape, qualified against
// the target's table.
func TestBuildExistsSubqueryHidesSoftDeletedInEveryShape(t *testing.T) {
	shapes := []struct {
		name string
		rel  entity.Relation
	}{
		{"many-to-one", entity.Relation{Type: entity.RelManyToOne, Entity: "notes", ForeignKey: "note_id"}},
		{"has-many", entity.Relation{Type: entity.RelHasMany, Entity: "notes", ForeignKey: "board_id"}},
		{"has-one", entity.Relation{Type: entity.RelHasOne, Entity: "notes", ForeignKey: "board_id"}},
		{"many-to-many", entity.Relation{Type: entity.RelManyToMany, Entity: "notes", Through: "board_notes", LocalKey: "board_id", ForeignKeyTarget: "note_id"}},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			nf := nestedFilter{Relation: sh.rel, Field: "body", Op: "eq", Value: "x", softDelete: true, table: "note_rows"}
			sql, _ := buildExistsSubquery("boards", "id", nf)
			if !strings.Contains(sql, "note_rows.deleted_at IS NULL") {
				t.Errorf("no soft-delete predicate on the target table:\n%s", sql)
			}
			if strings.Contains(sql, "FROM notes ") || strings.Contains(sql, "JOIN notes ") {
				t.Errorf("the subquery named the entity KEY as its table instead of the resolved table:\n%s", sql)
			}
			// And without the flag it must not appear.
			nf.softDelete = false
			plain, _ := buildExistsSubquery("boards", "id", nf)
			if strings.Contains(plain, "deleted_at") {
				t.Errorf("soft-delete predicate emitted for an entity that does not soft delete:\n%s", plain)
			}
		})
	}
}

// The cross-scope branches in the include scope helpers: a caller entitled to
// read across owners or tenants must not have the relation narrowed to their
// own scope, matching what the routes do. Fixing the tenant branch without the
// owner one is exactly how these two drifted apart before.
func TestRelatedScopeHelpersHonourCrossScopeMarkers(t *testing.T) {
	ownerTarget := gateEntity(t, "onotes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true)},
	})
	tenantTarget := gateEntity(t, "tnotes2", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{MultiTenant: true},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true)},
	})

	prev := owner.GetExtractor()
	owner.SetExtractor(func(context.Context) (any, bool) { return "u1", true })
	t.Cleanup(func() { owner.SetExtractor(prev) })

	t.Run("owner scope is applied for an ordinary caller", func(t *testing.T) {
		node := &IncludeNode{Target: ownerTarget}
		applyRelatedOwnerScope(context.Background(), node)
		if len(node.Filters) != 1 {
			t.Fatalf("want one owner predicate, got %d", len(node.Filters))
		}
	})
	t.Run("owner scope is skipped for a cross-owner caller", func(t *testing.T) {
		node := &IncludeNode{Target: ownerTarget}
		applyRelatedOwnerScope(owner.AllowCrossOwner(context.Background()), node)
		if len(node.Filters) != 0 {
			t.Errorf("a cross-owner caller had the relation narrowed to their own rows: %+v", node.Filters)
		}
	})
	t.Run("tenant scope is applied for an ordinary caller", func(t *testing.T) {
		node := &IncludeNode{Target: tenantTarget}
		applyRelatedTenantScope(tenant.SetTenantID(context.Background(), "a"), node)
		if len(node.Filters) != 1 {
			t.Fatalf("want one tenant predicate, got %d", len(node.Filters))
		}
	})
	t.Run("tenant scope is skipped for a cross-tenant caller", func(t *testing.T) {
		node := &IncludeNode{Target: tenantTarget}
		applyRelatedTenantScope(tenant.AllowCrossTenant(context.Background()), node)
		if len(node.Filters) != 0 {
			t.Errorf("a cross-tenant caller had the relation narrowed: %+v", node.Filters)
		}
	})
}

// checkIncludeReadable walks children, and refuses on the target's gate.
func TestCheckIncludeReadableRecursesAndRefuses(t *testing.T) {
	gated := gateEntity(t, "secrets", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{
			CRUD:   boolPtrGate(true),
			Access: entity.AccessControl{Read: "secrets:read"},
		},
	})
	open := gateEntity(t, "opens", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	ch := &CrudHandler{Entity: open}

	// A gated child under a readable parent must still refuse.
	nodes := []*IncludeNode{{
		Name: "parent", Target: open,
		Children: []*IncludeNode{{Name: "child", Target: gated}},
	}}
	err := ch.checkIncludeReadable(context.Background(), nodes)
	if err == nil {
		t.Fatal("a gated entity one level down was permitted — the walk must recurse into Children")
	}
	if !strings.Contains(err.Error(), "secrets") {
		t.Errorf("the refusal should name the entity that failed, got %v", err)
	}
	// All-readable trees pass.
	if err := ch.checkIncludeReadable(context.Background(), []*IncludeNode{{Name: "p", Target: open}}); err != nil {
		t.Errorf("a readable include was refused: %v", err)
	}
	// A nil target is skipped rather than panicking.
	if err := ch.checkIncludeReadable(context.Background(), []*IncludeNode{{Name: "p"}}); err != nil {
		t.Errorf("a node with no resolved target should be skipped, got %v", err)
	}
}

// The include error writer maps each failure class to its status: a budget
// overrun is the caller's fault (400), a posture refusal is 403 naming the
// entity, and anything else is an internal error that must not echo details.
func TestWriteIncludeErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"posture refusal", &includeForbiddenError{Entity: "users"}, http.StatusForbidden, "users"},
		{"budget overrun", errIncludeBudget, http.StatusBadRequest, ""},
		{"anything else", errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeIncludeError(rec, "list", tc.err)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body should name %q, got %s", tc.wantBody, rec.Body.String())
			}
			if tc.wantStatus == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("an internal error echoed its detail to the caller: %s", rec.Body.String())
			}
		})
	}
}

// The scope helpers no-op for entities that declare no scope, and for a nil
// node — the defensive paths every caller relies on.
func TestRelatedScopeHelpersNoOpWhenUnscoped(t *testing.T) {
	plain := gateEntity(t, "plain", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	node := &IncludeNode{Target: plain}
	applyRelatedOwnerScope(context.Background(), node)
	applyRelatedTenantScope(context.Background(), node)
	if len(node.Filters) != 0 {
		t.Errorf("an unscoped entity gained %d predicate(s): %+v", len(node.Filters), node.Filters)
	}
	// Nil node and nil target must not panic.
	applyRelatedOwnerScope(context.Background(), nil)
	applyRelatedTenantScope(context.Background(), nil)
	applyRelatedOwnerScope(context.Background(), &IncludeNode{})
	applyRelatedTenantScope(context.Background(), &IncludeNode{})
}

// resolveNestedFilters is the in-process twin of the HTTP parser: typed repos
// reach it through ListAll/CountAll with a NestedFilters spec. It sets the same
// softDelete and table flags, and neither was exercised — so a soft-deleted
// target could be enumerated through a typed repo, and a target whose Name
// differs from its Table would query the wrong table, both silently.
func TestResolveNestedFiltersCarriesSoftDeleteAndResolvedTable(t *testing.T) {
	// Name != Table on purpose: resolvedTable must win over the registry key.
	target := gateEntity(t, "notes", entity.EntityConfig{
		Table:    "note_rows",
		Scope:    &entity.ScopeConfig{SoftDelete: true},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
		Fields:   []schema.Field{{Name: "body", Type: schema.String}},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"notes": target}}
	parent := gateEntity(t, "boards", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
		Relations: []entity.Relation{
			{Type: entity.RelHasMany, Name: "notes", Entity: "notes", ForeignKey: "board_id"},
		},
	})

	for _, spec := range []NestedFilter{
		{Relation: "notes", Field: "body", Op: filter.OpEq, Value: "x"},
		{Relation: "notes", Field: "body", Op: filter.OpIn, Values: []string{"x", "y"}},
	} {
		got, err := resolveNestedFilters(parent, &reg, []NestedFilter{spec})
		if err != nil {
			t.Fatalf("resolveNestedFilters(%v): %v", spec.Op, err)
		}
		if len(got) != 1 {
			t.Fatalf("resolveNestedFilters returned %d filters, want 1", len(got))
		}
		if !got[0].softDelete {
			t.Errorf("op %v: softDelete not carried — a typed repo would enumerate trashed rows", spec.Op)
		}
		if got[0].table != "note_rows" {
			t.Errorf("op %v: table = %q, want note_rows — the subquery must name the resolved table, not the registry key", spec.Op, got[0].table)
		}
	}
}

// eagerScopeFilters is the EagerLoad twin of the include path's
// applyRelatedOwnerScope / applyRelatedTenantScope. When those learned to
// exempt cross-scope callers, this one did not — while its own comment still
// claimed it mirrored them "exactly". The result: a caller holding
// AllowCrossOwner got every row through ?include= and an empty relation
// through EagerLoad.
func TestEagerScopeFiltersExemptCrossScopeCallers(t *testing.T) {
	ownerScoped := gateEntity(t, "notes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	tenantScoped := gateEntity(t, "tnotes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{MultiTenant: true},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	granted := gateEntity(t, "gnotes", entity.EntityConfig{
		Scope: &entity.ScopeConfig{
			OwnerField:     "owner_id",
			CrossOwnerRead: "notes:read:all",
		},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})

	prev := owner.GetExtractor()
	owner.SetExtractor(func(context.Context) (any, bool) { return "u1", true })
	t.Cleanup(func() { owner.SetExtractor(prev) })

	// The ordinary caller is scoped — that is the IDOR control and it must stay.
	if got := eagerScopeFilters(context.Background(), ownerScoped); len(got) != 1 {
		t.Errorf("an ordinary caller got %d owner filters, want 1 — the cross-table control is gone", len(got))
	}
	if got := eagerScopeFilters(context.Background(), tenantScoped); len(got) != 1 {
		t.Errorf("an ordinary caller got %d tenant filters, want 1", len(got))
	}

	// Each cross-scope marker lifts its own scope and nothing else.
	if got := eagerScopeFilters(owner.AllowCrossOwner(context.Background()), ownerScoped); len(got) != 0 {
		t.Errorf("a cross-owner caller still got %v — EagerLoad narrows a relation the routes serve in full", got)
	}
	if got := eagerScopeFilters(tenant.AllowCrossTenant(context.Background()), tenantScoped); len(got) != 0 {
		t.Errorf("a cross-tenant caller still got %v", got)
	}
	// A cross-owner marker must NOT lift the tenant scope, or the exemption is
	// a blanket bypass rather than a per-scope one.
	if got := eagerScopeFilters(owner.AllowCrossOwner(context.Background()), tenantScoped); len(got) != 1 {
		t.Errorf("a cross-OWNER marker lifted the TENANT scope: %v", got)
	}
	if got := eagerScopeFilters(tenant.AllowCrossTenant(context.Background()), ownerScoped); len(got) != 1 {
		t.Errorf("a cross-TENANT marker lifted the OWNER scope: %v", got)
	}

	// The declared CrossOwnerRead permission is the third exemption, and it is
	// read from the TARGET entity — the same shape the include path uses.
	policy := access.NewRolePolicy()
	policy.Grant("auditor", "notes:read:all")
	ctx := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"auditor"})
	if got := eagerScopeFilters(ctx, granted); len(got) != 0 {
		t.Errorf("a caller holding the target's CrossOwnerRead still got %v", got)
	}
	// Without the grant, the same entity stays scoped.
	if got := eagerScopeFilters(context.Background(), granted); len(got) != 1 {
		t.Errorf("an ungranted caller got %d filters on a CrossOwnerRead entity, want 1", len(got))
	}
}
