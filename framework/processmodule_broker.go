package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Broker is the production capability broker for the module reverse channel
// (design #37 §5). It implements [ReverseBroker]: it installs the host.*
// reverse-request handlers on each child's [moduleproto.Peer] and mints
// per-call delegation handles that let a reverse call re-attach the
// originating end-user request's caller context to the CRUD re-dispatch.
//
// The capability model is module-grant ∩ caller-authority (design §5):
//
//   - The MODULE half is [access.ScopeMatch] of the derived required
//     permission against the operator-approved grant view, run BEFORE any
//     dispatch. The required permission is derived from the TRUSTED method +
//     canonical host resource (entity name / "search" / event topic), NEVER
//     from a child-supplied capability string (the confused-deputy control).
//   - The CALLER half is the CRUD chokepoint re-entered via [crud.Redispatch]
//     through the router so owner/tenant/permission + token-scope re-run on
//     the re-attached caller identity.
//
// The CrossOwnerRead carve-out (design §5) is enforced on BOTH halves: a
// module never brokers data in a cross-owner/cross-tenant frame, so even a
// delegated end-user who legitimately holds CrossOwnerRead in their own
// session cannot exercise it through a module.
//
// Fail-closed by construction: an unknown / expired / released delegation
// handle denies; an ambient (caller-less) reverse call carries no owner/tenant
// id so the CRUD owner/tenant gates refuse (errOwnerRequired / RequireTenant);
// a missing entity/search surface denies rather than silently returning empty.
type Broker struct {
	router    http.Handler       // app router for CRUD re-dispatch (nil → entity ops deny)
	entities  entity.Registry    // entity name → table + CrossOwnerRead perm (nil → entity ops deny)
	events    *event.EventBus    // host.event.emit target (nil → emit denies)
	apiPrefix string             // "" or "/api"; prepended to entity table path
	policy    *access.RolePolicy // optional app-wide policy for ambient synthetic role

	// searchEndpoint, when set, is the http.Handler a host.search.query call
	// re-dispatches through after the perm gate. The framework ships no global
	// search surface in v1; nil → search denies after the perm gate (honest
	// denial, never an empty-results success).
	searchEndpoint http.Handler

	// --- Delegation handle table (design §5): in-memory, replica-local. ---
	// The handle round-trips only within one replica over stdio; an unknown
	// handle denies. No HMAC, no signed token, an exfiltrated handle is
	// meaningless on another replica.
	mu      sync.Mutex
	handles map[string]delegationEntry

	handleTTL time.Duration
	now       func() time.Time
}

// delegationEntry is the snapshot stashed at delegation-mint time. It carries
// exactly what the reverse path needs to (a) re-attach the caller's identity
// to the CRUD re-dispatch (Cookie/Authorization re-injected via crud.Redispatch
// reading mcp.WithRequest) and (b) run the CrossOwnerRead carve-out on the
// delegated caller (roles + policy). module binds the handle to the module
// that minted it (F6) so an app-global handle table cannot be replayed under
// a different module's reverse handler. parentCallID is diagnostic correlation.
type delegationEntry struct {
	cookie        string
	authorization string
	roles         []string
	policy        *access.RolePolicy
	module        string
	parentCallID  uint64
	expires       time.Time
}

// BrokerOption configures a [Broker].
type BrokerOption func(*Broker)

// WithBrokerPolicy installs the app-wide RolePolicy so an ambient (caller-less)
// reverse call can resolve the module's synthetic `module/<name>` role against
// its operator-approved grants. Optional: when nil, ambient re-dispatch is
// anonymous and fails closed on any RBAC-gated entity (still safe, the
// owner/tenant gates refuse regardless).
func WithBrokerPolicy(p *access.RolePolicy) BrokerOption {
	return func(b *Broker) { b.policy = p }
}

// WithBrokerSearchEndpoint wires the host.search.query re-dispatch target.
// Optional; nil → search denies after the perm gate.
func WithBrokerSearchEndpoint(h http.Handler) BrokerOption {
	return func(b *Broker) { b.searchEndpoint = h }
}

// WithBrokerHandleTTL bounds the lifetime of a leaked-but-unreleased handle.
// Default 5m. Tests override to exercise expiry without real sleeps.
func WithBrokerHandleTTL(d time.Duration) BrokerOption {
	return func(b *Broker) { b.handleTTL = d }
}

