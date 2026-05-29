package rest

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestRESTTokenScopeEnforced asserts the REST control plane honours a
// capability token's session/command scope — a token bound to session A
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

	// Token scoped to sessA, but only allowed to SendInput — must not
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
		// (403). We do not assert the success body — only that scope is
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
