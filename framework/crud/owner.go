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
	a := ch.Entity.Config.Exposure.Access
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

// CanRead reports whether ctx carries the permission EntityConfig.Access
// requires to read this entity. It answers true when the entity declares no
// read permission.
//
// This answers RBAC ONLY. It is not the whole read posture and is almost never
// what a new caller wants: a default-posture entity (no OwnerField, no Access,
// no Public) declares no read permission, so this returns true for an anonymous
// caller while GET /api/<entity> answers 401.
//
// Prefer CanReadScoped, or CanReadRecordScoped for a single record. Those are
// what framework/ui/resource gates on, including its island handler. CanRead
// survives there only as the compatibility fallback for a custom DataSource
// written before CanReadScoped existed, load-bearing, not dead. Reach for it
// directly only when you specifically want the RBAC question in isolation.
func (ch *CrudHandler) CanRead(ctx context.Context) bool {
	perm := ch.permissionForOp(opRead)
	if perm == "" {
		return true
	}
	return access.CanResource(ctx, access.Permission(perm), access.Ref{Type: ch.Entity.GetName()})
}

// CanReadScoped reports whether ctx may read this entity's rows: the baseline
// session requirement, owner scoping, tenant scoping, and RBAC, as a boolean
// with no HTTP response written.
//
// It answers the STRICTER of itself and requireScope(opRead) wherever the two
// could differ. In particular it does not honour owner.AllowCrossOwner, which
// the in-process requireOwnerContext does and the HTTP route's RequireOwner
// does not: that marker is exported, so a host could apply it to a request
// context, and a rendering surface trusting the looser answer would show rows
// the REST route refuses. Read scoping is still a separate implementation of
// one rule and will drift if edited without its counterpart.
//
// It is COLLECTION-level: the RBAC check asks about the entity, not a specific
// record id, so a resource-aware Decider that denies one row is not consulted.
// Use it to decide whether a caller may see a listing. For a single record,
// the record's own read must still go through the CRUD route or an equivalent
// per-id check.
//
// CanRead alone answers only the RBAC question, which is not the whole read
// posture. Auto-CRUD is secure by default: an entity declaring no OwnerField,
// no Access, and no Public requires a session for every operation, and that
// rule lives in requireAuthenticated rather than in Access. A surface that
// gated on CanRead alone therefore served every row of a default-posture
// entity to anonymous callers while GET /api/<entity> answered 401, which is
// exactly what generated list screens did.
//
// Use this from any surface that renders the same rows outside the CRUD routes
// (a server-rendered table, an island fragment, a report). See
// framework/ui/resource.
func (ch *CrudHandler) CanReadScoped(ctx context.Context) bool {
	return ch.canReadScopedRecord(ctx, "")
}

// CanReadRecordScoped is CanReadScoped for ONE record.
//
// The difference matters only when a resource-aware Decider is installed
// (access.WithDecider): the decider is asked about access.Ref{Type, ID}, so it
// can allow the listing and deny an individual row, "member may read project
// 42" is the whole point of that seam. The HTTP read-one route already passes
// r.PathValue("id") into the check; a screen rendering the same record with the
// collection-level predicate would show a row the API refuses by id.
//
// Pass the record id for a detail or edit view. With no decider installed this
// answers exactly what CanReadScoped answers.
func (ch *CrudHandler) CanReadRecordScoped(ctx context.Context, id string) bool {
	return ch.canReadScopedRecord(ctx, id)
}

func (ch *CrudHandler) canReadScopedRecord(ctx context.Context, id string) bool {
	cfg := ch.Entity.Config
	// Baseline session requirement, the boolean mirror of requireAuthenticated.
	if cfg.Scope.OwnerField == "" && !cfg.Exposure.Access.Declared() && !cfg.Exposure.Public {
		if _, ok := handler.GetUser(ctx); !ok {
			return false
		}
	}
	// Deliberately NOT requireOwnerContext: that honours owner.AllowCrossOwner,
	// and the HTTP route's RequireOwner does not. The divergence used to be
	// unreachable trivia; three rendering surfaces now trust this boolean, so a
	// host applying the exported marker to a request context would make screens
	// render rows the REST route refuses. Answer the stricter of the two.
	if field := cfg.Scope.OwnerField; field != "" {
		if _, ok := owner.Get(ctx); !ok {
			return false
		}
	}
	if err := ch.requireTenantContext(ctx); err != nil {
		return false
	}
	perm := ch.permissionForOp(opRead)
	if perm == "" {
		return true
	}
	return access.CanResource(ctx, access.Permission(perm), access.Ref{Type: ch.Entity.GetName(), ID: id})
}