// WithBrokerClock injects the clock the handle-expiry check reads.
func WithBrokerClock(now func() time.Time) BrokerOption {
	return func(b *Broker) { b.now = now }
}

// NewBroker constructs a capability broker. router/entities/events may be nil;
// a nil dependency makes the corresponding reverse surface deny (fail-closed)
// rather than silently succeed, the supervisor's [NopBroker] is the all-deny
// degenerate case. apiPrefix is the app's API prefix ("" or "/api") used to
// build entity CRUD re-dispatch paths.
func NewBroker(router http.Handler, entities entity.Registry, events *event.EventBus, apiPrefix string, opts ...BrokerOption) *Broker {
	b := &Broker{
		router:    router,
		entities:  entities,
		events:    events,
		apiPrefix: strings.Trim(apiPrefix, "/"),
		handleTTL: 5 * time.Minute,
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Compile-time assertion that Broker satisfies ReverseBroker.
var _ ReverseBroker = (*Broker)(nil)

// InstallHandlers registers the six host.* reverse-request handlers (design
// §4.4) on p, each scoped to view. Handlers are registered once per child
// connection; the view captures the spawn's effective grant set + generation.
func (b *Broker) InstallHandlers(p *moduleproto.Peer, view ModuleGrantView) {
	mustHandle(p, moduleproto.MethodHostEntityQuery, b.entityHandler(view, opQuery))
	mustHandle(p, moduleproto.MethodHostEntityCreate, b.entityHandler(view, opCreate))
	mustHandle(p, moduleproto.MethodHostEntityUpdate, b.entityHandler(view, opUpdate))
	mustHandle(p, moduleproto.MethodHostEntityDelete, b.entityHandler(view, opDelete))
	mustHandle(p, moduleproto.MethodHostSearchQuery, b.searchHandler(view))
	mustHandle(p, moduleproto.MethodHostEventEmit, b.eventHandler(view))
}

func mustHandle(p *moduleproto.Peer, method string, h moduleproto.Handler) {
	if err := p.Handle(method, h); err != nil {
		// Handle only errors on the reserved module.cancel name or empty
		// method; neither is possible for the host.* catalog.
		panic(fmt.Sprintf("processmodule: broker install %q: %v", method, err))
	}
}

// MintDelegation stashes a snapshot of the originating request under a random
// opaque handle and returns it with a release func. The supervisor attaches
// the handle to module.http's Caller block; the child echoes it on reverse
// host.* calls so the broker can re-attach THIS request's caller context to
// the CRUD re-dispatch. release() MUST be called when the parent call returns
// (including buffered-503 crash paths) so the in-memory table does not leak.
// A nil r is the ambient (caller-less) path. MintDelegation is still legal
// but returns an empty handle the broker treats as ambient.
func (b *Broker) MintDelegation(r *http.Request, parentCallID uint64) (string, func()) {
	handle, err := randomHandle()
	if err != nil {
		// Entropy failure is fatal: rand.Read failing means the handle would
		// be the identical all-zero string for every call, and the handle IS
		// the delegation credential. Fail loud, mirrors
		// battery/auth/apitoken.go ("Entropy failure is fatal"). The panic
		// is contained by the host's recovery middleware (→ 503/500); no
		// handle is minted, so no delegation occurs.
		panic(fmt.Sprintf("processmodule broker: mint delegation handle: %v", err))
	}
	entry := delegationEntry{parentCallID: parentCallID, expires: b.now().Add(b.handleTTL)}
	if r != nil {
		entry.cookie = r.Header.Get("Cookie")
		entry.authorization = r.Header.Get("Authorization")
		entry.roles = access.GetRoles(r.Context())
		entry.policy = access.PolicyFromContext(r.Context())
		entry.module = delegationModuleFromCtx(r.Context())
	}
	b.mu.Lock()
	if b.handles == nil {
		b.handles = make(map[string]delegationEntry)
	}
	b.handles[handle] = entry
	b.mu.Unlock()
	return handle, func() {
		b.mu.Lock()
		delete(b.handles, handle)
		b.mu.Unlock()
	}
}

// randomHandle returns a 128-bit opaque delegation handle. The error from
// crypto/rand is propagated (never swallowed): on failure every handle would
// otherwise collapse to the same all-zero string, and the handle is the
// delegation credential. Callers MUST treat a non-nil error as fatal.
func randomHandle() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("read delegation entropy: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// delegationModuleKey carries the module name a delegation handle is being
// minted for (F6). It is stamped on the mint request's context by the
// supervisor's proxy and tool dispatch (the only legitimate minters) and read
// by MintDelegation so resolveCaller can refuse a handle minted for one
// module when it is echoed under a different module's reverse handler. The
// unexported key type means no value can be set except via withDelegationModule.
type delegationModuleKey struct{}

// withDelegationModule stamps ctx with the module name a delegation handle is
// being minted for. The supervisor's proxy (serveProxy) and tool dispatch
// (dispatchToolCall) call this immediately before MintDelegation.
func withDelegationModule(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, delegationModuleKey{}, module)
}

func delegationModuleFromCtx(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	m, _ := ctx.Value(delegationModuleKey{}).(string)
	return m
}

// ----- entity ops -----

type entityOp int

const (
	opQuery entityOp = iota
	opCreate
	opUpdate
	opDelete
)

// verb derives the capability verb for op (design §4.4). create and update
// both map to "write"; the CRUD Access block distinguishes them but the
// module-grant vocabulary collapses the two into a single write scope.
func (op entityOp) verb() string {
	switch op {
	case opCreate, opUpdate:
		return "write"
	case opDelete:
		return "delete"
	default:
		return "read"
	}
}

// entityHandler builds the reverse handler for one host.entity.* method. It
// runs the capability gate once (module-grant pre-filter + CrossOwnerRead
// carve-out on both halves + delegation resolve), then performs the op-shaped
// CRUD re-dispatch. On any denial it returns a [*moduleproto.Error] with
// [moduleproto.CodeCapabilityDenied] so the child receives a per-call denial,
// not a protocol fault.
func (b *Broker) entityHandler(view ModuleGrantView, op entityOp) moduleproto.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		entityName, caller, err := parseEntityCall(op, params)
		if err != nil {
			return nil, err
		}
		ent, rctx, err := b.gate(ctx, entityName, op.verb(), view, caller)
		if err != nil {
			return nil, err
		}
		return b.dispatchEntity(op, ent, rctx, params)
	}
}

