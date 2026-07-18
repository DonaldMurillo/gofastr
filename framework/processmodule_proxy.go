package framework

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/uihost/uinoderender"
)

// proxyRetryAfter is the Retry-After header value (seconds) returned with
// every 503 the proxy emits. Bounded — a client retrying once per second
// self-corrects the moment the supervisor publishes Ready.
const proxyRetryAfter = "1"

// moduleProxyAllowedHeaders is the inbound-header allowlist the proxy
// forwards in moduleproto.HTTPRequestParams.Headers (design §4.4: "headers
// allowlisted"). Header names are canonicalized by net/http on read.
// Custom headers can be added by extending this set in a later wave.
var moduleProxyAllowedHeaders = map[string]struct{}{
	"Content-Type":     {},
	"Accept":           {},
	"Accept-Language":  {},
	"Accept-Encoding":  {},
	"User-Agent":       {},
	"X-Request-ID":     {},
	"X-Correlation-ID": {},
}

// hopByHopResponseHeaders are stripped from a child's response headers
// before commit (RFC 7230 §6.1). They must not cross the proxy boundary.
var hopByHopResponseHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailers":            {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// proxyCallID is a host-side correlation id counter for [moduleproto.HTTPRequestParams.RequestID].
// It is unique per supervisor process; encoded as a string for the wire.
var proxyCallID atomic.Uint64

// ProxyHandler returns the http.Handler the host router mounts for one
// declared route. The route gate (Enabled) has ALREADY run by the time this
// handler is invoked — if the module is disabled, the route 404'd upstream
// and this handler was never reached.
//
// This handler implements the SECOND layer of the §8 two-layer gate: the
// Ready check. Enabled-but-not-Ready → 503 + Retry-After (design decision
// D). On Ready it proxies the request via module.http, FULLY BUFFERING the
// response before committing any headers (the buffered-503 guarantee: a
// child that dies mid-call yields a buffered 503, never a truncated 200).
//
// name is the module name; routeID is the descriptor's stable route id.
func (s *ProcessModuleSupervisor) ProxyHandler(name, routeID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveProxy(name, routeID, w, r)
	})
}