// canReadEntityGate answers the part of the read posture that is a GATE rather
// than a row filter: the baseline session requirement and RBAC.
//
// Owner and tenant are deliberately excluded. They scope WHICH rows a caller
// sees, and the eager loaders already apply them per node
// (applyRelatedOwnerScope / applyRelatedTenantScope), so a missing owner or
// tenant yields zero rows rather than a refusal, the framework's existing,
// tested fail-closed behavior. Folding them in here would convert that
// filtering into a 403 and change the answer for callers who are legitimately
// scoped to nothing.
//
// Use this ONLY where the row scoping is provably applied separately. Today
// that is exactly one caller: the include gate, whose eager loaders call
// applyRelatedOwnerScope and applyRelatedTenantScope per node. Nested filters
// looked like the same case and are not, their EXISTS subquery emits no owner
// or tenant predicate, so choosing this gate there left a live count oracle
// over rows the target's own route refuses. If you are reaching for this
// predicate, first find the code that applies owner and tenant for your path;
// if you cannot point at it, you want CanReadScoped.
func (ch *CrudHandler) canReadEntityGate(ctx context.Context) bool {
	cfg := ch.Entity.Config
	if cfg.Scope.OwnerField == "" && !cfg.Exposure.Access.Declared() && !cfg.Exposure.Public {
		if _, ok := handler.GetUser(ctx); !ok {
			return false
		}
	}
	return ch.CanRead(ctx)
}

// requirePermission enforces EntityConfig.Access for op. When the entity
// declares a permission for the operation and the request context does not
// carry it, it writes 403 and returns false. No-op when the operation is not
// gated.
//
// The check goes through access.CanResource so a resource-aware Decider
// installed in ctx (via access.WithDecider / access.DeciderMiddleware) is
// consulted before the role policy, the issue #80 seam for per-resource
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
// MultiTenant entity with no tenant id in the context. Fails closed,
// the HTTP layer normally refuses these requests at middleware, but
// in-process callers (typed repos, jobs, scripts) bypass that path.
var errTenantRequired = errors.New("tenant context required for multi-tenant entity")

// brokeredCallKey marks a context as a process-module broker re-dispatch.
// A module never brokers data in a cross-owner frame (design #37 §5), so a
// brokered call must never exercise CrossOwnerRead, even when the
// re-resolved caller legitimately holds it in their own session. The
// unexported key type means no HTTP-derived context can carry it: it is set
// ONLY by the broker (framework/processmodule_broker.go resolveCaller) via
// WithBrokeredCall, exactly mirroring owner.crossOwnerKey / AllowCrossOwner.
type brokeredCallKey struct{}

// WithBrokeredCall stamps ctx as originating from the process-module
// broker's CRUD re-dispatch. crossOwnerReadGranted returns false
// unconditionally when it is present, so owner scoping holds by
// construction for every brokered call regardless of the re-resolved
// caller's permissions. SECURITY: set ONLY from the broker's resolveCaller.
func WithBrokeredCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, brokeredCallKey{}, true)
}

// IsBrokeredCall reports whether ctx was stamped by the process-module
// broker. Exported so the broker test surface can pin the root-cause marker.
func IsBrokeredCall(ctx context.Context) bool {
	v, _ := ctx.Value(brokeredCallKey{}).(bool)
	return v
}

