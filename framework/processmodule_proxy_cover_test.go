package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// This file adds unit coverage for the PURE proxy helpers + introspection
// accessors in processmodule_proxy.go. None of these tests spawn a child;
// they exercise the helpers directly or against a supervisor with a
// registered descriptor (no spawn, Register installs the row + slot).

// ---- servingState ----

func TestServingState_onlyReadyHealthyPasses(t *testing.T) {
	cases := []struct {
		state        ProcessState
		leaseFailing bool
		want         bool
	}{
		{StateReady, false, true},
		{StateReady, true, false},
		{StateStarting, false, false},
		{StateHandshaking, false, false},
		{StateCrashed, false, false},
		{StateInstalledDisabled, false, false},
	}
	for _, c := range cases {
		if got := servingState(c.state, c.leaseFailing); got != c.want {
			t.Errorf("servingState(%s, leaseFailing=%v) = %v, want %v", c.state, c.leaseFailing, got, c.want)
		}
	}
}

// ---- writeProxy503 ----

func TestWriteProxy503_setsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProxy503(rec, proxyRetryAfter)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") != proxyRetryAfter {
		t.Errorf("Retry-After = %q, want %q", rec.Header().Get("Retry-After"), proxyRetryAfter)
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "unavailable") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// ---- decodeBody ----

func TestDecodeBody_nilReturnsEmpty(t *testing.T) {
	b, ct, err := decodeBody(nil, nil)
	if err != nil || ct != "" || len(b) != 0 {
		t.Fatalf("decodeBody(nil) = %q %q %v", string(b), ct, err)
	}
}

func TestDecodeBody_jsonAndText(t *testing.T) {
	b, ct, err := decodeBody(&moduleproto.HTTPResponseBody{
		Kind:  moduleproto.BodyKindJSON,
		Value: jsonRaw(`{"ok":true}`),
	}, nil)
	if err != nil || ct != "application/json; charset=utf-8" {
		t.Fatalf("json decode = %q %q %v", string(b), ct, err)
	}
	b, ct, err = decodeBody(&moduleproto.HTTPResponseBody{
		Kind:  moduleproto.BodyKindText,
		Value: jsonRaw("hello"),
	}, nil)
	if err != nil || ct != "text/plain; charset=utf-8" || string(b) != "hello" {
		t.Fatalf("text decode = %q %q %v", string(b), ct, err)
	}
}

func TestDecodeBody_uiNodeV1NoRendererErrors(t *testing.T) {
	_, _, err := decodeBody(&moduleproto.HTTPResponseBody{
		Kind:  moduleproto.BodyKindUINodeV1,
		Value: jsonRaw(`{}`),
	}, nil)
	if err == nil {
		t.Fatal("ui.node.v1 with nil renderer must error")
	}
}

func TestDecodeBody_unknownKindErrors(t *testing.T) {
	_, _, err := decodeBody(&moduleproto.HTTPResponseBody{
		Kind:  "bogus",
		Value: jsonRaw(`x`),
	}, nil)
	if err == nil {
		t.Fatal("unknown body kind must error")
	}
}

// ---- commitBufferedResponse ----

func TestCommitBufferedResponse_stripsHopByHop(t *testing.T) {
	rec := httptest.NewRecorder()
	res := &moduleproto.HTTPResponseResult{
		Status: 200,
		Headers: map[string]string{
			"Connection":   "keep-alive",
			"X-Custom":     "kept",
			"Content-Type": "application/json; charset=utf-8",
		},
		Body: moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindJSON, Value: jsonRaw(`{"a":1}`)},
	}
	commitBufferedResponse(rec, res, nil)
	if rec.Header().Get("Connection") != "" {
		t.Error("hop-by-hop Connection header was not stripped")
	}
	if rec.Header().Get("X-Custom") != "kept" {
		t.Error("non-hop-by-hop header was dropped")
	}
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestCommitBufferedResponse_derivesContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	res := &moduleproto.HTTPResponseResult{
		Status: 200,
		Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw("hi")},
	}
	commitBufferedResponse(rec, res, nil)
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("derived Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestCommitBufferedResponse_badBodyWrites503(t *testing.T) {
	rec := httptest.NewRecorder()
	res := &moduleproto.HTTPResponseResult{
		Status: 200,
		Body:   moduleproto.HTTPResponseBody{Kind: "unknown-kind", Value: jsonRaw(`x`)},
	}
	commitBufferedResponse(rec, res, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 on decode failure", rec.Code)
	}
}