// parseEntityCall unmarshals the op's params shape and returns the canonical
// entity name + the echoed CallerRef. query uses EntityQueryParams; the three
// mutations share EntityMutationParams.
func parseEntityCall(op entityOp, params json.RawMessage) (entityName string, caller moduleproto.CallerRef, err error) {
	if op == opQuery {
		var p moduleproto.EntityQueryParams
		if err = unmarshalParams(params, &p); err != nil {
			return "", moduleproto.CallerRef{}, err
		}
		return p.Entity, p.Caller, nil
	}
	var p moduleproto.EntityMutationParams
	if err = unmarshalParams(params, &p); err != nil {
		return "", moduleproto.CallerRef{}, err
	}
	return p.Entity, p.Caller, nil
}

// gate is the capability gate shared by every host.entity.* call. It enforces,
// in order:
//  1. module-grant pre-filter: [access.ScopeMatch](view.Grants, <entity>:<verb>);
//  2. CrossOwnerRead carve-out, module-grant half (belt-and-suspenders): the
//     install-time carve-out already removed these from view.Grants, so a
//     present non-grantable scope means tampering: deny;
//  3. delegation resolve: look up the handle (ambient when empty), denying on
//     unknown/expired/released;
//  4. CrossOwnerRead carve-out, delegated-caller half (belt-and-suspenders):
//     the root-cause guarantee is the brokeredCall marker on the re-dispatch
//     context (crossOwnerReadGranted returns false for it). This gate is the
//     explicit deny: it evaluates the carve-out against the delegated
//     caller's snapshot policy, falling back to the broker's app-wide policy,
//     and DENIES when neither is available (the pre-fix no-op).
//
// It returns the resolved entity (for the re-dispatch path) and the
// re-dispatch context with the caller's identity re-attached.
func (b *Broker) gate(ctx context.Context, entityName, verb string, view ModuleGrantView, caller moduleproto.CallerRef) (*entity.Entity, context.Context, error) {
	// Trust boundary: a child cannot name an arbitrary resource. The resource
	// is the entity it asked for, resolved through the HOST's registry, a
	// name the registry does not know is denied, never silently empty.
	ent := b.lookupEntity(entityName)
	if ent == nil {
		return nil, nil, brokerDeny("entity %q is not a host resource", entityName)
	}

	// (1) Confused-deputy control: the required permission is derived from the
	// trusted method + canonical resource. A child-supplied capability string
	// is never consulted.
	required := access.Permission(entityName + ":" + verb)
	if !access.ScopeMatch(view.Grants, required) {
		return nil, nil, brokerDeny("module grant does not satisfy %q", required)
	}

	// (2) CrossOwnerRead module-grant half (belt-and-suspenders). Install-time
	// validation already removed these; presence here is tamper-detection.
	for _, g := range view.Grants {
		if reason := nonGrantableReason(g); reason != "" {
			return nil, nil, brokerDeny("non-grantable scope in view (%s)", reason)
		}
	}

	// (3) Delegation resolve (ambient when the handle is empty).
	rctx, entry, err := b.resolveCaller(ctx, caller, view)
	if err != nil {
		return nil, nil, err
	}

	// (4) CrossOwnerRead delegated-caller half, belt-and-suspenders. The
	// root-cause guarantee is the brokeredCall marker resolveCaller stamps on
	// the re-dispatch context: crossOwnerReadGranted returns false for it, so
	// owner scoping holds by construction regardless of the re-resolved
	// caller's permissions. This gate is the explicit, visible deny. It
	// evaluates against the delegated caller's snapshot policy when present,
	// falling back to the broker's app-wide policy; when neither is available
	// the carve-out is unevaluatable and we DENY rather than silently permit
	// (the pre-fix behavior, which no-op'd on a nil entry.policy).
	if cor := ent.Config.Scope.CrossOwnerRead; cor != "" {
		policy := b.policy
		roles := []string{moduleRole(view.Name)}
		if entry != nil {
			if entry.policy != nil {
				policy = entry.policy
			}
			if len(entry.roles) > 0 {
				roles = entry.roles
			}
		}
		if policy == nil {
			return nil, nil, brokerDeny("CrossOwnerRead is not brokerable through a module (no policy to evaluate)")
		}
		if access.Can(access.WithPolicy(access.WithRoles(ctx, roles), policy), access.Permission(cor)) {
			return nil, nil, brokerDeny("CrossOwnerRead is not brokerable through a module")
		}
	}
	return ent, rctx, nil
}