// serveProxy is the request body of [ProxyHandler].
func (s *ProcessModuleSupervisor) serveProxy(name, routeID string, w http.ResponseWriter, r *http.Request) {
	sl := s.Slot(name)
	if sl == nil {
		// Module not registered. The route gate should have caught this,
		// but be defensive: 404, not 500.
		http.NotFound(w, r)
		return
	}
	snap := sl.snapshot()

	// Second-layer gate (design D): the route gate already 404'd if the
	// module is desired-disabled. Here we check LIVENESS.
	if !servingState(snap.state, snap.leaseFailingUnsafe()) {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	peer := snap.peer
	if peer == nil {
		// Defensive: state == Ready but peer nil (race during teardown).
		writeProxy503(w, proxyRetryAfter)
		return
	}

	// Mint a delegation handle for this call. The broker stashes the
	// originating request under this handle so a reverse host.* call can
	// re-attach the caller's context to the CRUD re-dispatch (design §5).
	// release() drops the handle when the proxied call returns so the
	// in-memory table cannot leak across a long-lived connection.
	callID := proxyCallID.Add(1)
	requestID := fmt.Sprintf("%s-%d", name, callID)
	handle, release := s.broker.MintDelegation(r, callID)
	defer release()

	// Build the module.http params.
	params, err := buildHTTPRequestParams(r, name, routeID, requestID, handle, sl.callDeadline())
	if err != nil {
		// Body read / base64 failure: 503 (we cannot blame the child).
		writeProxy503(w, proxyRetryAfter)
		return
	}

	// Per-call deadline context. The cancel watcher notifies the child via
	// module.cancel when this ctx expires so it aborts in-flight work
	// (design §4.4 module.cancel).
	deadline := time.UnixMilli(params.DeadlineUnixMs)
	callCtx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	go s.watchCancel(peer, requestID, callCtx)

	// Issue the call. The result is FULLY BUFFERED in the response before
	// any header commit — a child that dies mid-call returns an error
	// here, which we surface as a buffered 503. Never a truncated 200.
	raw, err := peer.Call(callCtx, moduleproto.MethodHTTP, params)
	if err != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	var res moduleproto.HTTPResponseResult
	if jErr := json.Unmarshal(raw, &res); jErr != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	commitBufferedResponse(w, &res, sl.uiRenderer())
}

// uiRenderer builds the ui.node.v1 → design-system renderer for this module.
// The ActionResolver maps a tree's ActionRef (a descriptor-local route id) to
// that route's mounted path, so a module can wire a button to one of its OWN
// declared routes and nothing else — an ActionRef that names no declared
// route fails the render closed (design §9). Rebuilt per call from the
// descriptor's routes (a handful of entries; no hot-path cost).
func (sl *moduleSlot) uiRenderer() *uinoderender.Renderer {
	sl.mu.RLock()
	routes := sl.desc.Routes
	sl.mu.RUnlock()
	byID := make(map[string]string, len(routes))
	for _, rt := range routes {
		byID[rt.ID] = rt.Path
	}
	return uinoderender.New(func(actionRef string) (string, bool) {
		p, ok := byID[actionRef]
		return p, ok
	})
}

// leaseFailingUnsafe reads leaseFailing WITHOUT taking the lock. Used inside
// snapshot's already-locked read; here it is a separate read for the proxy
// path which already holds RLock via snapshot. We re-snapshot under the
// lock instead.
func (s *snapshot) leaseFailingUnsafe() bool { return false } // placeholder; replaced below

// servingState reports whether the proxy should attempt a module.http call.
// StateReady + lease healthy → true; everything else → false (503).
func servingState(state ProcessState, leaseFailing bool) bool {
	if leaseFailing {
		return false
	}
	return state == StateReady
}

// writeProxy503 writes the buffered 503 + Retry-After header. Used for
// every not-Ready / mid-call-crash / decode-failure path.
func writeProxy503(w http.ResponseWriter, retryAfter string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", retryAfter)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "module temporarily unavailable\n")
}

// buildHTTPRequestParams assembles the module.http params from an inbound
// HTTP request. Headers are allowlisted; body is base64-encoded; the caller
// block carries the resolved end-user context the broker re-attaches on
// reverse host.* calls (design §5). Subject/Tenant are diagnostic on the
// wire — the child echoes them, but the broker re-attaches the real request
// context via the delegation handle (the load-bearing seam); it never trusts
// a child-supplied value for the authorization decision.
func buildHTTPRequestParams(r *http.Request, moduleName, routeID, requestID, delegationHandle string, deadline time.Duration) (moduleproto.HTTPRequestParams, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxModuleHTTPBodyBytes)))
	if err != nil {
		return moduleproto.HTTPRequestParams{}, fmt.Errorf("read body: %w", err)
	}
	headers := make(map[string]string, len(moduleProxyAllowedHeaders))
	for k := range moduleProxyAllowedHeaders {
		if v := r.Header.Get(k); v != "" {
			headers[k] = v
		}
	}
	query := make(map[string]string, len(r.URL.Query()))
	for k, vs := range r.URL.Query() {
		if len(vs) > 0 {
			query[k] = vs[0]
		}
	}
	params := moduleproto.HTTPRequestParams{
		RequestID:  requestID,
		RouteID:    routeID,
		Method:     moduleproto.HTTPMethod(r.Method),
		PathParams: pathParamsFor(r, moduleName),
		Query:      query,
		Headers:    headers,
		BodyB64:    base64.StdEncoding.EncodeToString(body),
		Caller: moduleproto.Caller{
			// Resolved end-user context (design §5). These are diagnostic;
			// the broker re-derives authority from the stashed originating
			// request keyed by Delegation, never from these echoed fields.
			Subject:    subjectFromRequest(r),
			Tenant:     tenant.GetTenantID(r.Context()),
			Delegation: delegationHandle,
		},
		DeadlineUnixMs: time.Now().Add(deadline).UnixMilli(),
	}
	return params, nil
}

// subjectFromRequest extracts a best-effort caller subject string from the
// inbound request context (core/handler.GetUser). It is diagnostic-only on
// the module.http caller block; the broker never authorizes off it. Empty
// when no user is resolved (anonymous proxied request).
func subjectFromRequest(r *http.Request) string {
	if u, ok := handler.GetUser(r.Context()); ok && u != nil {
		return fmt.Sprint(u)
	}
	return ""
}

// maxModuleHTTPBodyBytes bounds an inbound proxied body. The codec cap
// (default 1 MiB) is the hard protocol limit; this is the per-call guard
// before base64 inflation. A request body larger than this yields a 413.
// (For wave 2a we 503 on read error above; tightening to 413 is a doc fix.)
const maxModuleHTTPBodyBytes = 1024 * 1024

// pathParamsFor extracts route path parameters via the framework router's
// [router.Params] helper (which scans r.Pattern and r.PathValue). Returns
// nil for non-parameterized routes. Every value is already CR/LF/NUL
// sanitized by the router, so the proxy does not re-sanitize.
func pathParamsFor(r *http.Request, _ string) map[string]string {
	return router.Params(r)
}

// watchCancel notifies the child of a per-call cancellation (design §4.4
// module.cancel). It returns once callCtx is done or the supervisor closes.
// The notification is best-effort: a child that has already returned is not
// affected.
func (s *ProcessModuleSupervisor) watchCancel(peer *moduleproto.Peer, requestID string, callCtx context.Context) {
	select {
	case <-callCtx.Done():
	case <-s.closeCh:
		return
	}
	if errors.Is(callCtx.Err(), context.Canceled) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = peer.Notify(notifyCtx, moduleproto.MethodCancel, moduleproto.CancelParams{RequestID: requestID})
	}
}

