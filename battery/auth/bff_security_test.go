package auth

// BFF surface: origin echo and bearer-classification boundaries.
//
// Property 1: the BFF API surface never reflects an Origin it did not
// allow-list, and reflects an allowed one EXACTLY — byte-for-byte, never
// a derived/normalized echo. A reflected Access-Control-Allow-Origin with
// Allow-Credentials:true is a credentialed cross-origin read primitive.
//
// Property 2: every Authorization shape that IS a bearer credential
// (case-insensitive scheme, any whitespace run) is classified as one, so
// a JWT cannot sneak past the BFF's bearer-JWT rejection by spelling.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func newBFFGuardApp(t *testing.T, extra ...framework.AppOption) *framework.App {
	t.Helper()
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	opts := append([]framework.AppOption{
		WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}),
	}, extra...)
	return framework.NewApp(opts...)
}

// Surfaces: one guard decision per Origin shape, observed through the
// response status AND the Access-Control-Allow-Origin header actually
// written. The allow-list echo is the only header value that may appear.
func TestBFFOriginEchoAllowlistOnly(t *testing.T) {
	app := newBFFGuardApp(t)
	app.Router().Get("/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name     string
		origin   string
		wantCode int
	}{
		{"allow-listed origin", "https://app.example.com", http.StatusNoContent},
		{"foreign origin", "https://evil.example", http.StatusForbidden},
		{"serialized null origin", "null", http.StatusForbidden},
		{"trailing-slash variant", "https://app.example.com/", http.StatusForbidden},
		{"uppercase-host variant", "https://APP.example.com", http.StatusForbidden},
		{"subdomain lookalike", "https://app.example.com.evil.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("Origin %q: status = %d, want %d", tc.origin, rec.Code, tc.wantCode)
			}
			echo := rec.Header().Get("Access-Control-Allow-Origin")
			if echo != "" && echo != "https://app.example.com" {
				t.Errorf("SECURITY: [bff-origin-echo] Origin %q was reflected as Access-Control-Allow-Origin %q with Allow-Credentials:true — only the exact allow-listed origin may ever be echoed", tc.origin, echo)
			}
			if tc.wantCode == http.StatusNoContent {
				if echo != "https://app.example.com" {
					t.Errorf("allowed Origin was not echoed: ACAO=%q", echo)
				}
				if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
					t.Errorf("allowed Origin missing Allow-Credentials:true")
				}
				if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
					t.Errorf("allowed Origin response missing Vary: Origin (cached per-origin echo)")
				}
			}
		})
	}
}

// Surfaces: the Authorization header's spelling variants. strings.Fields
// splits on any whitespace run and EqualFold matches the scheme, so every
// legitimate bearer spelling must classify as a credential (and be
// rejected while bearer JWTs are disabled). The non-credential controls
// must keep passing: a gfsk_ API token is the BFF's own automation
// credential, and a 3-token Authorization value is not a credential pair
// at all (every HTTP parser treats exactly two as canonical).
func TestBFFBearerRejectedEverySpelling(t *testing.T) {
	app := newBFFGuardApp(t)
	app.Router().Get("/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name        string
		auth        string
		wantCode    int
		description string
	}{
		{"lowercase bearer", "bearer header.payload.signature", http.StatusUnauthorized, "case-insensitive scheme"},
		{"uppercase BEARER", "BEARER header.payload.signature", http.StatusUnauthorized, "case-insensitive scheme"},
		{"double space", "Bearer  header.payload.signature", http.StatusUnauthorized, "any whitespace run is a separator"},
		{"tab separator", "Bearer\theader.payload.signature", http.StatusUnauthorized, "any whitespace run is a separator"},
		{"gfsk api token", "Bearer gfsk_automation-token", http.StatusNoContent, "API tokens are the BFF's own credential"},
		{"basic scheme", "Basic dXNlcjpwYXNz", http.StatusNoContent, "not a bearer credential"},
		{"three tokens", "Bearer header.payload.signature extra", http.StatusNoContent, "not a credential pair (2 tokens is canonical)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
			req.Header.Set("Authorization", tc.auth)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("Authorization %q (%s): status = %d, want %d", tc.auth, tc.description, rec.Code, tc.wantCode)
			}
		})
	}
}

// Boundary pin: the guard covers exactly the API prefix SUBTREE. The
// match is on the whole prefix and on prefix+'/' — a path sharing the
// prefix's leading characters but not its segment boundary (/apifoo) is
// a different route tree and stays outside the guard. Pinning this keeps
// the boundary deliberate: widening it silently would change what hosts
// may safely mount next to their API prefix.
func TestBFFGuardPrefixIsSegmentScoped(t *testing.T) {
	app := newBFFGuardApp(t, framework.WithAPIPrefix("/api"))
	for _, path := range []string{"/api", "/api/", "/api/x"} {
		app.Router().Get(path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	app.Router().Get("/apifoo", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	inGuard := []string{"/api", "/api/", "/api/x"}
	for _, path := range inGuard {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s under the API prefix: untrusted Origin status = %d, want 403", path, rec.Code)
		}
	}

	// Outside the guard by the segment boundary: /apifoo is not /api/…,
	// the guard passes it through untouched (documented scope).
	req := httptest.NewRequest(http.MethodGet, "/apifoo", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("/apifoo shares leading characters but not the segment boundary: status = %d, want 204 (outside the API prefix subtree)", rec.Code)
	}
}
