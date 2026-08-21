package crud

import (
	"context"
	"errors"
	"fmt"
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

// The subquery counts rows without selecting them, so it cannot narrow them by
// selecting — it has to carry the caller's owner and tenant predicates instead.
// This test pins both halves of that: which targets are refused outright, and
// which scope predicates each permitted one comes back carrying. A permitted
// target with no predicate is the count oracle, so "permitted" alone is not the
// assertion.
func TestScopeNestedFiltersForCaller_ScopedTargets(t *testing.T) {
	// Public on every target here: the baseline SESSION gate refuses first
	// otherwise, and the test would pass for the wrong reason. The rule under
	// test is which rows a permitted target is narrowed to — "may they read it
	// at all" is pinned separately, by
	// TestScopeNestedFiltersForCallerStillRefusesUnreadableTargets.
	ownerScoped := gateEntity(t, "notes", entity.EntityConfig{
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
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
	// scopesFor runs the gate and returns the predicates it attached, failing
	// the test if the target was refused.
	scopesFor := func(t *testing.T, ctx context.Context, target string) []scopePredicate {
		t.Helper()
		nf := filterOn(target)
		if err := ch.scopeNestedFiltersForCaller(ctx, nf); err != nil {
			t.Fatalf("target %q was refused: %v", target, err)
		}
		return nf[0].scopes
	}
	wantScope := func(t *testing.T, got []scopePredicate, col, val string) {
		t.Helper()
		for _, sc := range got {
			if sc.Column == col {
				if sc.Value != val {
					t.Errorf("scope on %q binds %q, want %q", col, sc.Value, val)
				}
				return
			}
		}
		t.Errorf("no scope predicate on %q — the EXISTS clause counts every row, which is the oracle: %+v", col, got)
	}

	prev := owner.GetExtractor()
	owner.SetExtractor(func(context.Context) (any, bool) { return "u1", true })
	t.Cleanup(func() { owner.SetExtractor(prev) })

	// An owner filtering across a relation is a legitimate query, and it used
	// to be refused outright. It is permitted now — narrowed to their rows.
	wantScope(t, scopesFor(t, context.Background(), "notes"), "owner_id", "u1")

	// A cross-owner caller may already list the target wholesale, so narrowing
	// would remove a capability without protecting anything.
	if got := scopesFor(t, owner.AllowCrossOwner(context.Background()), "notes"); len(got) != 0 {
		t.Errorf("a cross-owner caller had the subquery narrowed: %+v", got)
	}

	// Tenant behaves the same way on its own axis.
	tctx := tenant.SetTenantID(context.Background(), "tenant-a")
	wantScope(t, scopesFor(t, tctx, "tnotes"), "tenant_id", "tenant-a")
	if got := scopesFor(t, tenant.AllowCrossTenant(context.Background()), "tnotes"); len(got) != 0 {
		t.Errorf("a cross-tenant caller had the subquery narrowed: %+v", got)
	}

	// The two axes are independent, and this is the case that keeps being got
	// wrong: a grant on ONE axis must not lift the other's narrowing. Under the
	// old blanket refusal this showed up as "permitted"; it now shows up as a
	// missing predicate, which is the same bug one layer down.
	wantScope(t, scopesFor(t, owner.AllowCrossOwner(tctx), "tnotes"), "tenant_id", "tenant-a")
	wantScope(t, scopesFor(t, tenant.AllowCrossTenant(context.Background()), "notes"), "owner_id", "u1")

	// No owner in context. CanReadScoped refuses first — an owner-scoped entity
	// with nobody to scope to is not readable — so a caller with no identity
	// never reaches the narrowing at all.
	owner.SetExtractor(func(context.Context) (any, bool) { return nil, false })
	if err := ch.scopeNestedFiltersForCaller(context.Background(), filterOn("notes")); err == nil {
		t.Error("a caller with no owner was permitted to filter across an owner-scoped target")
	}
	// And if that gate ever loosened, the narrowing itself is the backstop: the
	// predicate binds the empty value, which matches no real row. Asserted on
	// the builder directly, because the branch above means the gate cannot
	// reach it — an unreachable fail-closed path is still worth pinning, since
	// the whole oracle is what sits behind it.
	if got := eagerScopeFilters(context.Background(), ownerScoped); len(got) != 1 || got[0].Value != "" {
		t.Errorf("an ownerless caller must be narrowed to the empty value, got %+v", got)
	}
	owner.SetExtractor(func(context.Context) (any, bool) { return "u1", true })

	// An unscoped, readable target carries nothing.
	if got := scopesFor(t, context.Background(), "pnotes"); len(got) != 0 {
		t.Errorf("an unscoped public target gained predicates: %+v", got)
	}

	// A relation naming an entity the registry does not hold. Refusing is
	// load-bearing twice over: filtering against a table nobody vouched for
	// would be wrong, and the probe built below the branch dereferences the
	// resolved target — so falling through would panic rather than merely
	// permit. Nothing else in this file constructs an unresolvable relation.
	if err := ch.scopeNestedFiltersForCaller(context.Background(), filterOn("ghosts")); err == nil {
		t.Error("a relation naming an unregistered entity was permitted — the subquery would filter against an unvouched table")
	} else if !strings.Contains(err.Error(), "ghosts") {
		t.Errorf("the refusal should name the unresolvable target, got %v", err)
	}
}

// A target the caller may not read AT ALL is still refused. Narrowing answers
// "which rows", never "may they look" — an entity behind a read permission
// stays refused however well the subquery is scoped.
func TestScopeNestedFiltersForCallerStillRefusesUnreadableTargets(t *testing.T) {
	gated := gateEntity(t, "secrets", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{
			CRUD:   boolPtrGate(true),
			Access: entity.AccessControl{Read: "secrets:read"},
		},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"secrets": gated}}
	parent := gateEntity(t, "boards", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true},
	})
	ch := &CrudHandler{Entity: parent, Registry: &reg}
	nf := []nestedFilter{{
		Relation: entity.Relation{Type: entity.RelHasMany, Name: "secrets", Entity: "secrets", ForeignKey: "board_id"},
		Field:    "body",
	}}
	err := ch.scopeNestedFiltersForCaller(context.Background(), nf)
	if err == nil {
		t.Fatal("a target behind a read permission was permitted")
	}
	if !strings.Contains(err.Error(), "secrets") {
		t.Errorf("the refusal should name the target, got %v", err)
	}
}

