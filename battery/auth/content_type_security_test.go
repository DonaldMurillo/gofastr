package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestLogin_RejectsTextPlainJSON(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@example.com", "password123")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/login accepted JSON body with text/plain content type (%d). Attack: content-type smuggling into login.", rec.Code)
	}
}

func TestRegister_RejectsTextPlainJSON(t *testing.T) {
	mgr, _ := newTestManager(t)
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "new@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/register accepted JSON body with text/plain content type (%d). Attack: content-type smuggling into anonymous registration.", rec.Code)
	}
}

func TestLogin_RejectsMissingContentType(t *testing.T) {
	mgr, store := newTestManager(t)
	seedUser(t, store, "alice@example.com", "password123")
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/login accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance at login.", rec.Code)
	}
}

func TestRegister_RejectsMissingContentType(t *testing.T) {
	mgr, _ := newTestManager(t)
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "new2@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/register accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance at anonymous registration.", rec.Code)
	}
}

func TestMagicLinkSend_RejectsTextPlainJSON(t *testing.T) {
	mgr, _, _ := newMagicLinkManager(t, nil)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/magic-link/send accepted JSON body with text/plain content type (%d). Attack: content-type smuggling into magic-link delivery.", rec.Code)
	}
}

func TestMagicLinkSend_RejectsMissingContentType(t *testing.T) {
	mgr, _, _ := newMagicLinkManager(t, nil)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/magic-link/send accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance into magic-link delivery.", rec.Code)
	}
}

func TestForgotPassword_RejectsTextPlainJSON(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-forgot", Email: "forgot@example.com", Roles: []string{"user"}}
	store.users[user.Email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[user.Email]

	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": user.Email})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/forgot-password accepted JSON body with text/plain content type (%d). Attack: content-type smuggling into reset-token issuance.", rec.Code)
	}
}

func TestForgotPassword_RejectsMissingContentType(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-forgot-2", Email: "forgot2@example.com", Roles: []string{"user"}}
	store.users[user.Email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[user.Email]

	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": user.Email})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SECURITY: [auth-content-type] /auth/forgot-password accepted JSON body without Content-Type (%d). Attack: ambiguous parser acceptance into reset-token issuance.", rec.Code)
	}
}
