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
