//go:build red

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
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// ---------------------------------------------------------------------------
// Property: a stream opened by a credential that is later revoked stops
// delivering (or terminates) — revocation must reach held streams, not just
// new requests.
// Surfaces: rest.go handleSSE (GET /v1/sessions/{id}/events) — the harness
// control plane's operator SSE stream (loopback bearer-token transport).
// Finding: the handle() wrapper verifies the bearer token (signature,
// expiry, RevocationList) on every request, and that part is solid — but
// handleSSE then subscribes to the bus and loops until ctx.Done()
// (rest.go:352-366) with no re-verify of the verified claims, no
// heartbeat, and no stream bound. RevocationList.Revoke(jti) is therefore
// invisible to an already-open events stream: a token that 401s on every
// fresh request keeps receiving the session's full event flow until the
// client disconnects.
// Severity: loopback operator tool (dev harness surface; per-request
// verification on the REST routes is solid — the held SSE loop is the
// asymmetric surface; revocation's whole point on this plane is to cut off
// a compromised/stale operator session immediately).
// Fix direction: re-check revocation inside the loop (IsRevoked on the
// stashed claims' jti, or re-Verify on a bounded interval / between
// events) and terminate the stream when it flips; or bound the stream
// lifetime so clients cycle back through the per-request gate.
// ---------------------------------------------------------------------------

// newRedRevokedServer builds a wired Server like newRedWiredServer but
// mints two session-bound tokens: tokSSE (the one the stream opens with
// and that will be revoked) and tokLive (a second operator that stays
// valid and drives turns, so events keep flowing legitimately).
func newRedRevokedServer(t *testing.T) (*Server, ids.SessionID, *engine.Engine, string, ids.JTI, string) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	eng := engine.NewEngine(session, bus, redFakeProvider{}, "fake", engine.NewDispatcher(bus, reg))
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

// redStreamBuf is a mutex-guarded sink so the test can snapshot what the
// SSE stream has delivered so far without racing the reader goroutine.
type redStreamBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *redStreamBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *redStreamBuf) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// redWaitUntil polls cond until it holds or the deadline passes.
func redWaitUntil(deadline time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return cond()
}

func TestHarnessRestRedRevokedMidStream(t *testing.T) {
	s, sess, eng, tokSSE, jtiSSE, tokLive := newRedRevokedServer(t)
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
	stream := &redStreamBuf{}
	readDone := make(chan struct{})
	go func() { _, _ = io.Copy(stream, resp.Body); close(readDone) }()

	// Control: a turn driven by tokLive (not revoked) reaches the stream,
	// so the absence demanded below can only come from the revocation.
	if code := postRedInput(t, s, eng, sess, tokLive, `{"content":[{"type":"text","text":"control"}]}`); code != 202 {
		t.Fatalf("control turn: got %d, want 202 — harness problem", code)
	}
	if !redWaitUntil(2*time.Second, func() bool { return strings.Contains(stream.snapshot(), "event: TurnStarted") }) {
		t.Fatalf("control failed: the live stream never delivered the pre-revocation turn — harness problem, not a finding. stream=%s", stream.snapshot())
	}

	// Revoke the token that opened the stream, then prove the revocation is
	// live on this surface's own semantics: a fresh request with the same
	// token is refused 401.
	s.Revocations.Revoke(jtiSSE)
	freshRec := httptest.NewRecorder()
	freshReq, _ := http.NewRequest(http.MethodPost, "/v1/sessions/"+string(sess)+"/input",
		strings.NewReader(`{"content":[{"type":"text","text":"blocked"}]}`))
	freshReq.Header.Set("X-Harness-Token", tokSSE)
	s.Handler().ServeHTTP(freshRec, freshReq)
	if freshRec.Code != http.StatusUnauthorized {
		t.Fatalf("control: a fresh request with the revoked token must 401 (got %d, body=%s) — otherwise the revocation below is not observable", freshRec.Code, freshRec.Body.String())
	}

	// The still-valid second operator drives another turn. The revoked
	// stream must not carry it (today it does).
	if code := postRedInput(t, s, eng, sess, tokLive, `{"content":[{"type":"text","text":"after-revoke"}]}`); code != 202 {
		t.Fatalf("second turn: got %d, want 202 — harness problem", code)
	}
	redWaitUntil(2*time.Second, func() bool {
		return strings.Count(stream.snapshot(), "event: TurnStarted") >= 2
	})

	cancel()
	_ = resp.Body.Close()
	<-readDone
	if got := strings.Count(stream.snapshot(), "event: TurnStarted"); got >= 2 {
		t.Errorf("SECURITY: [rest-sse-stale-revocation] the events stream kept delivering after the "+
			"token that opened it was revoked: %d turns delivered, and a fresh request with the same "+
			"token 401s. handleSSE verifies claims per request via the handle() wrapper, but the held "+
			"loop (rest.go:352) runs until ctx.Done with no re-verify, no heartbeat, and no bound, so "+
			"Revoke(jti) is invisible to an open stream. Re-check revocation in the loop or bound the "+
			"stream lifetime. stream=%.400s", got, stream.snapshot())
	}
}
