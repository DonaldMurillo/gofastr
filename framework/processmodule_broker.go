package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// Broker is the capability broker for the module reverse channel. It
// implements [ReverseBroker]: a reverse call needs both the module's grant
// and, re-checked by the CRUD routes, the originating caller's authority.
type Broker struct {
	router         http.Handler
	entities       entity.Registry
	events         *event.EventBus
	apiPrefix      string
	policy         *access.RolePolicy
	searchEndpoint http.Handler

	mu      sync.Mutex
	handles map[string]delegationEntry

	handleTTL time.Duration
	now       func() time.Time
}

type delegationEntry struct {
	credentials  http.Header
	roles        []string
	policy       *access.RolePolicy
	module       string
	parentCallID uint64
	expires      time.Time
}

// BrokerOption configures a [Broker].
type BrokerOption func(*Broker)

// WithBrokerPolicy installs the app-wide RolePolicy that resolves an ambient
// reverse call's synthetic `module/<name>` role.
func WithBrokerPolicy(p *access.RolePolicy) BrokerOption {
	return func(b *Broker) { b.policy = p }
}

// WithBrokerSearchEndpoint wires the host.search.query target; without it
// search denies.
func WithBrokerSearchEndpoint(h http.Handler) BrokerOption {
	return func(b *Broker) { b.searchEndpoint = h }
}

// WithBrokerHandleTTL bounds the lifetime of an unreleased handle. Default
// 5m; d <= 0 keeps the default.
func WithBrokerHandleTTL(d time.Duration) BrokerOption {
	return func(b *Broker) {
		if d > 0 {
			b.handleTTL = d
		}
	}
}

// WithBrokerClock injects the clock the handle-expiry check reads.
func WithBrokerClock(now func() time.Time) BrokerOption {
	return func(b *Broker) { b.now = now }
}

// NewBroker constructs a capability broker. A nil router, entities or events
// makes that reverse surface deny. apiPrefix ("" or "/api") prefixes entity
// re-dispatch paths.
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

var _ ReverseBroker = (*Broker)(nil)

// InstallHandlers registers the six host.* reverse handlers on p, each
// scoped to view.
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
		panic(fmt.Sprintf("processmodule: broker install %q: %v", method, err))
	}
}

// MintDelegation stashes the request's credentials under a random opaque
// handle and returns it with a release func the caller must invoke when the
// parent call returns. A nil r mints an ambient entry.
func (b *Broker) MintDelegation(r *http.Request, parentCallID uint64) (string, func()) {
	handle, err := randomHandle()
	if err != nil {
		panic(fmt.Sprintf("processmodule broker: mint delegation handle: %v", err))
	}
	entry := delegationEntry{parentCallID: parentCallID, expires: b.now().Add(b.handleTTL)}
	if r != nil {
		entry.credentials = credentialSnapshot(r)
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

func randomHandle() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("read delegation entropy: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

type delegationModuleKey struct{}

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

type entityOp int

const (
	opQuery entityOp = iota
	opCreate
	opUpdate
	opDelete
)

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

func (b *Broker) gate(ctx context.Context, entityName, verb string, view ModuleGrantView, caller moduleproto.CallerRef) (*entity.Entity, context.Context, error) {
	ent := b.lookupEntity(entityName)
	if ent == nil {
		return nil, nil, brokerDeny("entity %q is not a host resource", entityName)
	}

	required := access.Permission(entityName + ":" + verb)
	if !access.ScopeMatch(view.Grants, required) {
		return nil, nil, brokerDeny("module grant does not satisfy %q", required)
	}

	for _, g := range view.Grants {
		if reason := nonGrantableReason(g); reason != "" {
			return nil, nil, brokerDeny("non-grantable scope in view (%s)", reason)
		}
	}

	rctx, entry, err := b.resolveCaller(ctx, caller, view)
	if err != nil {
		return nil, nil, err
	}

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

func (b *Broker) resolveCaller(ctx context.Context, caller moduleproto.CallerRef, view ModuleGrantView) (context.Context, *delegationEntry, error) {
	if caller.Delegation == "" {
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
	if entry.module != "" && entry.module != view.Name {
		return nil, nil, brokerDeny("delegation handle bound to module %q, not %q", entry.module, view.Name)
	}
	snap := snapshotRequest(entry)
	rctx := mcp.WithRequest(ctx, snap)
	return crud.WithBrokeredCall(rctx), &entry, nil
}

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
		return mapMutationResult(res)
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
		return mapMutationResult(res)
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
		return mapSearchResult(res)
	}
}

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
			if err := jsonexact.Unmarshal(p.Payload, &data); err != nil {
				return nil, paramErr("event payload: %v", err)
			}
		}
		if err := b.events.Emit(ctx, event.Event{Type: p.Topic, Data: data}); err != nil {
			return nil, brokerDeny("event emit failed: %v", err)
		}
		return moduleproto.EventEmitResult{}, nil
	}
}