// ---- subjectFromRequest ----

func TestSubjectFromRequest_anonymous(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := subjectFromRequest(r); got != "" {
		t.Errorf("subject without user = %q, want empty", got)
	}
}

func TestSubjectFromRequest_withUser(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(handler.SetUser(r.Context(), "user-42"))
	if got := subjectFromRequest(r); got != "user-42" {
		t.Errorf("subject = %q, want user-42", got)
	}
}

// ---- buildHTTPRequestParams ----

func TestBuildHTTPRequestParams_carriesQueryAndBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 32)
	r := httptest.NewRequest(http.MethodPost, "/items?sort=desc&status=open", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Drop", "secret") // not in allowlist
	params, err := buildHTTPRequestParams(r, "demo", "list", "req-1", "hdl", time.Second)
	if err != nil {
		t.Fatalf("buildHTTPRequestParams: %v", err)
	}
	if params.RouteID != "list" || params.RequestID != "req-1" {
		t.Errorf("ids = %+v", params)
	}
	if params.Query["sort"] != "desc" || params.Query["status"] != "open" {
		t.Errorf("query = %+v", params.Query)
	}
	if params.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type not forwarded: %+v", params.Headers)
	}
	if _, has := params.Headers["X-Drop"]; has {
		t.Error("non-allowlisted header was forwarded")
	}
	if params.BodyB64 == "" {
		t.Error("BodyB64 empty")
	}
	if params.Caller.Delegation != "hdl" {
		t.Errorf("delegation = %q", params.Caller.Delegation)
	}
}

// ---- ProxyHandler ----

func TestProxyHandler_returnsNonNil(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if h := sup.ProxyHandler("demo", "list"); h == nil {
		t.Error("ProxyHandler returned nil")
	}
}

// ---- Info / List / DeclaredRoutes (no spawn, Register installs the slot) ----

