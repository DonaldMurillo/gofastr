package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// classToken mints a token for session with the given identity class.
func classToken(t *testing.T, s *Server, session ids.SessionID, class control.IdentityClass) string {
	t.Helper()
	return jtiToken(t, s, session, class, ids.NewJTI())
}

// jtiToken mints a token with an explicit JTI.
func jtiToken(t *testing.T, s *Server, session ids.SessionID, class control.IdentityClass, jti ids.JTI) string {
	t.Helper()
	tok, err := s.Encoder.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           jti,
		Sessions:      []ids.SessionID{session},
		IdentityClass: class,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("encode token: %v", err)
	}
	return tok
}

// A REST command runs as the token's identity class. An agent-class
// token cannot answer a permission prompt, which needs a human; a
// human-class token can. Every REST caller used to be stamped human.
func TestRestAnswerUsesTokenClass(t *testing.T) {
	for _, tc := range []struct {
		class control.IdentityClass
		want  int
	}{
		{control.IdentityAgent, http.StatusConflict},
		{control.IdentityHuman, http.StatusAccepted},
	} {
		t.Run(tc.class.String(), func(t *testing.T) {
			s, session, _, _ := newWiredTestServer(t)
			tok := classToken(t, s, session, tc.class)
			call := ids.NewCallID()
			answers := s.Mux.Subscribe(session, call)
			defer s.Mux.Unsubscribe(session, call)

			req := httptest.NewRequest("POST", "/v1/sessions/"+string(session)+"/permission",
				strings.NewReader(`{"callId":"`+string(call)+`","decision":"allow"}`))
			req.Header.Set("X-Harness-Token", tok)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s token answer: status %d, want %d (%s)", tc.class, rec.Code, tc.want, rec.Body)
			}
			select {
			case a := <-answers:
				if tc.class != control.IdentityHuman {
					t.Fatalf("%s token's answer reached the prompt: %+v", tc.class, a)
				}
			default:
				if tc.class == control.IdentityHuman {
					t.Fatal("human answer never reached the prompt")
				}
			}
		})
	}
}

// Every REST request made with one token dispatches as the same client,
// so a turn's originator names the token that started it, and a second
// token is a different client. A JTI without the tok_ shape, which the
// verifier accepts, is one client too.
func TestRestClientIDStablePerToken(t *testing.T) {
	s, session, eng, _ := newWiredTestServer(t)
	tokA := classToken(t, s, session, control.IdentityHuman)
	tokB := classToken(t, s, session, control.IdentityHuman)
	tokC := jtiToken(t, s, session, control.IdentityHuman, "custom-jti")
	originator := func(tok string) ids.ClientID {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		events := eng.Bus.Subscribe(ctx)
		req := httptest.NewRequest("POST", "/v1/sessions/"+string(session)+"/input",
			strings.NewReader(`{"content":[{"type":"text","text":"hi"}]}`))
		req.Header.Set("X-Harness-Token", tok)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("input: status %d (%s)", rec.Code, rec.Body)
		}
		var who ids.ClientID
		deadline := time.After(2 * time.Second)
		for {
			select {
			case env := <-events:
				ev, err := control.DecodeEvent(env)
				if err != nil {
					t.Fatal(err)
				}
				switch e := ev.(type) {
				case control.TurnStarted:
					who = e.Originator
				case control.TurnEnded:
					return who
				}
			case <-deadline:
				t.Fatal("turn never ended")
			}
		}
	}
	a1, a2, b := originator(tokA), originator(tokA), originator(tokB)
	if a1 == "" || a1 != a2 {
		t.Errorf("one token dispatched as %q then %q, want one stable client", a1, a2)
	}
	if !ids.ValidClient(a1) {
		t.Errorf("client id %q is not a valid ClientID", a1)
	}
	if b == a1 {
		t.Errorf("two tokens dispatched as the same client %q", a1)
	}
	c1, c2 := originator(tokC), originator(tokC)
	if c1 == "" || c1 != c2 || c1 == a1 {
		t.Errorf("custom-JTI token dispatched as %q then %q, want one client of its own", c1, c2)
	}
}
