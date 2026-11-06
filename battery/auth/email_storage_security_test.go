package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestEmailRefusesUnstorableText(t *testing.T) {
	for _, invalid := range []string{"bad\x00@example.com", "bad\xff@example.com", "bad\xe2\x82@example.com", "bad\xff\u200b@example.com"} {
		for _, override := range []func(string) (string, error){nil, func(string) (string, error) { return "safe@example.com", nil }} {
			manager := New(AuthConfig{CanonicalizeEmail: override})
			for _, canonicalize := range []func(string) (string, error){func(raw string) (string, error) { return canonicalizeEmailWith(override, raw) }, manager.canonicalizeEmail} {
				if _, err := canonicalize(invalid); err == nil || err.Error() != reasonInvalidEmail.Format(nil) {
					t.Errorf("canonicalization accepts unstorable %q or loses invalid_email: %v", invalid, err)
				}
			}
		}
		manager := New(AuthConfig{CanonicalizeEmail: func(string) (string, error) { return invalid, nil }})
		if _, err := manager.canonicalizeEmail("safe@example.com"); err == nil {
			t.Errorf("override emitted unstorable %q", invalid)
		}
	}
}

func TestEmailRoutesRefuseBadText(t *testing.T) {
	fixture := auditHarness(t)
	for _, path := range []string{"/auth/register", "/auth/login"} {
		for _, invalid := range []string{"bad\x00@example.com", "bad\xff\u200b@example.com", "bad\xe2\x82@example.com"} {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"email": {invalid}, "password": {"supersecret1"}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			response := httptest.NewRecorder()
			fixture.router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_email") {
				t.Errorf("%s accepts %q: %d %s", path, invalid, response.Code, response.Body)
			}
		}
	}
	if len(fixture.store.users) != 0 {
		t.Error("invalid registration reached user storage")
	}
	magicSender := &mockEmailSender{}
	magicManager, _, magicPlugin := newMagicLinkManager(t, magicSender)
	resetManager := New(AuthConfig{UserStore: newUserStoreWithPassword(), DevMode: true})
	resetPlugin := NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: &stubEmailSender{}})
	resetManager.Use(NewCorePlugin())
	resetManager.Use(resetPlugin)
	if err := resetManager.Init(nil); err != nil {
		t.Fatal(err)
	}
	resetRoutes := router.New()
	resetManager.RegisterRoutes(resetRoutes)
	for _, endpoint := range []struct {
		path   string
		routes http.Handler
	}{
		{"/auth/magic-link/send", mountMagicLinkRoutes(magicManager)},
		{"/auth/forgot-password", resetRoutes},
	} {
		request := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(`{"email":"bad\u0000@example.com"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		endpoint.routes.ServeHTTP(response, request)
		var refusal struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &refusal); err != nil || response.Code != http.StatusBadRequest || refusal.Reason != "invalid_email" {
			t.Errorf("%s answered %d %s: %v", endpoint.path, response.Code, response.Body, err)
		}
	}
	if len(magicPlugin.tokenStore.(*MemoryMagicLinkTokenStore).tokens) != 0 || len(resetPlugin.store.(*MemoryMagicLinkTokenStore).tokens) != 0 {
		t.Error("invalid email minted a token")
	}
}
