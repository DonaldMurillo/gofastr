package crud

import (
	"context"
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

type crudOp int

const (
	opRead crudOp = iota // List + Get
	opCreate
	opUpdate
	opDelete
)

// permRefused is the permission of a write a declared Access block leaves blank.
const permRefused = "(none)"

// permissionForOp returns "" when op is not RBAC-gated.
func (ch *CrudHandler) permissionForOp(op crudOp) string {
	a := ch.Entity.Config.Exposure.Access
	var perm string
	switch op {
	case opCreate:
		perm = a.Create
	case opUpdate:
		perm = a.Update
	case opDelete:
		perm = a.Delete
	default:
		perm = a.Read
	}
	switch {
	case perm == entity.Anyone:
		return ""
	case perm == "" && op != opRead && a.Declared():
		return permRefused
	}
	return perm
}

// CanRead reports whether ctx holds the Access permission to read this
// entity, true when none is declared. It answers RBAC only, not the read
// posture the routes enforce: gate a screen on CanReadScoped instead.
func (ch *CrudHandler) CanRead(ctx context.Context) bool {
	perm := ch.permissionForOp(opRead)
	if perm == "" {
		return true
	}
	return ch.accessAllows(ctx, perm, "")
}

// CanReadScoped reports whether ctx may read this entity's rows, the answer
// the list route gives: owner, tenant, the baseline session and RBAC. It is
// collection-level; for one record use CanReadRecordScoped.
func (ch *CrudHandler) CanReadScoped(ctx context.Context) bool {
	return ch.canScopedRecord(ctx, opRead, "")
}

// CanReadRecordScoped is CanReadScoped for record id, which a Decider is
// asked about.
func (ch *CrudHandler) CanReadRecordScoped(ctx context.Context, id string) bool {
	return ch.canScopedRecord(ctx, opRead, id)
}

// CanReadRelatedScoped is CanReadScoped for this entity reached through
// another entity's relation (a label, facet or picker). It also refuses what
// ?include= refuses: CRUD off, or a scoped request without "<table>:read".
func (ch *CrudHandler) CanReadRelatedScoped(ctx context.Context) bool {
	return ch.relationReachable(ctx, "read") && ch.CanReadScoped(ctx)
}

// canCascadeWrite is requireScope for a cascade write reaching this entity.
func (ch *CrudHandler) canCascadeWrite(ctx context.Context, r *http.Request, op crudOp, id string) bool {
	if serverWrites(ctx) {
		return true
	}
	if (r == nil || !inProcess(r)) && !ch.relationReachable(ctx, "write") {
		return false
	}
	if ch.requireOwnerContext(ctx) != nil || ch.requireTenantContext(ctx) != nil {
		return false
	}
	if ch.sessionGated() && (r == nil || !inProcess(r)) {
		if _, ok := handler.GetUser(ctx); !ok {
			return false
		}
	}
	perm := ch.permissionForOp(op)
	if perm == "" {
		return true
	}
	return ch.accessAllows(ctx, perm, id)
}

// relationReachable reports whether another entity's route may reach this
// entity's rows through a relation (?include=, ?rel.field=, a cascade write).
func (ch *CrudHandler) relationReachable(ctx context.Context, verb string) bool {
	if crud := ch.Entity.Config.Exposure.CRUD; crud != nil && !*crud {
		return false
	}
	return access.ScopeAllows(ctx, access.Permission(ch.Entity.GetTable()+":"+verb))
}

// canReadEntityGate is the read posture without owner and tenant: the
// baseline session and RBAC.
func (ch *CrudHandler) canReadEntityGate(ctx context.Context) bool {
	return ch.sessionGate(ctx) == nil && ch.CanRead(ctx)
}

func (ch *CrudHandler) requirePermission(w http.ResponseWriter, r *http.Request, op crudOp, recordID string) bool {
	return ch.permissionGate(r.Context(), op, recordID).pass(w, r)
}

// gateRefusal is the answer a refusing gate writes; nil lets the caller through.
type gateRefusal struct {
	status int
	reason i18n.Phrase
	params map[string]any
}

func (g *gateRefusal) pass(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case g == nil:
		return true
	case g.params != nil:
		writeJSONError(w, r, g.status, g.reason, g.params)
	default:
		writeJSONError(w, r, g.status, g.reason)
	}
	return false
}

