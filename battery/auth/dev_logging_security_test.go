package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func captureDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestMagicLink_DevModeDoesNotLogLiveURL(t *testing.T) {
	buf := captureDefaultLogger(t)
	mgr, _, _ := newMagicLinkManager(t, nil)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("magic-link send failed: %d", rec.Code)
	}

	logs := buf.String()
	if strings.Contains(logs, "token=") || strings.Contains(logs, "http://localhost:8080") {
		t.Fatalf("SECURITY: [auth-log] magic-link dev mode logged live takeover URL: %q", logs)
	}
}

func TestPasswordReset_DevModeDoesNotLogLiveURL(t *testing.T) {
	buf := captureDefaultLogger(t)
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
	user := &BasicUser{ID: "u-reset", Email: "reset@example.com", Roles: []string{"user"}}
	store.users[user.Email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[user.Email]

	r := router.New()
	mgr.RegisterRoutes(r)
	body, _ := json.Marshal(map[string]string{"email": user.Email})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("forgot-password failed: %d", rec.Code)
	}

	logs := buf.String()
	if strings.Contains(logs, "token=") || strings.Contains(logs, "http://localhost/auth/reset-password") {
		t.Fatalf("SECURITY: [auth-log] password-reset dev mode logged live takeover URL: %q", logs)
	}
}

func TestEmailVerification_DevModeDoesNotLogLiveURL(t *testing.T) {
	buf := captureDefaultLogger(t)
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, _ := HashPassword("pwlong123")
	user := &BasicUser{ID: "u-verify", Email: "verify@example.com", Roles: []string{"user"}}
	store.users[user.Email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[user.Email]

	r := router.New()
	mgr.RegisterRoutes(r)

	loginBody, _ := json.Marshal(map[string]string{"email": user.Email, "password": "pwlong123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	r.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}

	sendReq := httptest.NewRequest(http.MethodPost, "/auth/send-verification", nil)
	for _, c := range loginRec.Result().Cookies() {
		sendReq.AddCookie(c)
	}
	sendRec := httptest.NewRecorder()
	r.ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusOK {
		t.Fatalf("send-verification failed: %d %s", sendRec.Code, sendRec.Body.String())
	}

	logs := buf.String()
	if strings.Contains(logs, "token=") || strings.Contains(logs, "http://localhost/auth/verify-email") {
		t.Fatalf("SECURITY: [auth-log] email-verification dev mode logged live takeover URL: %q", logs)
	}
}

var _ = context.Background
