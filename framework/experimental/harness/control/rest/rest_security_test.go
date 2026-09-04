package rest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// TestRESTTokenScopeEnforced asserts the REST control plane honours a
// capability token's session/command scope, a token bound to session A
// must not be able to drive a different live session B, nor issue a
// command kind its claims forbid. Mirrors the ws.go AllowsSession check.
func TestRESTTokenScopeEnforced(t *testing.T) {
	sessA := ids.NewSessionID()
	sessB := ids.NewSessionID()

	// Token scoped to sessA only, with full command rights.
	scopedTok := func(s *Server) string {
		tok, err := s.Encoder.Encode(auth.Claims{
			Ver:      auth.VerCurrent,
			JTI:      ids.NewJTI(),
			Sessions: []ids.SessionID{sessA},
		})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	// Token scoped to sessA, but only allowed to SendInput, must not
	// be able to answer a permission prompt.
	inputOnlyTok := func(s *Server) string {
		tok, err := s.Encoder.Encode(auth.Claims{
			Ver:      auth.VerCurrent,
			JTI:      ids.NewJTI(),
			Sessions: []ids.SessionID{sessA},
			Commands: []string{"SendInput"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	cases := []struct {
		name   string
		path   string
		method string
		body   string
		tok    func(*Server) string
		// wantForbidden true means the request must NOT reach the mux
		// (403). We do not assert the success body, only that scope is
		// the gate.
		wantForbidden bool
	}{
		{
			name:          "cross-session permission answer is rejected",
			path:          "/v1/sessions/" + string(sessB) + "/permission",
			method:        "POST",
			body:          `{"decision":"allow"}`,
			tok:           scopedTok,
			wantForbidden: true,
		},
		{
			name:          "cross-session input is rejected",
			path:          "/v1/sessions/" + string(sessB) + "/input",
			method:        "POST",
			body:          `{"content":[]}`,
			tok:           scopedTok,
			wantForbidden: true,
		},
		{
			name:          "cross-session event stream is rejected",
			path:          "/v1/sessions/" + string(sessB) + "/events",
			method:        "GET",
			tok:           scopedTok,
			wantForbidden: true,
		},
		{
			name:          "forbidden command kind is rejected on own session",
			path:          "/v1/sessions/" + string(sessA) + "/permission",
			method:        "POST",
			body:          `{"decision":"allow"}`,
			tok:           inputOnlyTok,
			wantForbidden: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t)
			var bodyR *strings.Reader
			if tc.body != "" {
				bodyR = strings.NewReader(tc.body)
			} else {
				bodyR = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyR)
			req.Header.Set("X-Harness-Token", tc.tok(s))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if tc.wantForbidden && rec.Code != 403 {
				t.Fatalf("status = %d, want 403 (scope enforced); body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ─── Strict-key decode + mid-stream revocation ─────────────────────
// Both properties drive the wired server below: the strict-keys tests
// need a live engine so a 202 proves decode+dispatch (a rejection then
// means the guard fired, not broken plumbing), and the revocation test
// needs two operators on one session.

// strictWireProvider streams one text delta and stops.
type strictWireProvider struct{}

func (strictWireProvider) Name() string { return "fake" }
func (strictWireProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "rest-hello"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (strictWireProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (strictWireProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// newWiredTestServer builds a Server with a live engine behind the mux.
func newWiredTestServer(t *testing.T) (*Server, ids.SessionID, *engine.Engine, string) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	eng := engine.NewEngine(session, bus, strictWireProvider{}, "fake", engine.NewDispatcher(bus, reg))
	mux := multiplex.New()
	mux.RegisterEngine(eng)
	t.Cleanup(func() { bus.Close() })

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	s := &Server{
		Mux:         mux,
		Catalog:     resources.NewCatalog(),
		Encoder:     auth.NewEncoder(secret),
		Revocations: auth.NewRevocationList(),
		Features:    []string{"rest"},
	}
	tok, err := s.Encoder.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		Sessions:      []ids.SessionID{session},
		IdentityClass: control.IdentityHuman,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	return s, session, eng, tok
}

// postTestInput POSTs one SendInput body and waits for the async turn
// to finish so the bus is quiet before cleanup.
func postTestInput(t *testing.T, s *Server, eng *engine.Engine, sess ids.SessionID, tok, body string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := eng.Bus.Subscribe(ctx)

	req := httptest.NewRequest("POST", "/v1/sessions/"+string(sess)+"/input", strings.NewReader(body))
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Kind == "TurnEnded" {
				return rec.Code
			}
		case <-deadline:
			return rec.Code
		}
	}
}

// TestRestInputRejectsDuplicateKeys: POSTed command bodies refuse an
// exact duplicate top-level key — stdlib json resolves duplicates
// last-wins, so the command the server executes can differ from the one
// any first-read intermediary saw.
func TestRestInputRejectsDuplicateKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed shape dispatches
	// (202), so the 400 demanded below can only come from key
	// strictness, not plumbing.
	hs, hsess, heng, htok := newWiredTestServer(t)
	if code := postTestInput(t, hs, heng, hsess, htok, `{"content":[{"type":"text","text":"rest-hello"}]}`); code != 202 {
		t.Fatalf("happy path: well-formed body must dispatch (got %d)", code)
	}

	s, sess, eng, tok := newWiredTestServer(t)
	code := postTestInput(t, s, eng, sess, tok,
		`{"content":[{"type":"text","text":"first"}],"content":[{"type":"text","text":"second"}]}`)
	if code != 400 {
		t.Errorf("SECURITY: [rest-strict-keys] POST /v1/sessions/{id}/input accepted a body "+
			"with a smuggled key shape (duplicate top-level content key): status %d, want 400 InvalidBody — "+
			"encoding/json resolves duplicate top-level keys last-wins, so the command dispatches; "+
			"the decode must go through handler.UnmarshalStrict", code)
	}
}

// TestRestInputRejectsCaseFoldedKeys: "CONTENT" case-folds onto the
// tagged "content" field via stdlib json's tag-insensitive match — a
// duplicate modulo folding; survives a dedup-only fix.
func TestRestInputRejectsCaseFoldedKeys(t *testing.T) {
	hs, hsess, heng, htok := newWiredTestServer(t)
	if code := postTestInput(t, hs, heng, hsess, htok, `{"content":[{"type":"text","text":"rest-hello"}]}`); code != 202 {
		t.Fatalf("happy path: well-formed body must dispatch (got %d)", code)
	}

	s, sess, eng, tok := newWiredTestServer(t)
	code := postTestInput(t, s, eng, sess, tok, `{"CONTENT":[{"type":"text","text":"rest-hello"}]}`)
	if code != 400 {
		t.Errorf("SECURITY: [rest-strict-keys] POST /v1/sessions/{id}/input accepted a body "+
			"with a smuggled key shape (case-folded top-level keys): status %d, want 400 InvalidBody — "+
			"encoding/json still matches case-folded tags, so the command dispatches; "+
			"the decode must go through handler.UnmarshalStrict", code)
	}
}

// newRevokedServer builds a wired Server like newWiredTestServer but
// mints two session-bound tokens: tokSSE (the one the stream opens with
// and that will be revoked) and tokLive (a second operator that stays
// valid and drives turns, so events keep flowing legitimately).
func newRevokedServer(t *testing.T) (*Server, ids.SessionID, *engine.Engine, string, ids.JTI, string) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	eng := engine.NewEngine(session, bus, strictWireProvider{}, "fake", engine.NewDispatcher(bus, reg))
	mux := multiplex.New()
	mux.RegisterEngine(eng)
	t.Cleanup(func() { bus.Close() })

	secret, err := auth.GenerateSecret()
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	s := &Server{
		Mux:         mux,
		Catalog:     resources.NewCatalog(),
		Encoder:     auth.NewEncoder(secret),
		Revocations: auth.NewRevocationList(),
		Features:    []string{"rest"},
	}
	mint := func() (string, ids.JTI) {
		jti := ids.NewJTI()
		tok, err := s.Encoder.Encode(auth.Claims{
			Ver:           auth.VerCurrent,
			JTI:           jti,
			Sessions:      []ids.SessionID{session},
			IdentityClass: control.IdentityHuman,
			ExpiresAt:     time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("encode token: %v", err)
		}
		return tok, jti
	}
	tokSSE, jtiSSE := mint()
	tokLive, _ := mint()
	return s, session, eng, tokSSE, jtiSSE, tokLive
}

// streamBuf is a mutex-guarded sink so the test can snapshot what the
// SSE stream has delivered so far without racing the reader goroutine.
type streamBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *streamBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *streamBuf) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitUntil polls cond until it holds or the deadline passes.
func waitUntil(deadline time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return cond()
}

// TestRestSSEStreamStopsOnRevocation: a stream opened by a credential
// that is later revoked stops delivering — revocation must reach held
// streams, not just new requests (handleSSE re-verifies before every
// event write and on a ticker).
func TestRestSSEStreamStopsOnRevocation(t *testing.T) {
	s, sess, eng, tokSSE, jtiSSE, tokLive := newRevokedServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// Open the SSE stream under tokSSE. handleSSE writes and flushes the
	// headers immediately, so client.Do returns once the stream is live.
	sctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(sctx, http.MethodGet,
		srv.URL+"/v1/sessions/"+string(sess)+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Harness-Token", tokSSE)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	stream := &streamBuf{}
	readDone := make(chan struct{})
	go func() { _, _ = io.Copy(stream, resp.Body); close(readDone) }()

	// Control: a turn driven by tokLive (not revoked) reaches the stream,
	// so the absence demanded below can only come from the revocation.
	if code := postTestInput(t, s, eng, sess, tokLive, `{"content":[{"type":"text","text":"control"}]}`); code != 202 {
		t.Fatalf("control turn: got %d, want 202 — harness problem", code)
	}
	if !waitUntil(2*time.Second, func() bool { return strings.Contains(stream.snapshot(), "event: TurnStarted") }) {
		t.Fatalf("control failed: the live stream never delivered the pre-revocation turn — harness problem. stream=%s", stream.snapshot())
	}

	// Revoke the token that opened the stream, then prove the revocation
	// is live on this surface's own semantics: a fresh request with the
	// same token is refused 401.
	s.Revocations.Revoke(jtiSSE)
	freshRec := httptest.NewRecorder()
	freshReq, _ := http.NewRequest(http.MethodPost, "/v1/sessions/"+string(sess)+"/input",
		strings.NewReader(`{"content":[{"type":"text","text":"blocked"}]}`))
	freshReq.Header.Set("X-Harness-Token", tokSSE)
	s.Handler().ServeHTTP(freshRec, freshReq)
	if freshRec.Code != http.StatusUnauthorized {
		t.Fatalf("control: a fresh request with the revoked token must 401 (got %d, body=%s)", freshRec.Code, freshRec.Body.String())
	}

	// The still-valid second operator drives another turn. The revoked
	// stream must not carry it.
	if code := postTestInput(t, s, eng, sess, tokLive, `{"content":[{"type":"text","text":"after-revoke"}]}`); code != 202 {
		t.Fatalf("second turn: got %d, want 202 — harness problem", code)
	}
	waitUntil(2*time.Second, func() bool {
		return strings.Count(stream.snapshot(), "event: TurnStarted") >= 2
	})

	cancel()
	_ = resp.Body.Close()
	<-readDone
	if got := strings.Count(stream.snapshot(), "event: TurnStarted"); got >= 2 {
		t.Errorf("SECURITY: [rest-sse-stale-revocation] the events stream kept delivering after the "+
			"token that opened it was revoked: %d turns delivered, and a fresh request with the same "+
			"token 401s. The held loop must re-verify the credential between events and on a ticker. "+
			"stream=%.400s", got, stream.snapshot())
	}
}