func TestInfo_unknownModuleErrors(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if _, err := sup.Info("ghost"); !errors.Is(err, ErrNoDesiredRow) {
		t.Fatalf("Info(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

func TestInfo_registeredModuleSnapshot(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	d := validDescriptor()
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	info, err := sup.Info("demo")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "demo" || info.State != StateInstalledDisabled || info.RouteCount != 2 {
		t.Errorf("Info = %+v", info)
	}
	if info.TrustTier != TrustTrusted {
		t.Errorf("TrustTier = %v, want TrustTrusted", info.TrustTier)
	}
}

func TestList_emptyReturnsEmpty(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if got := sup.List(); len(got) != 0 {
		t.Errorf("empty List = %+v, want empty", got)
	}
}

func TestList_ordersByName(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	for _, name := range []string{"zeta", "alpha", "mike"} {
		d := validDescriptor()
		d.Name = name
		// Recompute surface digest so validation passes under the new name.
		surf, err := ComputeSurfaceSHA256(d)
		if err != nil {
			t.Fatalf("surface: %v", err)
		}
		d.SurfaceSHA256 = surf
		if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	list := sup.List()
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "mike" || list[2].Name != "zeta" {
		names := []string{list[0].Name, list[1].Name, list[2].Name}
		t.Errorf("List order = %v, want [alpha mike zeta]", names)
	}
}

func TestDeclaredRoutes_unknownReturnsNil(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	if got := sup.DeclaredRoutes("ghost"); got != nil {
		t.Errorf("DeclaredRoutes(unknown) = %+v, want nil", got)
	}
}

func TestDeclaredRoutes_returnsCopy(t *testing.T) {
	sup := newBareTestSupervisor(t, newTestStore(t), &TrustedProcessRunner{})
	defer sup.Close(context.Background())
	d := validDescriptor()
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	routes := sup.DeclaredRoutes("demo")
	if len(routes) != 2 || routes[0].ID != "list" {
		t.Errorf("DeclaredRoutes = %+v", routes)
	}
	// Mutating the returned slice must not affect the descriptor's routes.
	routes[0] = RouteDeclaration{ID: "mutated", Method: "POST", Path: "/x"}
	again := sup.DeclaredRoutes("demo")
	if again[0].ID != "list" {
		t.Error("DeclaredRoutes did not return a copy")
	}
}

// ---- serveProxy deadline path over an in-memory peer (no child spawn) ----

// proxyDeadlineTestSlot wires a supervisor whose "demo" slot is Ready with
// an in-memory moduleproto peer pair and a 150ms per-call deadline. The
// slot is hand-built (no supervise/heartbeat loops), so reconcile can
// never flip its state mid-test; serveProxy runs for real.
func proxyDeadlineTestSlot(t *testing.T) (sup *ProcessModuleSupervisor, sl *moduleSlot, host, child *moduleproto.Peer, cleanup func()) {
	t.Helper()
	store := newTestStore(t)
	sup = newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	d := validToolDescriptor(t, "demo", nil)
	d.Limits.Deadline = 150 * time.Millisecond
	host, child, pipeClose := newModuleProtoPipe(t)
	sl = &moduleSlot{
		name: d.Name,
		desc: d,
		sup:  sup,
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	sl.mu.Lock()
	sl.peer = host
	sl.enabled = true
	sl.state = StateReady
	sl.mu.Unlock()
	sup.mu.Lock()
	sup.slots[d.Name] = sl
	sup.mu.Unlock()
	return sup, sl, host, child, pipeClose
}

// TestServeProxyDeadlineCancelReachesChild drives the real serveProxy path
// against a moduleproto peer pair. A proxied request whose child handler
// hangs must, when the host's per-call deadline expires: (1) receive a
// module.cancel that aborts the child handler's context, and (2) leave the
// peer healthy — the child's late paired response to the abandoned id must
// be dropped, not raised as a terminal "unsolicited response" fault — so
// the NEXT proxied request still succeeds.
//
// Both properties were dead in wiring (issue #356): watchCancel sent the
// proxy's "<name>-<counter>" RequestID, which the child's frame-id-keyed
// cancel registry can never match (module.cancel was a silent no-op on the
// proxy path; the child burned CPU/DB until its own deadline), and the
// late response then faulted the peer, tearing down every concurrent
// caller with it.
func TestServeProxyDeadlineCancelReachesChild(t *testing.T) {
	sup, sl, host, child, cleanup := proxyDeadlineTestSlot(t)
	defer cleanup()

	var hangFirst atomic.Bool
	hangFirst.Store(true)
	childCancelled := make(chan struct{}, 1)
	if err := child.Handle(moduleproto.MethodHTTP, func(ctx context.Context, _ json.RawMessage) (any, error) {
		if !hangFirst.CompareAndSwap(true, false) {
			// Subsequent calls: answer normally so the peer proves usable.
			return moduleproto.HTTPResponseResult{
				Status: http.StatusOK,
				Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw(`"ok"`)},
			}, nil
		}
		select {
		case <-ctx.Done():
			// Nothing but module.cancel (or a child-peer Close, which this
			// test never does before cleanup) cancels a served request's
			// ctx: reaching here IS the cancel notification arriving.
			childCancelled <- struct{}{}
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
			// Fallback: answer late so the "response for an abandoned id"
			// path is exercised even if cancel never arrives.
			return moduleproto.HTTPResponseResult{
				Status: http.StatusOK,
				Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw(`"late"`)},
			}, nil
		}
	}); err != nil {
		t.Fatal(err)
	}

	// First proxied call: hangs past the 150ms descriptor deadline → 503.
	rec := httptest.NewRecorder()
	sup.ProxyHandler(sl.desc.Name, "r1").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("first proxied call: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// (1) module.cancel must reach the child handler promptly — well
	// before the handler's own 3s fallback.
	select {
	case <-childCancelled:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("child handler was not cancelled by module.cancel after the host deadline expired: per-call cancellation is a no-op on the proxy path (watchCancel sent an id the child cannot resolve)")
	}

	// (2) The child's late paired response to the abandoned id must not
	// fault the peer. Give the response a window to land, then require
	// the peer healthy and a fresh proxied call green.
	select {
	case <-host.FatalDone():
		t.Fatalf("late response to the abandoned id faulted the peer: %v", host.FatalError())
	case <-host.Done():
		t.Fatalf("host read loop exited after late response: %v", host.FatalError())
	case <-time.After(400 * time.Millisecond):
	}

	rec2 := httptest.NewRecorder()
	sup.ProxyHandler(sl.desc.Name, "r1").ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/r", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("peer unusable after late response to abandoned id: second proxied call status = %d, peer fatal: %v", rec2.Code, host.FatalError())
	}
}

// jsonRaw is a tiny helper to build json.RawMessage without repeating the cast.
func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }
