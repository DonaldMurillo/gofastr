package auth

// Strict credential-field decoding at the auth battery's two body
// surfaces (decodeAuthCredentials: the JSON path via decodeJSONLimited,
// the HTML-form path via ParseForm).
//
// Property: one credential body resolves each field to exactly ONE
// unambiguous value — a body naming a field twice (or via a case-folded
// variant of its name) is rejected at decode time, not resolved by
// parser accident. The framework enforces this for every handler.Bind
// consumer (core/handler/bind.go validateBodyKeys rejects duplicate and
// case-folded top-level keys); decodeAuthCredentials is a hand-rolled
// decoder doing the same job on the credential surface and must obey the
// same rule. The two default resolutions also DISAGREE with each other —
// encoding/json keeps the LAST duplicate and matches key names
// case-insensitively, net/url keeps the FIRST — which is exactly why the
// ambiguity must be an error rather than a silent pick: the same smuggled
// body authenticates a different account depending on Content-Type.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func setupStrictDecodeRoute(t *testing.T) *router.Router {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, userStore, "alice@example.com", "hunter22")
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

func postLogin(t *testing.T, r *router.Router, contentType, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// JSON surface: duplicate and case-folded top-level credential keys must
// be a 400 decode failure. Attack shape: a body that names two emails (or
// EMAIL vs email) resolves differently across parsers/proxies — whatever
// wins, the request authenticated as a value the operator never saw as
// "the" email.
func TestLoginJSONStrictTopLevelKeys(t *testing.T) {
	r := setupStrictDecodeRoute(t)

	cases := []struct {
		name string
		body string
	}{
		{"happy single exact keys", `{"email":"alice@example.com","password":"hunter22"}`},
		{"duplicate email key", `{"email":"alice@example.com","email":"alice@example.com","password":"hunter22"}`},
		{"duplicate password key", `{"email":"alice@example.com","password":"hunter22","password":"hunter22"}`},
		{"case-folded EMAIL key", `{"EMAIL":"alice@example.com","password":"hunter22"}`},
		{"case-folded PASSword key", `{"email":"alice@example.com","PASSword":"hunter22"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := postLogin(t, r, "application/json", tc.body)
			if tc.name == "happy single exact keys" {
				if code != http.StatusOK {
					t.Fatalf("control: single-key login must succeed, got %d", code)
				}
				return
			}
			if code != http.StatusBadRequest {
				t.Errorf("SECURITY: [auth-strict-json] %s accepted (status %d), want 400: an ambiguous credential field is silently resolved (encoding/json keeps the LAST duplicate and matches keys case-insensitively), so one smuggled body can authenticate as whichever value the parser happens to keep — the same body decodes differently per surface", tc.name, code)
			}
		})
	}
}

// Form surface: the same property on application/x-www-form-urlencoded.
// Attack shape: email=alice&…&email=mallory keeps the FIRST value in Go,
// so a proxy/WAF inspecting the last occurrence sees a different identity
// than the handler authenticates.
func TestLoginFormDuplicateFieldsRejected(t *testing.T) {
	r := setupStrictDecodeRoute(t)

	cases := []struct {
		name string
		body string
	}{
		{"happy single fields", "email=alice%40example.com&password=hunter22"},
		{"duplicate email field", "email=alice%40example.com&password=hunter22&email=alice%40example.com"},
		{"duplicate password field", "email=alice%40example.com&password=hunter22&password=hunter22"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := postLogin(t, r, "application/x-www-form-urlencoded", tc.body)
			if tc.name == "happy single fields" {
				if code != http.StatusSeeOther && code != http.StatusOK {
					t.Fatalf("control: single-field form login must succeed, got %d", code)
				}
				return
			}
			if code != http.StatusBadRequest {
				t.Errorf("SECURITY: [auth-strict-form] %s accepted (status %d), want 400: duplicate form fields resolve to the FIRST value while the JSON surface keeps the LAST — the same smuggled body authenticates a different account depending on Content-Type", tc.name, code)
			}
		})
	}
}

// Cross-surface divergence pin: the SAME duplicated pair decodes to
// DIFFERENT identities on the two surfaces today. Proving the divergence
// through decodeAuthCredentials directly shows the fix target (reject at
// decode) rather than picking one winner.
func TestDecodeDuplicateResolvesDifferentlyPerSurface(t *testing.T) {
	const victim = "alice@example.com"
	const mallory = "mallory@example.com"

	formBody := "email=" + victim + "&email=" + mallory
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	emailForm, _, _, okForm := decodeAuthCredentials(httptest.NewRecorder(), req)

	jsonBody := `{"email":"` + victim + `","email":"` + mallory + `"}`
	jreq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte(jsonBody)))
	jreq.Header.Set("Content-Type", "application/json")
	emailJSON, _, _, okJSON := decodeAuthCredentials(httptest.NewRecorder(), jreq)

	if !okForm || !okJSON {
		t.Fatalf("both ambiguous bodies decode ok today: form=%v json=%v (if either now fails, the strict fix landed; update the sibling tests)", okForm, okJSON)
	}
	if emailForm != emailJSON {
		t.Errorf("SECURITY: [auth-strict-decode] one duplicated email pair resolved to %q on the form surface and %q on the JSON surface — the identity depends on Content-Type, not on the operator", emailForm, emailJSON)
	}
}