// resolveCaller maps the echoed Caller to a re-dispatch context. An empty
// Delegation handle is the ambient path (module's own cron/queue work): the
// synthetic module/<name> role + the broker's app-wide policy are attached so
// the CRUD permission gate can consult the module's grants; owner/tenant
// gates then fail closed (no owner id). A non-empty handle is looked up; an
// unknown/expired/released handle, or one bound to a different module (F6),
// denies. Every returned context is stamped brokeredCall (F3) so the CRUD
// owner gate never lifts scope for a brokered call.
func (b *Broker) resolveCaller(ctx context.Context, caller moduleproto.CallerRef, view ModuleGrantView) (context.Context, *delegationEntry, error) {
	if caller.Delegation == "" {
		// Ambient. Attach the synthetic role; the design's safe-by-construction
		// proof (RequireOwner/RequireTenant refuse without an owner id) holds
		// regardless of whether the host's middleware preserves these roles.
		rctx := access.WithRoles(ctx, []string{moduleRole(view.Name)})
		if b.policy != nil {
			rctx = access.WithPolicy(rctx, b.policy)
		}
		return crud.WithBrokeredCall(rctx), nil, nil
	}
	b.mu.Lock()
	entry, ok := b.handles[caller.Delegation]
	b.mu.Unlock()
	if !ok {
		return nil, nil, brokerDeny("unknown delegation handle")
	}
	if b.now().After(entry.expires) {
		b.mu.Lock()
		delete(b.handles, caller.Delegation)
		b.mu.Unlock()
		return nil, nil, brokerDeny("expired delegation handle")
	}
	// F6: a handle is bound to the module that minted it (entry.module). A
	// handle minted for one module must not resolve under another module's
	// reverse handler. entry.module is "" for minters not yet wired through
	// withDelegationModule; those stay unbound (not rejected) for back-compat.
	if entry.module != "" && entry.module != view.Name {
		return nil, nil, brokerDeny("delegation handle bound to module %q, not %q", entry.module, view.Name)
	}
	// Re-attach the originating request so crud.Redispatch re-injects the
	// caller's Cookie/Authorization and the full middleware chain re-resolves
	// the identity (design §5 caveat b).
	snap := snapshotRequest(entry)
	rctx := mcp.WithRequest(ctx, snap)
	// F3: stamp the re-dispatch as brokered so crossOwnerReadGranted refuses
	// to lift owner scoping no matter what the re-resolved caller holds.
	return crud.WithBrokeredCall(rctx), &entry, nil
}