// crossOwnerReadGranted reports whether the request context holds the
// entity's declared CrossOwnerRead permission. Returns false when the
// entity does not opt in (empty permission), when access.Can denies
// (including the fail-closed "no policy in context" case), and, the F3
// root-cause fix, unconditionally when the call was brokered through a
// process module, so a delegated caller who holds CrossOwnerRead cannot
// exercise it through a module. READ-ONLY by construction: only
// ApplyOwnerScope / ApplyOwnerScopeCount consult it.
func (ch *CrudHandler) crossOwnerReadGranted(ctx context.Context) bool {
	if IsBrokeredCall(ctx) {
		return false
	}
	perm := ch.Entity.Config.Scope.CrossOwnerRead
	return perm != "" && access.Can(ctx, access.Permission(perm))
}

// ApplyOwnerScope adds an `<owner_field> = ?` predicate to a SELECT query
// when the entity declares OwnerField and the request context carries an
// owner id (registered via framework/owner.SetExtractor, typically by
// battery/auth's init()). No-op when either condition is missing.
//
// Uses PostgreSQL-style $N placeholders, matching ApplyTenantScope.
func (ch *CrudHandler) ApplyOwnerScope(qb *query.QueryBuilder, r *http.Request) {
	field := ch.Entity.Config.Scope.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) || ch.crossOwnerReadGranted(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		qb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeCount mirrors ApplyOwnerScope for count queries.
func (ch *CrudHandler) ApplyOwnerScopeCount(cb *query.CountBuilder, r *http.Request) {
	field := ch.Entity.Config.Scope.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) || ch.crossOwnerReadGranted(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		cb.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeUpdate mirrors ApplyOwnerScope for UPDATE queries.
func (ch *CrudHandler) ApplyOwnerScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	field := ch.Entity.Config.Scope.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		ub.Where(field+" = $1", id)
	}
}

// ApplyOwnerScopeDelete mirrors ApplyOwnerScope for DELETE queries.
func (ch *CrudHandler) ApplyOwnerScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	field := ch.Entity.Config.Scope.OwnerField
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
	if ch.Entity.Config.Scope.OwnerField == "" {
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
	if !ch.Entity.Config.Scope.MultiTenant {
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
	field := ch.Entity.Config.Scope.OwnerField
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
// requires an owner but none is available, the caller MUST refuse the
// request. Writes 401 to w and returns ok=false in that case so handlers
// can `if _, ok := ch.RequireOwner(w, r); !ok { return }`.
//
// This is the secure-by-default seam: without it, ApplyOwnerScope would
// silently no-op for anonymous requests on OwnerField entities, returning
// every row in the table. With OwnerField set the framework refuses
// requests that can't produce an owner id, regardless of whether the
// caller mounted auth middleware in front of the route.
func (ch *CrudHandler) RequireOwner(w http.ResponseWriter, r *http.Request) (id any, ok bool) {
	if ch.Entity.Config.Scope.OwnerField == "" {
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
// opts into an Access block, an entity declaring NEITHER got zero
// enforcement, so a plain blueprint entity's List/Get/Create/Update/Delete
// were all reachable by an anonymous caller (POST returning 201 and
// persisting the row). Unless an explicit mechanism already governs the
// entity (OwnerField or a declared Access block, either "takes over as
// today") or the entity opts all the way out (Config.Public, a
// deliberate "yes, this is a public form/feed" declaration, e.g. a public
// contact form or a blog's comments), an authenticated session is
// required for every operation.
//
// Mirrors the "baseline auth check" EventStream has carried since the SSE
// fix (see EventStream in crud_events.go), same core/handler.GetUser
// signal, generalized to every CRUD entrypoint instead of just the SSE
// feed.
func (ch *CrudHandler) requireAuthenticated(w http.ResponseWriter, r *http.Request, op crudOp) bool {
	cfg := ch.Entity.Config
	if cfg.Scope.OwnerField != "" || cfg.Exposure.Access.Declared() || cfg.Exposure.Public {
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
// the request carries no tenant id, the caller MUST refuse the request. Writes
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
	if !ch.Entity.Config.Scope.MultiTenant {
		return true
	}
	ctx := r.Context()
	if tenantIDFromCtx(ctx) == "" && !tenant.IsCrossTenant(ctx) {
		writeJSONError(w, http.StatusUnauthorized, "tenant context required")
		return false
	}
	return true
}
