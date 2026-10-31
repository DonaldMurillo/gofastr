package rest

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// The past listing capped the log at 50 sessions before the token's
// scope applied, so a token bound to an older session found nothing
// (#621 review). The scope goes to the store, ahead of the cap.
func TestPastSessionScopeBeforeCap(t *testing.T) {
	s := newServer(t)
	old := ids.NewSessionID()
	var rows []session.PastSession
	for range 60 {
		rows = append(rows, session.PastSession{SessionID: ids.NewSessionID()})
	}
	rows = append(rows, session.PastSession{SessionID: old, FirstMessage: "mine"})
	s.SessionStore = fakePastStore{rows: rows}
	tok, err := s.Encoder.Encode(auth.Claims{Ver: auth.VerCurrent, JTI: ids.NewJTI(), Sessions: []ids.SessionID{old}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/sessions?past=true", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var got []session.PastSession
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	if len(got) != 1 || got[0].SessionID != old {
		t.Errorf("a token bound to a session behind 60 newer ones listed %v", got)
	}
}

// The route wrapper always installs claims, so the guard for a request
// without them is driven on the handler itself.
func TestSessionsWithoutClaimsIs403(t *testing.T) {
	s := newServer(t)
	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest("GET", "/v1/sessions", nil))
	if rec.Code != 403 {
		t.Errorf("handleSessions with no claims = %d, want 403", rec.Code)
	}
}

// A token bound to session A listed every live and past session, past
// ones with the first 80 bytes of their first prompt (2026-10-27 H10).
// The listings now hold only the sessions the token may attach to.
func TestSessionListHonoursTokenScope(t *testing.T) {
	s := newServer(t)
	sessA, sessB := ids.NewSessionID(), ids.NewSessionID()
	for _, id := range []ids.SessionID{sessA, sessB} {
		bus := engine.NewBus(id)
		t.Cleanup(func() { bus.Close() })
		s.Catalog.RegisterEngine(engine.NewEngine(id, bus, strictWireProvider{}, "fake", engine.NewDispatcher(bus, tool.NewRegistry())))
	}
	s.SessionStore = fakePastStore{rows: []session.PastSession{
		{SessionID: sessA, FirstMessage: "mine"},
		{SessionID: sessB, FirstMessage: "someone else's prompt"},
	}}
	tok, err := s.Encoder.Encode(auth.Claims{Ver: auth.VerCurrent, JTI: ids.NewJTI(), Sessions: []ids.SessionID{sessA}})
	if err != nil {
		t.Fatal(err)
	}
	list := func(path string) []ids.SessionID {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Harness-Token", tok)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
		var rows []struct {
			Live ids.SessionID `json:"session"`
			Past ids.SessionID `json:"sessionId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		var out []ids.SessionID
		for _, r := range rows {
			out = append(out, r.Live+r.Past)
		}
		return out
	}
	for _, path := range []string{"/v1/sessions", "/v1/sessions?past=true"} {
		if got := list(path); len(got) != 1 || got[0] != sessA {
			t.Errorf("SECURITY: GET %s with a token bound to %s listed %v", path, sessA, got)
		}
	}
}