// dispatchEntity performs the op-shaped CRUD re-dispatch through the router
// and maps the JSON envelope to the moduleproto result type.
func (b *Broker) dispatchEntity(op entityOp, ent *entity.Entity, ctx context.Context, params json.RawMessage) (any, error) {
	if b.router == nil {
		return nil, brokerDeny("no CRUD router configured")
	}
	base := b.entityPath(ent)
	switch op {
	case opQuery:
		var p moduleproto.EntityQueryParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		// F2: a child cannot smuggle control keys (include/trashed/where/…)
		// or undeclared names through p.Filter into the CRUD query. Drop
		// every key that is not a declared, non-Hidden, non-NoQuery field.
		filtered, err := sanitizeFilter(ent, p.Filter)
		if err != nil {
			return nil, paramErr("filter: %v", err)
		}
		p.Filter = filtered
		path := base + entityQuerySuffix(p)
		res, err := crud.Redispatch(ctx, b.router, http.MethodGet, path, nil)
		if err != nil {
			return nil, mapRedispatchErr(err)
		}
		return mapQueryResult(res)
	case opCreate:
		var p moduleproto.EntityMutationParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		res, err := crud.Redispatch(ctx, b.router, http.MethodPost, base, rawBody(p.Payload))
		if err != nil {
			return nil, mapRedispatchErr(err)
		}
		return mapMutationResult(res), nil
	case opUpdate:
		var p moduleproto.EntityMutationParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		id, err := singleID(p.IDs)
		if err != nil {
			return nil, err
		}
		path := base + "/" + url.PathEscape(id)
		res, err := crud.Redispatch(ctx, b.router, http.MethodPatch, path, rawBody(p.Payload))
		if err != nil {
			return nil, mapRedispatchErr(err)
		}
		return mapMutationResult(res), nil
	case opDelete:
		var p moduleproto.EntityMutationParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		id, err := singleID(p.IDs)
		if err != nil {
			return nil, err
		}
		path := base + "/" + url.PathEscape(id)
		if _, err := crud.Redispatch(ctx, b.router, http.MethodDelete, path, nil); err != nil {
			return nil, mapRedispatchErr(err)
		}
		return moduleproto.EntityMutationResult{Affected: 1}, nil
	}
	return nil, brokerDeny("unknown entity op")
}

// ----- search -----

// searchHandler enforces the search:query module-grant pre-filter then
// re-dispatches through the configured search endpoint. No global search
// surface in v1 → a nil endpoint denies after the perm gate (honest denial).
// v1 applies only the module-grant half; per-caller search authority is not
// modeled (search results are not owner-scoped in the framework today).
func (b *Broker) searchHandler(view ModuleGrantView) moduleproto.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.SearchQueryParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		if !access.ScopeMatch(view.Grants, access.Permission("search:query")) {
			return nil, brokerDeny("module grant does not satisfy %q", "search:query")
		}
		for _, g := range view.Grants {
			if reason := nonGrantableReason(g); reason != "" {
				return nil, brokerDeny("non-grantable scope in view (%s)", reason)
			}
		}
		if b.searchEndpoint == nil {
			return nil, brokerDeny("no search surface configured")
		}
		q := url.Values{}
		q.Set("q", p.Query)
		if p.Limit > 0 {
			q.Set("limit", fmt.Sprint(p.Limit))
		}
		res, err := crud.Redispatch(ctx, b.searchEndpoint, http.MethodGet, "/search?"+q.Encode(), nil)
		if err != nil {
			return nil, mapRedispatchErr(err)
		}
		return mapSearchResult(res), nil
	}
}

// ----- events -----

