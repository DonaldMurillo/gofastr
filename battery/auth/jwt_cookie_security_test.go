package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newProdTestManager(t *testing.T) (*AuthManager, *memoryUserStore) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:  "test-secret",
		JWTExpiry:  time.Hour,
		SessionTTL: 24 * time.Hour,
		UserStore:  userStore,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, userStore
}

func TestJWTRejectsFutureIssuedAt(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret-key", time.Hour)
	token, err := encodeToken(jwtAuth.Secret, jwtAuth.Issuer, Claims{
		UserID:    "user-1",
		Email:     "alice@example.com",
		Roles:     []string{"user"},
		IssuedAt:  time.Now().Add(2 * time.Hour),
		ExpiresAt: time.Now().Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("encodeToken: %v", err)
	}

	if _, err := jwtAuth.ValidateToken(token); err == nil {
		t.Fatal("SECURITY: [auth-jwt] JWT with future iat was accepted. Attack: forged tokens can be minted with invalid issuance time.")
	}
}

func TestJWTRejectsEmptySubject(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret-key", time.Hour)
	token, err := encodeToken(jwtAuth.Secret, jwtAuth.Issuer, Claims{
		UserID:    "",
		Email:     "alice@example.com",
		Roles:     []string{"admin"},
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("encodeToken: %v", err)
	}

	if _, err := jwtAuth.ValidateToken(token); err == nil {
		t.Fatal("SECURITY: [auth-jwt] JWT with empty subject was accepted. Attack: anonymous or malformed tokens can flow into authz paths.")
	}
}

func TestCorePlugin_LoginCookieUsesStrictSameSite(t *testing.T) {
	mgr, store := newProdTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Fatalf("SECURITY: [auth-cookie] login session cookie not marked SameSite=Strict: %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestCorePlugin_LogoutCookieUsesStrictSameSite(t *testing.T) {
	mgr, store := newProdTestManager(t)
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)

	loginBody, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	r.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	for _, c := range loginRec.Result().Cookies() {
		logoutReq.AddCookie(c)
	}
	logoutRec := httptest.NewRecorder()
	r.ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout failed: %d %s", logoutRec.Code, logoutRec.Body.String())
	}
	if !strings.Contains(logoutRec.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Fatalf("SECURITY: [auth-cookie] logout cookie clear not marked SameSite=Strict: %q", logoutRec.Header().Get("Set-Cookie"))
	}
}
