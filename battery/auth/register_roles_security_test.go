package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestRegister_IgnoresClientSuppliedRoles_JSON pins the security
// contract: anyone POSTing roles:["admin"] in the JSON body MUST NOT
// be created as an admin. The /auth/register endpoint is anonymous —
// honoring client roles trivially self-promotes anyone to any role.
func TestRegister_IgnoresClientSuppliedRoles_JSON(t *testing.T) {
	mgr, store := newTestManager(t)
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]any{
		"email":    "attacker@example.com",
		"password": "password123",
		"roles":    []string{"admin", "owner", "superuser"},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	u, _, err := store.FindByEmail(req.Context(), "attacker@example.com")
	if err != nil {
		t.Fatalf("created user not found: %v", err)
	}
	for _, role := range u.GetRoles() {
		if role == "admin" || role == "owner" || role == "superuser" {
			t.Fatalf("PRIVILEGE ESCALATION: register honored client-supplied role %q. user.Roles=%v", role, u.GetRoles())
		}
	}
}

// TestRegister_IgnoresClientSuppliedRoles_Form pins the same contract
// for the form-encoded register path — CSRF-style cross-origin POSTs
// can target this with <form><input name=roles value=admin>.
func TestRegister_IgnoresClientSuppliedRoles_Form(t *testing.T) {
	mgr, store := newTestManager(t)
	r := mountRoutes(mgr)

	form := url.Values{}
	form.Set("email", "csrf-victim@example.com")
	form.Set("password", "password123")
	form.Add("roles", "admin")
	form.Add("roles", "owner")
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther && w.Code != http.StatusCreated {
		t.Fatalf("expected 303 or 201, got %d: %s", w.Code, w.Body.String())
	}
	u, _, err := store.FindByEmail(req.Context(), "csrf-victim@example.com")
	if err != nil {
		t.Fatalf("created user not found: %v", err)
	}
	for _, role := range u.GetRoles() {
		if role == "admin" || role == "owner" {
			t.Fatalf("PRIVILEGE ESCALATION via form: register honored client-supplied role %q. user.Roles=%v", role, u.GetRoles())
		}
	}
}

// newTestManagerWithRoles builds a CorePlugin-backed manager whose
// DefaultRoles are operator-configured (the default newTestManager
// leaves them empty, exercising the ["user"] fallback).
func newTestManagerWithRoles(t *testing.T, roles []string) (*AuthManager, *memoryUserStore) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:    "test-secret",
		JWTExpiry:    time.Hour,
		SessionTTL:   24 * time.Hour,
		UserStore:    userStore,
		DefaultRoles: roles,
		DevMode:      true,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, userStore
}

// TestRegisterUsesConfiguredRoles pins that an operator-configured
// DefaultRoles is what lands on the created account — the hardcoded
// ["user"] is gone in favour of AuthManager.DefaultRoles().
func TestRegisterUsesConfiguredRoles(t *testing.T) {
	mgr, store := newTestManagerWithRoles(t, []string{"member", "editor"})
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	u, _, err := store.FindByEmail(req.Context(), "alice@example.com")
	if err != nil {
		t.Fatalf("created user not found: %v", err)
	}
	got := u.GetRoles()
	if len(got) != 2 || got[0] != "member" || got[1] != "editor" {
		t.Fatalf("expected [member editor], got %v", got)
	}
}

// TestDefaultRolesIgnoreClientInput pins that even with DefaultRoles
// configured, a client-supplied roles key is ignored — configured
// roles win, client input never reaches the created account.
func TestDefaultRolesIgnoreClientInput(t *testing.T) {
	mgr, store := newTestManagerWithRoles(t, []string{"member"})
	r := mountRoutes(mgr)

	body, _ := json.Marshal(map[string]any{
		"email":    "attacker@example.com",
		"password": "password123",
		"roles":    []string{"admin", "superuser"},
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	u, _, err := store.FindByEmail(req.Context(), "attacker@example.com")
	if err != nil {
		t.Fatalf("created user not found: %v", err)
	}
	for _, role := range u.GetRoles() {
		if role == "admin" || role == "superuser" {
			t.Fatalf("PRIVILEGE ESCALATION: client role %q honored. user.Roles=%v", role, u.GetRoles())
		}
	}
	if len(u.GetRoles()) != 1 || u.GetRoles()[0] != "member" {
		t.Fatalf("expected configured [member], got %v", u.GetRoles())
	}
}