// eventHandler enforces the <topic>:emit module-grant pre-filter then emits on
// the host event bus. v1 applies only the module-grant half (events are not
// owner-scoped). A nil event bus denies.
func (b *Broker) eventHandler(view ModuleGrantView) moduleproto.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.EventEmitParams
		if err := unmarshalParams(params, &p); err != nil {
			return nil, err
		}
		required := access.Permission(p.Topic + ":emit")
		if !access.ScopeMatch(view.Grants, required) {
			return nil, brokerDeny("module grant does not satisfy %q", required)
		}
		for _, g := range view.Grants {
			if reason := nonGrantableReason(g); reason != "" {
				return nil, brokerDeny("non-grantable scope in view (%s)", reason)
			}
		}
		if b.events == nil {
			return nil, brokerDeny("no event bus configured")
		}
		var data any
		if len(p.Payload) > 0 {
			if err := json.Unmarshal(p.Payload, &data); err != nil {
				return nil, paramErr("event payload: %v", err)
			}
		}
		if err := b.events.Emit(ctx, event.Event{Type: p.Topic, Data: data}); err != nil {
			return nil, brokerDeny("event emit failed: %v", err)
		}
		return moduleproto.EventEmitResult{}, nil
	}
}

// ----- helpers -----

// lookupEntity resolves an entity name through the host registry. Returns nil
// for an unknown name or a nil registry, both deny (fail-closed).
func (b *Broker) lookupEntity(name string) *entity.Entity {
	if b.entities == nil || name == "" {
		return nil
	}
	ent, err := b.entities.Get(name)
	if err != nil {
		return nil
	}
	return ent
}

// entityPath builds the CRUD re-dispatch path for ent: <apiPrefix>/<table>.
// Mirrors CrudHandler.mcpBase so the re-dispatch hits the same routes REST
// traffic does.
func (b *Broker) entityPath(ent *entity.Entity) string {
	if b.apiPrefix == "" {
		return "/" + ent.GetTable()
	}
	return "/" + b.apiPrefix + "/" + ent.GetTable()
}

// moduleRole is the synthetic ambient role name for a module (design §5).
func moduleRole(name string) string { return "module/" + name }

// snapshotRequest builds a minimal *http.Request carrying the stashed caller
// credentials so crud.Redispatch's mcp.WithRequest header re-injection
// recovers the same identity the middleware would resolve for a live request.
func snapshotRequest(entry delegationEntry) *http.Request {
	r := &http.Request{Header: map[string][]string{}, URL: &url.URL{Path: "/"}}
	if entry.cookie != "" {
		r.Header.Set("Cookie", entry.cookie)
	}
	if entry.authorization != "" {
		r.Header.Set("Authorization", entry.authorization)
	}
	return r
}