func (b *Broker) lookupEntity(name string) (ent *entity.Entity) {
	if b.entities == nil || name == "" {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("processmodule: entity registry Get panicked; denying lookup (fail-closed)",
				"entity", name, "panic", textsafe.Recovered(r), "stack", string(debug.Stack()))
		}
	}()
	ent, err := b.entities.Get(name)
	if err != nil {
		return nil
	}
	return ent
}

func (b *Broker) entityPath(ent *entity.Entity) string {
	if b.apiPrefix == "" {
		return "/" + ent.GetTable()
	}
	return "/" + b.apiPrefix + "/" + ent.GetTable()
}

func moduleRole(name string) string { return "module/" + name }

var delegationCredentialHeaders = []string{"Authorization", "X-API-Key", fembed.GrantHeader, "Cookie"}

func credentialSnapshot(r *http.Request) http.Header {
	h := http.Header{}
	for _, name := range delegationCredentialHeaders {
		for _, v := range r.Header.Values(name) {
			if v == "" {
				continue
			}
			//gofastr:allow(controlbytes) the snapshot re-presents the caller's own credential headers on the internal re-dispatch request (snapshotRequest); the client transport validates and rejects control bytes at write time, the outbound posture this rule already grants a request-typed map
			h.Add(name, v)
		}
	}
	return h
}

func snapshotRequest(entry delegationEntry) *http.Request {
	header := entry.credentials.Clone()
	if header == nil {
		header = http.Header{}
	}
	return &http.Request{Header: header, URL: &url.URL{Path: "/"}}
}

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

func expandRaw(q url.Values, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var m map[string]any
	if jsonexact.Unmarshal(raw, &m) != nil {
		return
	}
	for k, v := range m {
		q.Set(k, filterParamString(v))
	}
}

func filterParamString(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func sanitizeFilter(ent *entity.Entity, raw json.RawMessage) (json.RawMessage, error) {
	if ent == nil || len(raw) == 0 {
		return raw, nil
	}
	var m map[string]any
	if jsonexact.Unmarshal(raw, &m) != nil {
		return raw, nil
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
			allowed[k] = true
			for _, s := range filter.FilterSuffixes {
				allowed[k+s.Suffix] = true
			}
			for _, op := range filter.BracketOps {
				allowed[filter.BracketKey(k, op)] = true
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
		rows, err := json.Marshal(res)
		if err != nil {
			return nil, fmt.Errorf("encode query rows: %w", err)
		}
		return moduleproto.EntityQueryResult{Rows: rows}, nil
	}
	rows, err := json.Marshal(env["data"])
	if err != nil {
		return nil, fmt.Errorf("encode query rows: %w", err)
	}
	total := 0
	if raw, present := env["total"]; present {
		t, ok := envelopeTotal(raw)
		if !ok {
			return nil, fmt.Errorf("query result envelope: %q must be an integer, got %T %v", "total", raw, raw)
		}
		total = t
	}
	return moduleproto.EntityQueryResult{Rows: rows, Total: total}, nil
}

func envelopeTotal(raw any) (int, bool) {
	n, ok := raw.(json.Number)
	if !ok {
		return 0, false
	}
	t, err := n.Int64()
	if err != nil || t < 0 || t > math.MaxInt {
		return 0, false
	}
	return int(t), true
}

func mapMutationResult(res any) (moduleproto.EntityMutationResult, error) {
	if env, ok := res.(map[string]any); ok {
		raw, err := json.Marshal(env["data"])
		if err != nil {
			return moduleproto.EntityMutationResult{}, fmt.Errorf("encode mutation rows: %w", err)
		}
		return moduleproto.EntityMutationResult{Affected: 1, Rows: raw}, nil
	}
	if res == nil {
		return moduleproto.EntityMutationResult{Affected: 1}, nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return moduleproto.EntityMutationResult{}, fmt.Errorf("encode mutation rows: %w", err)
	}
	return moduleproto.EntityMutationResult{Affected: 1, Rows: raw}, nil
}

func mapSearchResult(res any) (any, error) {
	if env, ok := res.(map[string]any); ok {
		raw, err := json.Marshal(env["data"])
		if err != nil {
			return nil, fmt.Errorf("encode search results: %w", err)
		}
		total, _ := envelopeTotal(env["total"])
		return moduleproto.SearchQueryResult{Results: raw, Total: total}, nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("encode search results: %w", err)
	}
	return moduleproto.SearchQueryResult{Results: raw}, nil
}

func mapRedispatchErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") {
		return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "caller authority denied by CRUD chokepoint: " + msg}
	}
	return &moduleproto.Error{Code: moduleproto.CodeInternalError, Message: "crud redispatch failed: " + msg}
}

func brokerDeny(format string, args ...any) error {
	return &moduleproto.Error{Code: moduleproto.CodeCapabilityDenied, Message: "moduleproto broker: " + fmt.Sprintf(format, args...)}
}

func paramErr(format string, args ...any) error {
	return &moduleproto.Error{Code: moduleproto.CodeInvalidParams, Message: "moduleproto broker: " + fmt.Sprintf(format, args...)}
}