// scopeGate is the refusal, if any, for ctx running op on record id.
func (ch *CrudHandler) scopeGate(ctx context.Context, op crudOp, id string) *gateRefusal {
	if g := ch.ownerGate(ctx); g != nil {
		return g
	}
	if g := ch.tenantGate(ctx); g != nil {
		return g
	}
	if g := ch.sessionGate(ctx); g != nil {
		return g
	}
	return ch.permissionGate(ctx, op, id)
}

func (ch *CrudHandler) ownerGate(ctx context.Context) *gateRefusal {
	if ch.Entity.Config.Scope.OwnerField == "" {
		return nil
	}
	if _, ok := owner.Get(ctx); !ok {
		return &gateRefusal{status: http.StatusUnauthorized, reason: handler.ReasonAuthenticationRequired}
	}
	return nil
}

func (ch *CrudHandler) tenantGate(ctx context.Context) *gateRefusal {
	if ch.requireTenantContext(ctx) != nil {
		return &gateRefusal{status: http.StatusUnauthorized, reason: reasonTenantRequired}
	}
	return nil
}

func (ch *CrudHandler) sessionGate(ctx context.Context) *gateRefusal {
	if !ch.sessionGated() {
		return nil
	}
	if _, ok := handler.GetUser(ctx); !ok {
		return &gateRefusal{status: http.StatusUnauthorized, reason: handler.ReasonAuthenticationRequired}
	}
	return nil
}

func (ch *CrudHandler) permissionGate(ctx context.Context, op crudOp, id string) *gateRefusal {
	perm := ch.permissionForOp(op)
	if perm == "" || ch.accessAllows(ctx, perm, id) {
		return nil
	}
	if perm == permRefused {
		return &gateRefusal{status: http.StatusForbidden, reason: reasonOpRefused}
	}
	return &gateRefusal{status: http.StatusForbidden, reason: handler.ReasonMissingPermission, params: map[string]any{"permission": string(perm)}}
}

func (ch *CrudHandler) itemPermitted(ctx context.Context, op crudOp, id string) bool {
	perm := ch.permissionForOp(op)
	if perm == "" {
		return true
	}
	return ch.accessAllows(ctx, perm, id)
}

func tenantIDFromCtx(ctx context.Context) string {
	return tenant.GetTenantID(ctx)
}

var errOwnerRequired = errors.New("owner context required for owner-scoped entity")

var errTenantRequired = errors.New("tenant context required for multi-tenant entity")

type brokeredCallKey struct{}

// WithBrokeredCall marks ctx as a process-module broker re-dispatch, which
// never exercises Scope.CrossOwnerRead. SECURITY: set it only from the
// broker's resolveCaller.
func WithBrokeredCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, brokeredCallKey{}, true)
}

// IsBrokeredCall reports whether ctx carries WithBrokeredCall.
func IsBrokeredCall(ctx context.Context) bool {
	v, _ := ctx.Value(brokeredCallKey{}).(bool)
	return v
}

func (ch *CrudHandler) crossOwnerReadGranted(ctx context.Context) bool {
	if IsBrokeredCall(ctx) {
		return false
	}
	perm := ch.Entity.Config.Scope.CrossOwnerRead
	return perm != "" && access.CanResource(ctx, access.Permission(perm),
		access.Ref{Type: ch.Entity.GetName(), ID: ""})
}

// ApplyOwnerScope adds `<owner_field> = $1` to a SELECT when the entity
// declares OwnerField and the request carries an owner id; otherwise, or when
// the caller holds Scope.CrossOwnerRead, it adds nothing.
func (ch *CrudHandler) ApplyOwnerScope(qb *query.QueryBuilder, r *http.Request) {
	applyOwnerScope(ch, qb, r, true)
}

