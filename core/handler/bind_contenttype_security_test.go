package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pins: JSON body decoding is gated on Content-Type: application/json for
// every destination shape (2026-09-06 adversarial pass, round 5).
// Property: JSON body decoding is gated on Content-Type: application/json
// for EVERY destination shape — Bind's doc promises the gate ("If the
// request has a JSON body and Content-Type is application/json, the body is
// decoded first") and TestBind_NonJSONContentTypeSkipped pins the struct
// branch only.
// Surfaces: core/handler/bind.go non-struct-pointer branch — bindBody(r, dst)
// with no IsJSONContentType check, while the struct branch checks it.
// Finding: a cross-site POST (text/plain is CORS-simple — no preflight) with
// a JSON body into *map[string]any or *string binds; the struct branch
// correctly skips the same body.
// Fix direction: apply the struct branch's IsJSONContentType gate to the
// non-struct branch before bindBody.

func TestBindRedNonStructJSONGate(t *testing.T) {
	// Negative: text/plain JSON body must NOT decode into non-struct dsts —
	// parity with the struct branch, which skips the body without error.
	t.Run("map dst not decoded under text/plain", func(t *testing.T) {
		var m map[string]any
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"evil":true}`))
		req.Header.Set("Content-Type", "text/plain")
		err := Bind(req, &m)
		if len(m) > 0 {
			t.Errorf("SECURITY: [bind-nonstruct-ct] Bind decoded a JSON body into *map[string]any "+
				"under Content-Type text/plain (m=%v, err=%v). Attack: text/plain is a CORS-simple "+
				"method-free content type — a cross-site form or fetch posts it with no preflight, "+
				"and the struct branch already refuses this exact body; the non-struct branch must "+
				"enforce the same gate.", m, err)
		}
	})
	t.Run("string dst not decoded under text/plain", func(t *testing.T) {
		var s string
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`"evil"`))
		req.Header.Set("Content-Type", "text/plain")
		err := Bind(req, &s)
		if s != "" {
			t.Errorf("SECURITY: [bind-nonstruct-ct] Bind decoded a JSON body into *string under "+
				"Content-Type text/plain (s=%q, err=%v). Attack: same cross-site no-preflight POST "+
				"as the map arm; the gate must hold for every destination shape.", s, err)
		}
	})

	// Positive control: application/json still decodes both shapes.
	t.Run("map dst decoded under application/json", func(t *testing.T) {
		var m map[string]any
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"ok":true}`))
		req.Header.Set("Content-Type", "application/json")
		if err := Bind(req, &m); err != nil {
			t.Fatalf("setup broken: Bind failed on a well-formed JSON body: %v", err)
		}
		if v, present := m["ok"]; !present || v != true {
			t.Errorf("SECURITY: [bind-nonstruct-ct] positive control: application/json body must "+
				"still decode into *map[string]any (m=%v)", m)
		}
	})
	t.Run("string dst decoded under application/json", func(t *testing.T) {
		var s string
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`"hello"`))
		req.Header.Set("Content-Type", "application/json")
		if err := Bind(req, &s); err != nil {
			t.Fatalf("setup broken: Bind failed on a well-formed JSON body: %v", err)
		}
		if s != "hello" {
			t.Errorf("SECURITY: [bind-nonstruct-ct] positive control: application/json body must "+
				"still decode into *string (s=%q)", s)
		}
	})
}

// TestIsJSONContentType pins the shared Content-Type gate table. The
// auth battery's former hand-rolled copy (battery/auth json_limit.go)
// answered the same for every input its tests exercised (text/plain,
// missing, application/json); battery/auth now calls this function, so
// this table is the one place those answers are pinned. The documented
// deltas over the old auth copy: any "+json" suffix counts (not only
// application/*+json), and malformed parameters reject the value.
func TestIsJSONContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"", false},                // missing Content-Type → gate closed
		{"application/json", true}, // the gate's pass case
		{"application/json; charset=utf-8", true},
		{"APPLICATION/JSON", true},   // ParseMediaType lowercases the type
		{"text/plain", false},        // auth's smuggle-reject case
		{"application/jsonp", false}, // literal-prefix trick must not match
		{"application/json-evil", false},
		{"application/vnd.api+json", true}, // RFC 6839 structured suffix
		{"text/calendar+json", true},       // suffix counts from any type (widened vs the auth copy)
		{"text/event-stream", false},
		{"application/json; charset=\"unclosed", false}, // params must parse (strict vs the auth copy)
	}
	for _, tc := range cases {
		if got := IsJSONContentType(tc.ct); got != tc.want {
			t.Errorf("IsJSONContentType(%q) = %v, want %v", tc.ct, got, tc.want)
		}
	}
}
