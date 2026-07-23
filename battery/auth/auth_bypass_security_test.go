package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAuthBypass_ExpiredJWTRejected verifies that expired JWT tokens are
// rejected. Attack: replaying an expired token to gain access.
// Depends on unexported: encodeToken (battery/auth/token.go).
func TestAuthBypass_ExpiredJWTRejected(t *testing.T) {
	secret := "test-secret-key"
	jwtAuth := NewJWTAuth(secret, -1*time.Second) // already expired

	user := &BasicUser{ID: "user-1", Email: "alice@example.com", Roles: []string{"user"}}
	token, err := jwtAuth.GenerateToken(user)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	_, err = jwtAuth.ValidateToken(token)
	if err == nil {
		t.Errorf("SECURITY: [auth_bypass] expired JWT was accepted. Attack: replaying expired token for unauthorized access.")
	}
}

// TestAuthBypass_SessionFixationViaCookieInjection verifies that an
// attacker cannot set a known session cookie before authentication and
// have it be valid after the victim authenticates.
// Attack: session fixation via cookie injection.
func TestAuthBypass_SessionFixationViaCookieInjection(t *testing.T) {
	mgr, store := newTestManager(t)
	r := mountRoutes(mgr)

	seedUser(t, store, "victim@example.com", "password123")

	// Attacker sets a pre-known session cookie
	attackerCookie := "attacker-known-session-id"
	cookieName := mgr.Config().SessionCookie

	// Login with the attacker's cookie already set
	body, _ := json.Marshal(map[string]string{
		"email":    "victim@example.com",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: attackerCookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}

	// Verify the response sets a NEW session cookie (not the attacker's).
	var minted string
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			minted = c.Value
			break
		}
	}
	if minted == "" {
		t.Fatalf("login did not mint the configured %q session cookie", cookieName)
	}
	if minted == attackerCookie {
		t.Fatal("SECURITY: [auth_bypass] login accepted the attacker-provided session token without rotation")
	}
}

// TestAuthBypass_DefaultBruteForceLimiter verifies that repeated failed login
// attempts trigger the default per-account limiter even when no explicit
// LoginRateLimit is configured.
func TestAuthBypass_DefaultBruteForceLimiter(t *testing.T) {
	mgr, store := newTestManager(t)
	r := mountRoutes(mgr)

	seedUser(t, store, "target@example.com", "correctpassword")

	// Attempt 20 failed logins
	successCount := 0
	for i := 0; i < 20; i++ {
		body, _ := json.Marshal(map[string]string{
			"email":    "target@example.com",
			"password": fmt.Sprintf("wrong-password-%d", i),
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytesReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			successCount++
		}
	}

	if successCount > 0 {
		t.Errorf("SECURITY: [auth_bypass] %d/20 brute-force attempts succeeded. Attack: credential stuffing without rate limiting.", successCount)
	}

	// After 20 rapid failures the account MUST be rate-limited, even
	// against the correct password. The production AuthConfig defaults
	// install a per-account login limiter so even an attacker who
	// rotates IPs is throttled on the email key.
	body, _ := json.Marshal(map[string]string{
		"email":    "target@example.com",
		"password": "correctpassword",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("SECURITY: [auth_bypass] correct password accepted after 20 failed attempts (status %d, want 429). Attack: no lockout / brute-force throttle.", w.Code)
	}
}

// TestAuthBypass_AlgNoneJWTRejected verifies that a JWT with algorithm
// "none" (or "None", "NONE") is rejected. Attack: CWE-327 — forging
// a JWT by setting the algorithm header to "none" to bypass signature
// verification.
func TestAuthBypass_AlgNoneJWTRejected(t *testing.T) {
	secret := "test-secret-key"
	jwtAuth := NewJWTAuth(secret, 1*time.Hour)

	// Craft a token with alg:none
	header := map[string]string{"alg": "none", "typ": "JWT"}
	payload := map[string]any{
		"sub":   "user-1",
		"email": "attacker@example.com",
		"roles": []string{"admin"},
		"iss":   "gofastr",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}

	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	// Token with empty signature (alg:none)
	forgedToken := headerB64 + "." + payloadB64 + "."

	_, err := jwtAuth.ValidateToken(forgedToken)
	if err == nil {
		t.Errorf("SECURITY: [auth_bypass] alg:none JWT was accepted (CWE-327). Attack: forging JWT by setting algorithm to 'none' bypasses signature verification entirely.")
	}
}

// bytesReader creates a strings.Reader from a byte slice.
func bytesReader(b []byte) *strings.Reader {
	return strings.NewReader(string(b))
}
