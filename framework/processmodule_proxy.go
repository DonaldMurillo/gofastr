package framework

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/framework/uihost/uinoderender"
)

const proxyRetryAfter = "1"

var moduleProxyAllowedHeaders = map[string]struct{}{
	"Content-Type":     {},
	"Accept":           {},
	"Accept-Language":  {},
	"Accept-Encoding":  {},
	"User-Agent":       {},
	"X-Request-ID":     {},
	"X-Correlation-ID": {},
}

var moduleResponseHeaders = map[string]struct{}{
	"Cache-Control":    {},
	"Content-Language": {},
	"Etag":             {},
	"Expires":          {},
	"Last-Modified":    {},
	"Retry-After":      {},
	"Vary":             {},
}

var proxyCallID atomic.Uint64

// ProxyHandler returns the handler the host mounts for one declared route
// of module name. It answers 503 with Retry-After while the module is not
// Ready, and otherwise proxies the request through module.http.
func (s *ProcessModuleSupervisor) ProxyHandler(name, routeID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveProxy(name, routeID, w, r)
	})
}

func (s *ProcessModuleSupervisor) serveProxy(name, routeID string, w http.ResponseWriter, r *http.Request) {
	sl := s.Slot(name)
	if sl == nil {
		http.NotFound(w, r)
		return
	}
	snap := sl.snapshot()

	if !servingState(snap.state, snap.leaseFailing) {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	peer := snap.peer
	if peer == nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}

	callID := proxyCallID.Add(1)
	requestID := fmt.Sprintf("%s-%d", name, callID)
	r = r.WithContext(withDelegationModule(r.Context(), name))
	handle, release := s.broker.MintDelegation(r, callID)
	defer release()

	params, err := buildHTTPRequestParams(r, name, routeID, requestID, handle, sl.callDeadline())
	if err == nil && !moduleHTTPFrameFits(params, sl.frameBytes()) {
		err = errModuleBodyTooLarge
	}
	if errors.Is(err, errModuleBodyTooLarge) {
		i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
		return
	}
	if err != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}

	deadline := time.UnixMilli(params.DeadlineUnixMs)
	callCtx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	raw, err := peer.CallWithID(callCtx, moduleproto.MethodHTTP, params, func(frameID uint64) {
		go s.watchCancel(peer, frameID, callCtx)
	})
	if err != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	var res moduleproto.HTTPResponseResult
	if jErr := json.Unmarshal(raw, &res); jErr != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	commitBufferedResponse(w, &res, sl.uiRenderer(r.Context()))
}

func (sl *moduleSlot) uiRenderer(ctx context.Context) *uinoderender.Renderer {
	sl.mu.RLock()
	routes := sl.desc.Routes
	sl.mu.RUnlock()
	byID := make(map[string]string, len(routes))
	for _, rt := range routes {
		byID[rt.ID] = rt.Path
	}
	return uinoderender.New(ctx, func(actionRef string) (string, bool) {
		p, ok := byID[actionRef]
		return p, ok
	})
}

func servingState(state ProcessState, leaseFailing bool) bool {
	if leaseFailing {
		return false
	}
	return state == StateReady
}

func writeProxy503(w http.ResponseWriter, retryAfter string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", retryAfter)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "module temporarily unavailable\n")
}

func buildHTTPRequestParams(r *http.Request, moduleName, routeID, requestID, delegationHandle string, deadline time.Duration) (moduleproto.HTTPRequestParams, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxModuleHTTPBodyBytes)+1))
	if err != nil {
		return moduleproto.HTTPRequestParams{}, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxModuleHTTPBodyBytes {
		return moduleproto.HTTPRequestParams{}, errModuleBodyTooLarge
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
			Subject:    subjectFromRequest(r),
			Tenant:     tenant.GetTenantID(r.Context()),
			Delegation: delegationHandle,
		},
		DeadlineUnixMs: time.Now().Add(deadline).UnixMilli(),
	}
	return params, nil
}

func subjectFromRequest(r *http.Request) string {
	if u, ok := handler.GetUser(r.Context()); ok && u != nil {
		return fmt.Sprint(u)
	}
	return ""
}

const maxModuleHTTPBodyBytes = 1024 * 1024

func moduleHTTPFrameFits(params moduleproto.HTTPRequestParams, frameBytes int) bool {
	raw, err := json.Marshal(params)
	if err != nil {
		return false
	}
	id := uint64(math.MaxUint64)
	body, err := json.Marshal(&moduleproto.Frame{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  moduleproto.MethodHTTP,
		Params:  raw,
	})
	if err != nil {
		return false
	}
	return len(body) <= frameBytes
}

var errModuleBodyTooLarge = errors.New("processmodule: request body over the proxy cap")

func pathParamsFor(r *http.Request, _ string) map[string]string {
	return router.Params(r)
}

func (s *ProcessModuleSupervisor) watchCancel(peer *moduleproto.Peer, frameID uint64, callCtx context.Context) {
	select {
	case <-callCtx.Done():
	case <-s.closeCh:
		return
	}
	if errors.Is(callCtx.Err(), context.Canceled) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = peer.Notify(notifyCtx, moduleproto.MethodCancel, moduleproto.CancelParams{
			RequestID: strconv.FormatUint(frameID, 10),
		})
	}
}

func commitBufferedResponse(w http.ResponseWriter, res *moduleproto.HTTPResponseResult, renderer *uinoderender.Renderer) {
	if res.Status < 200 || res.Status > 599 {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	body, contentType, err := decodeBody(&res.Body, renderer)
	if err != nil {
		writeProxy503(w, proxyRetryAfter)
		return
	}
	for _, k := range slices.Sorted(maps.Keys(res.Headers)) {
		name := textproto.CanonicalMIMEHeaderKey(k)
		if _, ok := moduleResponseHeaders[name]; !ok {
			continue
		}
		if name == "Vary" {
			w.Header().Add(name, res.Headers[k])
			continue
		}
		w.Header().Set(name, res.Headers[k])
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Del("Content-Type")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(res.Status)
	_, _ = w.Write(body)
}

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

// ProcessModuleInfo is the operator introspection view of one supervised
// module.
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

// DeclaredRoutes returns a copy of the descriptor's declared routes.
func (s *ProcessModuleSupervisor) DeclaredRoutes(name string) []RouteDeclaration {
	sl := s.Slot(name)
	if sl == nil {
		return nil
	}
	return append([]RouteDeclaration(nil), sl.desc.Routes...)
}