// entityQuerySuffix expands the structured query params into the CRUD list
// query string the filter/sort/select/limit/offset DSL expects. p.Filter MUST
// be allow-listed by the caller (dispatchEntity runs it through sanitizeFilter)
// before reaching here, expandRaw forwards every key verbatim, so an
// unfiltered filter would let a child inject control keys (include/trashed/…).
func entityQuerySuffix(p moduleproto.EntityQueryParams) string {
	q := url.Values{}
	expandRaw(q, p.Filter)
	if len(p.Sort) > 0 {
		q.Set("sort", sortParam(p.Sort))
	}
	if len(p.Select) > 0 {
		if sel := selectParam(p.Select); sel != "" {
			q.Set("fields", sel)
		}
	}
	if p.Limit > 0 {
		q.Set("limit", fmt.Sprint(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", fmt.Sprint(p.Offset))
	}
	enc := q.Encode()
	if enc == "" {
		return ""
	}
	return "?" + enc
}

// expandRaw sets one query param per top-level key of a JSON object filter.
// Non-object filters are ignored (the host's filter DSL is a flat map). It is
// an UNFILTERED primitive, every key is forwarded verbatim, so the caller
// MUST allow-list first (see sanitizeFilter, which mirrors the crud/mcp.go
// listTool field allow-list). Do NOT call expandRaw on an untrusted filter.
func expandRaw(q url.Values, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	for k, v := range m {
		q.Set(k, fmt.Sprint(v))
	}
}

// sanitizeFilter reduces a child-supplied filter object to the keys that name
// a declared, non-Hidden, non-NoQuery field of ent, optionally suffixed with
// a comparison operator the filter DSL recognizes (_gt/_gte/_lt/_lte/_like/_in).
// Control keys (include/trashed/where/limit/sort/fields/q/…), Hidden/NoQuery
// field names, and undeclared names are dropped so they can never reach the
// CRUD list query string. This is the F2 fix: it mirrors the allow-list
// crud/mcp.go listTool builds, which expandRaw alone does not. Non-object
// filters pass through unchanged (expandRaw ignores them anyway).
func sanitizeFilter(ent *entity.Entity, raw json.RawMessage) (json.RawMessage, error) {
	if ent == nil || len(raw) == 0 {
		return raw, nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw, nil // non-object: expandRaw ignores it; pass through
	}
	allowed := queryableFieldKeys(ent)
	out := make(map[string]any, len(m))
	for k, v := range m {
		if allowed[k] {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// queryableFieldKeys returns the set of filter keys the CRUD list parser
// accepts for ent's declared, non-Hidden, non-NoQuery fields: each field's
// column Name and WireName alias, each with the optional comparison suffixes
// the filter DSL recognizes. Mirrors crud/mcp.go listTool's mcpFieldKey loop.
func queryableFieldKeys(ent *entity.Entity) map[string]bool {
	allowed := make(map[string]bool)
	if ent == nil {
		return allowed
	}
	for _, f := range ent.GetFields() {
		if f.Hidden || f.NoQuery {
			continue
		}
		for _, k := range []string{f.Name, f.WireName} {
			if k == "" {
				continue
			}
			for _, suffix := range []string{"", "_gt", "_gte", "_lt", "_lte", "_like", "_in"} {
				allowed[k+suffix] = true
			}
		}
	}
	return allowed
}

func sortParam(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil {
		return strings.Join(ss, ",")
	}
	return ""
}

func selectParam(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil {
		return strings.Join(ss, ",")
	}
	return ""
}

func singleID(ids []string) (string, error) {
	if len(ids) == 0 || ids[0] == "" {
		return "", brokerDeny("mutation requires a non-empty id")
	}
	return ids[0], nil
}

func rawBody(p json.RawMessage) any {
	if len(p) == 0 {
		return nil
	}
	return p
}

func unmarshalParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return paramErr("empty params")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return paramErr("malformed params: %v", err)
	}
	return nil
}

func mapQueryResult(res any) (any, error) {
	env, ok := res.(map[string]any)
	if !ok {
		// Non-envelope (rare): return the raw value as rows.
		raw, _ := json.Marshal(res)
		return moduleproto.EntityQueryResult{Rows: raw}, nil
	}
	rows, _ := json.Marshal(env["data"])
	total := 0
	if t, ok := env["total"].(float64); ok {
		total = int(t)
	}
	return moduleproto.EntityQueryResult{Rows: rows, Total: total}, nil
}

func mapMutationResult(res any) moduleproto.EntityMutationResult {
	if env, ok := res.(map[string]any); ok {
		raw, _ := json.Marshal(env["data"])
		return moduleproto.EntityMutationResult{Affected: 1, Rows: raw}
	}
	if res == nil {
		return moduleproto.EntityMutationResult{Affected: 1}
	}
	raw, _ := json.Marshal(res)
	return moduleproto.EntityMutationResult{Affected: 1, Rows: raw}
}

func mapSearchResult(res any) any {
	if env, ok := res.(map[string]any); ok {
		raw, _ := json.Marshal(env["data"])
		if raw == nil {
			raw, _ = json.Marshal(env["results"])
		}
		total := 0
		if t, ok := env["total"].(float64); ok {
			total = int(t)
		}
		return moduleproto.SearchQueryResult{Results: raw, Total: total}
	}
	raw, _ := json.Marshal(res)
	return moduleproto.SearchQueryResult{Results: raw}
}

// mapRedispatchErr converts a CRUD chokepoint failure into a wire error. A
// 401/403 from the re-dispatch is the caller-authority half failing, surface
// it as a capability denial so the adversarial property ("caller lacks the
// perm → 403 from the re-dispatch") is observable as a denial.
func mapRedispatchErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") {
		return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "caller authority denied by CRUD chokepoint: " + msg}
	}
	return &moduleproto.Error{Code: moduleproto.CodeInternalError, Message: "crud redispatch failed: " + msg}
}

// brokerDeny returns a [*moduleproto.Error] capability denial. It is the single
// shape every gate denial takes so the child sees a consistent, diagnosable
// CodeCapabilityDenied rather than a generic internal error.
func brokerDeny(format string, args ...any) error {
	return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "moduleproto broker: " + fmt.Sprintf(format, args...)}
}

func paramErr(format string, args ...any) error {
	return &moduleproto.Error{Code: moduleproto.CodeInvalidParams, Message: "moduleproto broker: " + fmt.Sprintf(format, args...)}
}
