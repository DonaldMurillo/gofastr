package crud

import (
	"context"
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// crudOp identifies which CRUD operation a request is performing, so the
// permission gate can pick the right EntityConfig.Access permission.
type crudOp int

const (
	opRead crudOp = iota // List + Get
	opCreate
	opUpdate
	opDelete
)

// permissionForOp returns the declared RBAC permission for op, or "" when the
// operation is not RBAC-gated.
func (ch *CrudHandler) permissionForOp(op crudOp) string {
	a := ch.Entity.Config.Access
	switch op {
	case opCreate:
		return a.Create
	case opUpdate:
		return a.Update
	case opDelete:
		return a.Delete
	default:
		return a.Read
	}
}

// requirePermission enforces EntityConfig.Access for op. When the entity
// declares a permission for the operation and the request context does not
// carry it, it writes 403 and returns false. No-op when the operation is not
// gated.
//
// The check goes through access.CanResource so a resource-aware Decider
// installed in ctx (via access.WithDecider / access.DeciderMiddleware) is
// consulted before the role policy — the issue #80 seam for per-resource
// authority ("member may edit project 42"). recordID is the path id for
// item-scoped ops (read-one/update/delete) and "" for collection-level ops
// (list/create/batch/the SSE feed); with no decider configured, CanResource
// answers exactly what access.Can answered, so behaviour is byte-identical.
func (ch *CrudHandler) requirePermission(w http.ResponseWriter, r *http.Request, op crudOp, recordID string) bool {
	perm := ch.permissionForOp(op)
	if perm == "" {
		return true
	}
	resource := access.Ref{Type: ch.Entity.GetName(), ID: recordID}
	if !access.CanResource(r.Context(), access.Permission(perm), resource) {
		writeJSONError(w, http.StatusForbidden, "access denied: missing permission "+perm)
		return false
	}
	return true
}

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

// crossOwnerReadGranted reports whether the request context holds the
// entity's declared CrossOwnerRead permission. Returns false when the
// entity does not opt in (empty permission) or when access.Can denies
// (including the fail-closed "no policy in context" case). READ-ONLY by
// construction: only ApplyOwnerScope / ApplyOwnerScopeCount consult it.
func (ch *CrudHandler) crossOwnerReadGranted(ctx context.Context) bool {
	perm := ch.Entity.Config.CrossOwnerRead
	return perm != "" && access.Can(ctx, access.Permission(perm))
}

// ApplyOwnerScope adds an `<owner_field> = ?` predicate to a SELECT query
// when the entity declares OwnerField and the request context carries an
// owner id (registered via framework/owner.SetExtractor — typically by
// battery/auth's init()). No-op when either condition is missing.
//
// Uses PostgreSQL-style $N placeholders, matching ApplyTenantScope.
func (ch *CrudHandler) ApplyOwnerScope(qb *query.QueryBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) || ch.crossOwnerReadGranted(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		qb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeCount mirrors ApplyOwnerScope for count queries.
func (ch *CrudHandler) ApplyOwnerScopeCount(cb *query.CountBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) || ch.crossOwnerReadGranted(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		cb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeUpdate mirrors ApplyOwnerScope for UPDATE queries.
func (ch *CrudHandler) ApplyOwnerScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		ub.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeDelete mirrors ApplyOwnerScope for DELETE queries.
func (ch *CrudHandler) ApplyOwnerScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	field := ch.Entity.Config.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) {
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
	if owner.IsCrossOwner(ctx) {
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
	if tenantIDFromCtx(ctx) == "" && !tenant.IsCrossTenant(ctx) {
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

// requireAuthenticated is the secure-by-default gate that closes the
// anonymous-CRUD hole tracked in issue #65: RequireOwner only fires for
// OwnerField entities and requirePermission only fires when the entity
// opts into an Access block — an entity declaring NEITHER got zero
// enforcement, so a plain blueprint entity's List/Get/Create/Update/Delete
// were all reachable by an anonymous caller (POST returning 201 and
// persisting the row). Unless an explicit mechanism already governs the
// entity (OwnerField or a declared Access block — either "takes over as
// today") or the entity opts all the way out (Config.Public — a
// deliberate "yes, this is a public form/feed" declaration, e.g. a public
// contact form or a blog's comments), an authenticated session is
// required for every operation.
//
// Mirrors the "baseline auth check" EventStream has carried since the SSE
// fix (see EventStream in crud_events.go) — same core/handler.GetUser
// signal, generalized to every CRUD entrypoint instead of just the SSE
// feed.
func (ch *CrudHandler) requireAuthenticated(w http.ResponseWriter, r *http.Request, op crudOp) bool {
	cfg := ch.Entity.Config
	if cfg.OwnerField != "" || cfg.Access.Declared() || cfg.Public {
		return true // an explicit mechanism already governs this entity
	}
	if _, ok := handler.GetUser(r.Context()); !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	return true
}

// requireScope runs every secure-by-default access gate for an HTTP request in
// one place: owner (OwnerField entities), tenant (MultiTenant entities), the
// baseline session requirement (requireAuthenticated), and RBAC
// (requirePermission). It returns false after writing the appropriate
// 401/403 when any gate fails, so handlers can guard with
// `if !ch.requireScope(w, r, op) { return }`. Keeping every gate behind a
// single chokepoint guarantees a new handler can't accidentally enforce one
// scope but forget another.
func (ch *CrudHandler) requireScope(w http.ResponseWriter, r *http.Request, op crudOp) bool {
	if _, ok := ch.RequireOwner(w, r); !ok {
		return false
	}
	if !ch.RequireTenant(w, r) {
		return false
	}
	if !ch.requireAuthenticated(w, r, op) {
		return false
	}
	return ch.requirePermission(w, r, op, r.PathValue("id"))
}

// RequireTenant is the HTTP mirror of RequireOwner for multi-tenant entities.
// ok=true means: either the entity is not MultiTenant, or a tenant id is
// present in the request context. ok=false means the entity is MultiTenant but
// the request carries no tenant id — the caller MUST refuse the request. Writes
// 401 to w and returns ok=false in that case so handlers can
// `if !ch.RequireTenant(w, r) { return }`.
//
// This is the secure-by-default seam matching requireTenantContext (the
// in-process mirror). Without it, ApplyTenantScope* silently no-op when no
// tenant is in context, leaking every tenant's rows on read and permitting
// cross-tenant update/delete-by-id. Hosts that genuinely need cross-tenant
// access (admin tooling) must set a tenant id deliberately rather than rely on
// an empty context.
func (ch *CrudHandler) RequireTenant(w http.ResponseWriter, r *http.Request) (ok bool) {
	if !ch.Entity.Config.MultiTenant {
		return true
	}
	ctx := r.Context()
	if tenantIDFromCtx(ctx) == "" && !tenant.IsCrossTenant(ctx) {
		writeJSONError(w, http.StatusUnauthorized, "tenant context required")
		return false
	}
	return true
}