// The predicate has to reach the SQL, qualified against the target's table and
// bound as a placeholder, in every relation shape. A scope attached to the
// struct but dropped on the way to SQL is the same oracle with a passing unit
// test above it.
func TestBuildExistsSubqueryEmitsScopePredicates(t *testing.T) {
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
			nf := nestedFilter{
				Relation: sh.rel, Field: "body", Op: filter.OpEq, Value: "x", table: "note_rows",
				scopes: []scopePredicate{{Column: "owner_id", Value: "u1"}, {Column: "tenant_id", Value: "t1"}},
			}
			sql, args := buildExistsSubquery("boards", "id", nf)
			for _, want := range []string{"note_rows.owner_id = $", "note_rows.tenant_id = $"} {
				if !strings.Contains(sql, want) {
					t.Errorf("missing %q — the subquery counts every row:\n%s", want, sql)
				}
			}
			// Every placeholder must be backed by an arg at its own position,
			// or QueryBuilder renumbers against a shorter list and binds the
			// wrong value into the scope.
			if len(args) != 3 {
				t.Fatalf("args = %v, want both scope values plus the filter value", args)
			}
			// Order matters and is not the order the struct lists them in:
			// QueryBuilder renumbers positionally by encounter, so args must
			// follow the order the placeholders appear in the SQL. The scope
			// clauses are emitted first, so their values come first.
			for i, want := range []any{"u1", "t1", "x"} {
				if args[i] != want {
					t.Errorf("args[%d] = %v, want %v — placeholder $%d binds the wrong value", i, args[i], want, i+1)
				}
				if !strings.Contains(sql, fmt.Sprintf("$%d", i+1)) {
					t.Errorf("no $%d in the SQL for args[%d]:\n%s", i+1, i, sql)
				}
			}
		})
	}
	// A scope column that is not a plain identifier must match nothing rather
	// than be dropped: dropping it widens the subquery back to every row.
	nf := nestedFilter{
		Relation: entity.Relation{Type: entity.RelHasMany, Entity: "notes", ForeignKey: "board_id"},
		Field:    "body", Op: filter.OpEq, Value: "x", table: "notes",
		scopes: []scopePredicate{{Column: "owner_id; DROP TABLE notes --", Value: "u1"}},
	}
	if sql, _ := buildExistsSubquery("boards", "id", nf); sql != "1 = 0" {
		t.Errorf("an unsafe scope column produced %q, want an unconditionally-false predicate", sql)
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
