package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