// ApplyOwnerScopeCount mirrors ApplyOwnerScope for count queries.
func (ch *CrudHandler) ApplyOwnerScopeCount(cb *query.CountBuilder, r *http.Request) {
	applyOwnerScope(ch, cb, r, true)
}

// ApplyOwnerScopeUpdate mirrors ApplyOwnerScope for UPDATE queries, without
// the CrossOwnerRead lift.
func (ch *CrudHandler) ApplyOwnerScopeUpdate(ub *query.UpdateBuilder, r *http.Request) {
	applyOwnerScope(ch, ub, r, false)
}

// ApplyOwnerScopeDelete mirrors ApplyOwnerScopeUpdate for DELETE queries.
func (ch *CrudHandler) ApplyOwnerScopeDelete(db *query.DeleteBuilder, r *http.Request) {
	applyOwnerScope(ch, db, r, false)
}

// applyOwnerScope backs ApplyOwnerScope*; read allows the CrossOwnerRead lift.
func applyOwnerScope[B interface{ Where(string, ...any) B }](ch *CrudHandler, b B, r *http.Request, read bool) {
	field := ch.Entity.Config.Scope.OwnerField
	if field == "" || owner.IsCrossOwner(r.Context()) {
		return
	}
	if read && ch.crossOwnerReadGranted(r.Context()) {
		return
	}
	if id, ok := owner.Get(r.Context()); ok {
		b.Where(query.QuoteKeyword(field)+" = $1", id)
	}
}

// requireOwnerContext is the in-process RequireOwner. Unlike ownerGate it
// honours owner.AllowCrossOwner.
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

func (ch *CrudHandler) requireTenantContext(ctx context.Context) error {
	if !ch.Entity.Config.Scope.MultiTenant {
		return nil
	}
	if tenantIDFromCtx(ctx) == "" && !tenant.IsCrossTenant(ctx) {
		return errTenantRequired
	}
	return nil
}

// InjectOwner sets the OwnerField of a create payload to ctx's owner id.
func (ch *CrudHandler) InjectOwner(data map[string]any, ctx context.Context) {
	field := ch.Entity.Config.Scope.OwnerField
	if field == "" {
		return
	}
	if id, ok := owner.Get(ctx); ok {
		data[field] = id
	}
}

// RequireOwner returns the request's owner id. On an OwnerField entity with
// no owner in the request it writes 401 and returns ok=false, which the
// caller must answer by refusing; an entity with no OwnerField returns
// nil, true.
func (ch *CrudHandler) RequireOwner(w http.ResponseWriter, r *http.Request) (id any, ok bool) {
	if !ch.ownerGate(r.Context()).pass(w, r) {
		return nil, false
	}
	if ch.Entity.Config.Scope.OwnerField == "" {
		return nil, true
	}
	id, _ = owner.Get(r.Context())
	return id, true
}

func (ch *CrudHandler) sessionGated() bool {
	cfg := ch.Entity.Config
	return cfg.Scope.OwnerField == "" && !cfg.Exposure.Access.Declared() && !cfg.Exposure.Public
}

// RequireRead runs the gates the entity's list route runs and, when one
// refuses, writes the refusal that route writes and returns false. It answers
// what CanReadScoped answers.
func (ch *CrudHandler) RequireRead(w http.ResponseWriter, r *http.Request) bool {
	return ch.scopeGate(r.Context(), opRead, "").pass(w, r)
}

func (ch *CrudHandler) requireScope(w http.ResponseWriter, r *http.Request, op crudOp) bool {
	return ch.scopeGate(r.Context(), op, ch.pathID(r)).pass(w, r)
}

// RequireTenant is RequireOwner for a MultiTenant entity: with no tenant and
// no cross-tenant grant in the request it writes 401 and returns false.
func (ch *CrudHandler) RequireTenant(w http.ResponseWriter, r *http.Request) (ok bool) {
	return ch.tenantGate(r.Context()).pass(w, r)
}
