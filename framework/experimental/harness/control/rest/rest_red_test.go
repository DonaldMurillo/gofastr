//go:build red

package rest

import (
	"context"
	"net/http/httptest"
	"strings"
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

// Property: inbound JSON on operator-reachable control-plane routes is
// decoded under strict top-level key rules (duplicate keys rejected,
// keys must exact-match the struct's json tags) — the repo standard
// core/handler/bind.go validateBodyKeys enforces on every production
// Bind surface.
// Surfaces: rest.go handlePOST /v1/sessions/{id}/input (SendInput) —
// loopback bearer-token operator tool, dev harness surface.
// Finding: rest.go:268 sets DisallowUnknownFields but that guard only
// rejects keys matching NO field; a duplicate top-level key is silently
// resolved last-wins and a case-folded key ("CONTENT" vs "content")
// still matches its tag case-insensitively. encoding/json therefore
// accepts both smuggle shapes and dispatches the command (202), so a
// proxy/logger/audit layer that de-duplicates differently than
// last-wins sees a different request than the engine executes.
// Fix direction: run the body through a validateBodyKeys-equivalent
// (buffered, then strict decode) before Decode, 400 InvalidBody on
// duplicate or case-folded top-level keys.
// Round-6 mechanism split: exact duplicates and case-folded keys are
// separate top-level tests below (independently fixable mechanisms).

// redFakeProvider streams one text delta and stops.
type redFakeProvider struct{}

func (redFakeProvider) Name() string { return "fake" }
func (redFakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "rest-hello"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (redFakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (redFakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// newRedWiredServer builds a Server with a live engine behind the mux
// so a 202 proves the body was decoded AND dispatched (a rejection
// then means the guard fired, not broken plumbing).
func newRedWiredServer(t *testing.T) (*Server, ids.SessionID, *engine.Engine, string) {
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

// postRedInput POSTs one SendInput body and waits for the async turn
// to finish so the bus is quiet before cleanup.
func postRedInput(t *testing.T, s *Server, eng *engine.Engine, sess ids.SessionID, tok, body string) int {
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

// TestHarnessRestRedRejectsDuplicateKeys: exact duplicate top-level
// "content" keys — wire-level last-wins.
func TestHarnessRestRedRejectsDuplicateKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed shape of the same
	// body dispatches (202), so the 400 demanded below can only come
	// from key strictness, not plumbing.
	hs, hsess, heng, htok := newRedWiredServer(t)
	if code := postRedInput(t, hs, heng, hsess, htok, `{"content":[{"type":"text","text":"rest-hello"}]}`); code != 202 {
		t.Fatalf("happy path: well-formed body must dispatch (got %d, body=%s)", code, "")
	}

	s, sess, eng, tok := newRedWiredServer(t)
	code := postRedInput(t, s, eng, sess, tok,
		`{"content":[{"type":"text","text":"first"}],"content":[{"type":"text","text":"second"}]}`)
	if code != 400 {
		t.Errorf("SECURITY: [rest-strict-keys] POST /v1/sessions/{id}/input accepted a body "+
			"with a smuggled key shape (duplicate top-level content key): status %d, want 400 InvalidBody. rest.go:268 sets "+
			"DisallowUnknownFields but encoding/json resolves duplicate top-level keys "+
			"last-wins, so the command dispatches. Apply the "+
			"core/handler validateBodyKeys standard (reject duplicates + case-folded keys) "+
			"before Decode.", code)
	}
}

// TestHarnessRestRedRejectsCaseFoldedKeys: "CONTENT" case-folds onto the
// tagged "content" field via stdlib json's tag-insensitive match — a
// duplicate modulo folding; survives a dedup-only fix.
func TestHarnessRestRedRejectsCaseFoldedKeys(t *testing.T) {
	// Happy guard on its own stack: the well-formed shape of the same
	// body dispatches (202), so the 400 demanded below can only come
	// from key strictness, not plumbing.
	hs, hsess, heng, htok := newRedWiredServer(t)
	if code := postRedInput(t, hs, heng, hsess, htok, `{"content":[{"type":"text","text":"rest-hello"}]}`); code != 202 {
		t.Fatalf("happy path: well-formed body must dispatch (got %d, body=%s)", code, "")
	}

	s, sess, eng, tok := newRedWiredServer(t)
	code := postRedInput(t, s, eng, sess, tok, `{"CONTENT":[{"type":"text","text":"rest-hello"}]}`)
	if code != 400 {
		t.Errorf("SECURITY: [rest-strict-keys] POST /v1/sessions/{id}/input accepted a body "+
			"with a smuggled key shape (case-folded top-level keys): status %d, want 400 InvalidBody. rest.go:268 sets "+
			"DisallowUnknownFields but encoding/json still matches case-folded tags, "+
			"so the command dispatches. Apply the "+
			"core/handler validateBodyKeys standard (reject duplicates + case-folded keys) "+
			"before Decode.", code)
	}
}
