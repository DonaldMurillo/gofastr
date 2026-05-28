package crud

import (
	"context"
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// tenantIDFromCtx is a thin wrapper so owner.go doesn't drag the
// framework/tenant package across every helper signature.
func tenantIDFromCtx(ctx context.Context) string {
	return tenant.GetTenantID(ctx)
}

// errOwnerRequired signals a write attempt against an OwnerField entity
// without an authenticated caller in the context. In-process APIs
// (UpsertOne) bubble this up so callers can map to 401.
var errOwnerRequired = errors.New("owner context required for owner-scoped entity")

// errTenantRequired signals an in-process CRUD call against a
// MultiTenant entity with no tenant id in the context. Fails closed —
// the HTTP layer normally refuses these requests at middleware, but
// in-process callers (typed repos, jobs, scripts) bypass that path.
var errTenantRequired = errors.New("tenant context required for multi-tenant entity")

// ApplyOwnerScope adds an `<owner_field> = ?` predicate to a SELECT query
// when the entity declares OwnerField and the request context carries an
// owner id (registered via framework/owner.SetExtractor — typically by
// battery/auth's init()). No-op when either condition is missing.
//
// Uses PostgreSQL-style $N placeholders, matching ApplyTenantScope.
func (ch *CrudHandler) ApplyOwnerScope(qb *query.QueryBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		qb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeCount mirrors ApplyOwnerScope for count queries.
func (ch *CrudHandler) ApplyOwnerScopeCount(cb *query.CountBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		cb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeUpdate mirrors ApplyOwnerScope for UPDATE queries.
func (ch *CrudHandler) ApplyOwnerScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		ub.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeDelete mirrors ApplyOwnerScope for DELETE queries.
func (ch *CrudHandler) ApplyOwnerScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		db.Where(field+" = $1", id)
	}
}

// requireOwnerContext is the in-process mirror of RequireOwner: it
// returns errOwnerRequired when the entity declares OwnerField and the
// context carries no extractable owner id. Used by in-process APIs
// (UpsertOne, in-process Create variants) where there's no
// http.ResponseWriter to write a 401 to.
func (ch *CrudHandler) requireOwnerContext(ctx context.Context) error {
	if ch.Entity.Config.OwnerField == "" {
		return nil
	}
	if _, ok := owner.Get(ctx); !ok {
		return errOwnerRequired
	}
	return nil
}

// requireTenantContext returns errTenantRequired when the entity is
// configured for multi-tenancy and the context carries no tenant id.
// Wired into every in-process CRUD method that touches DB state, so a
// MultiTenant entity can never be queried unscoped through this API.
func (ch *CrudHandler) requireTenantContext(ctx context.Context) error {
	if !ch.Entity.Config.MultiTenant {
		return nil
	}
	if tenantIDFromCtx(ctx) == "" {
		return errTenantRequired
	}
	return nil
}

// InjectOwner stamps the owner id into a Create payload when the entity
// declares OwnerField. Mirrors InjectTenant's shape.
func (ch *CrudHandler) InjectOwner(data map[string]any, ctx context.Context) {
	field := ch.Entity.Config.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(ctx); ok {
		data[field] = id
	}
}

// RequireOwner returns the current owner id when the entity declares
// OwnerField. ok=true means: either no owner is required (entity has no
// OwnerField), or an owner was extracted. ok=false means: the entity
// requires an owner but none is available — the caller MUST refuse the
// request. Writes 401 to w and returns ok=false in that case so handlers
// can `if _, ok := ch.RequireOwner(w, r); !ok { return }`.
//
// This is the secure-by-default seam: without it, ApplyOwnerScope would
// silently no-op for anonymous requests on OwnerField entities, returning
// every row in the table. With OwnerField set the framework refuses
// requests that can't produce an owner id, regardless of whether the
// caller mounted auth middleware in front of the route.
func (ch *CrudHandler) RequireOwner(w http.ResponseWriter, r *http.Request) (id any, ok bool) {
	if ch.Entity.Config.OwnerField == "" {
		return nil, true
	}
	id, found := owner.Get(r.Context())
	if !found {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return id, true
}
