package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Cross-cutting tests: verify auth handlers don't OOM on hostile input by
// enforcing a body size limit on every JSON-decoding endpoint.
//
// Without the limit, a single request with a 100MB body field is buffered
// into RAM by encoding/json. With the limit, the decoder returns an error
// and the handler responds 4xx (typically 400 or 413).

const oversizedFieldBytes = 5 * 1024 * 1024 // 5 MB — well over the 1 MB cap

func makeOversizedBody(field string) []byte {
	huge := strings.Repeat("A", oversizedFieldBytes)
	body, _ := json.Marshal(map[string]string{field: huge})
	return body
}

func TestHandlerBodyLimit_MagicLinkSend(t *testing.T) {
	mgr, _, _ := newMagicLinkManager(t, &mockEmailSender{})
	r := mountMagicLinkRoutes(mgr)

	body := makeOversizedBody("email")
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerBodyLimit_TwoFAVerify(t *testing.T) {
	mgr := newTestAuthManagerWithSession(t)
	r := router.New()
	mgr.RegisterRoutes(r)

	// Need a valid session so we get past the auth check before hitting the decoder.
	sess, err := mgr.SessionStore().Create(context.Background(), "user-1", time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	body := makeOversizedBody("code")
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerBodyLimit_TwoFAChallenge(t *testing.T) {
	mgr := newTestAuthManagerWithSession(t)
	r := router.New()
	mgr.RegisterRoutes(r)

	sess, err := mgr.SessionStore().Create(context.Background(), "user-1", time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	body := makeOversizedBody("code")
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerBodyLimit_Login(t *testing.T) {
	mgr := newTestAuthManagerWithSession(t)
	r := router.New()
	mgr.RegisterRoutes(r)

	body := makeOversizedBody("email")
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerBodyLimit_Register(t *testing.T) {
	mgr := newTestAuthManagerWithSession(t)
	r := router.New()
	mgr.RegisterRoutes(r)

	body := makeOversizedBody("email")
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

// newTestAuthManagerWithSession builds an AuthManager with the core +
// twofa plugins and an in-memory user store, ready for handler tests.
func newTestAuthManagerWithSession(t *testing.T) *AuthManager {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr
}