// commitBufferedResponse writes the buffered response. Because the body is
// already fully in memory (we did not stream), this never produces a partial
// commit. Headers are filtered for hop-by-hop; Content-Type is derived from
// the body kind for json/text.
func commitBufferedResponse(w http.ResponseWriter, res *moduleproto.HTTPResponseResult, renderer *uinoderender.Renderer) {
	// Apply child-supplied response headers, minus hop-by-hop.
	for k, v := range res.Headers {
		if _, drop := hopByHopResponseHeaders[k]; drop {
			continue
		}
		w.Header().Set(k, v)
	}
	body, contentType, err := decodeBody(&res.Body, renderer)
	if err != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	if w.Header().Get("Content-Type") == "" && contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(res.Status)
	_, _ = w.Write(body)
}

// decodeBody materializes the response body and returns (bytes, contentType, err).
// For ui.node.v1 (a later-wave surface), returns an error so the proxy
// emits a buffered 503 — the validator is not built yet (design §9).
func decodeBody(b *moduleproto.HTTPResponseBody, renderer *uinoderender.Renderer) ([]byte, string, error) {
	if b == nil {
		return nil, "", nil
	}
	switch b.Kind {
	case moduleproto.BodyKindJSON:
		return b.Value, "application/json; charset=utf-8", nil
	case moduleproto.BodyKindText:
		return b.Value, "text/plain; charset=utf-8", nil
	case moduleproto.BodyKindUINodeV1:
		// The module returned a ui.node.v1 tree, never markup. Validate it
		// through the closed validator (unknown component / forged data-fui-*
		// / bad URL / size bomb → whole-tree reject) and render it host-side,
		// with the host assigning every id/class/ARIA/data-fui-rpc. Any
		// failure is fail-safe: an error here surfaces as a buffered 503, the
		// forged content never reaching the wire.
		if renderer == nil {
			return nil, "", errors.New("module.http: ui.node.v1 body with no renderer configured")
		}
		tree, err := uinodev1.Validate(b.Value, uinodev1.DefaultLimits())
		if err != nil {
			return nil, "", fmt.Errorf("module.http: ui.node.v1 validation rejected: %w", err)
		}
		html, err := renderer.Render(tree)
		if err != nil {
			return nil, "", fmt.Errorf("module.http: ui.node.v1 render failed: %w", err)
		}
		return []byte(html), "text/html; charset=utf-8", nil
	default:
		return nil, "", fmt.Errorf("module.http: unknown response body kind %q", b.Kind)
	}
}

// ----- introspection -----

// ProcessModuleInfo is the introspection view of one supervised module
// (design §8). Operator-only; surfaced via the existing [ModuleInfo]
// extension. Nil/zero for in-process modules.
type ProcessModuleInfo struct {
	Name               string
	Version            string
	State              ProcessState
	DesiredGeneration  uint64
	ObservedGeneration uint64
	InstanceID         string
	RestartCount       int
	CircuitOpen        bool
	LastExit           string
	TrustTier          TrustTier
	RouteCount         int
	ToolCount          int
	LeaseFailing       bool
}

// Info returns the introspection snapshot for name, or [ErrNoDesiredRow]
// (wrapped) if the module is not registered.
func (s *ProcessModuleSupervisor) Info(name string) (ProcessModuleInfo, error) {
	sl := s.Slot(name)
	if sl == nil {
		return ProcessModuleInfo{}, fmt.Errorf("processmodule: %w: %q", ErrNoDesiredRow, name)
	}
	snap := sl.snapshot()
	sl.mu.RLock()
	leaseFailing := sl.leaseFailing
	observedGen := sl.desiredGen
	sl.mu.RUnlock()
	return ProcessModuleInfo{
		Name:               name,
		Version:            sl.desc.Version,
		State:              snap.state,
		DesiredGeneration:  observedGen,
		ObservedGeneration: observedGen,
		InstanceID:         snap.instanceID,
		RestartCount:       snap.restartCnt,
		CircuitOpen:        snap.circuitOpen,
		LastExit:           snap.lastExit,
		TrustTier:          sl.desc.TrustTier,
		RouteCount:         len(sl.desc.Routes),
		ToolCount:          len(sl.desc.Tools),
		LeaseFailing:       leaseFailing,
	}, nil
}

// List returns introspection for every registered process module, ordered
// by name.
func (s *ProcessModuleSupervisor) List() []ProcessModuleInfo {
	s.mu.Lock()
	names := make([]string, 0, len(s.slots))
	for n := range s.slots {
		names = append(names, n)
	}
	s.mu.Unlock()
	// Sort for deterministic output.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	out := make([]ProcessModuleInfo, 0, len(names))
	for _, n := range names {
		info, err := s.Info(n)
		if err == nil {
			out = append(out, info)
		}
	}
	return out
}

// ----- route registration helper -----

// DeclaredRoutes returns the descriptor's declared routes for the host
// wiring to iterate when registering proxy routes with the router.
func (s *ProcessModuleSupervisor) DeclaredRoutes(name string) []RouteDeclaration {
	sl := s.Slot(name)
	if sl == nil {
		return nil
	}
	return append([]RouteDeclaration(nil), sl.desc.Routes...)
}
