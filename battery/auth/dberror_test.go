package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// flakyUserStore returns a transient error from FindByEmail/FindByID.
// Used to verify handlers don't conflate "transient DB error" with
// "user does not exist" — the auto-create path must NOT fire on a
// generic error.
type flakyUserStore struct {
	err          error
	createCalled bool
}

func (s *flakyUserStore) FindByEmail(_ context.Context, _ string) (User, string, error) {
	return nil, "", s.err
}
func (s *flakyUserStore) FindByID(_ context.Context, _ string) (User, error) {
	return nil, s.err
}
func (s *flakyUserStore) CreateUser(_ context.Context, email, _ string, _ []string) (User, error) {
	s.createCalled = true
	// Returning a benign success would let the handler complete the flow
	// (set cookie, redirect). The TEST asserts createCalled stayed false.
	return &BasicUser{ID: "auto-created", Email: email, Roles: []string{"user"}}, nil
}

// TestMagicLinkVerify_DBErrorDoesNotAutoCreate confirms a transient
// FindByEmail error does NOT silently auto-create a user. CreateUser
// returns a sentinel error that fails the request loudly.
func TestMagicLinkVerify_DBErrorDoesNotAutoCreate(t *testing.T) {
	store := &flakyUserStore{err: errors.New("connection refused")}
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:  "http://localhost",
		TokenTTL: 15 * time.Minute,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// Create a valid token so we get past the redeem step.
	token, err := plugin.tokenStore.CreateToken(context.Background(), "victim@example.com", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// CreateUser must NOT have been called: a transient DB error during
	// FindByEmail is not the same as "user does not exist".
	if store.createCalled {
		t.Fatalf("CreateUser should not be invoked on a non-NotFound FindByEmail error")
	}
	// And the response must surface the failure rather than redirect.
	if w.Code < 500 {
		t.Fatalf("expected 5xx on DB error, got %d (body=%s, location=%q)",
			w.Code, w.Body.String(), w.Header().Get("Location"))
	}
}

// TestOAuth2Callback_DBErrorDoesNotAutoCreate is the same shape for OAuth2.
func TestOAuth2Callback_DBErrorDoesNotAutoCreate(t *testing.T) {
	store := &flakyUserStore{err: errors.New("connection refused")}
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{
			"stub": &stubOAuthProvider{
				name:     "stub",
				userInfo: &OAuth2UserInfo{ID: "ext-1", Email: "victim@example.com", Provider: "stub"},
			},
		},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// Generate a state via the plugin to get past CSRF check.
	state, err := plugin.generateState("stub")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/callback?state="+state+"&code=fakecode", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if store.createCalled {
		t.Fatalf("CreateUser should not be invoked on a non-NotFound FindByEmail error")
	}
	if w.Code < 500 {
		t.Fatalf("expected 5xx on DB error, got %d (body=%s, location=%q)",
			w.Code, w.Body.String(), w.Header().Get("Location"))
	}
}

// stubOAuthProvider is a deterministic OAuth2 provider used in tests.
// ExchangeCode and FetchUserInfo never hit the network.
type stubOAuthProvider struct {
	name        string
	userInfo    *OAuth2UserInfo
	exchangeErr error
}

func (p *stubOAuthProvider) Name() string                { return p.name }
func (p *stubOAuthProvider) AuthURL(state string) string { return "https://example.com?state=" + state }
func (p *stubOAuthProvider) ExchangeCode(_ context.Context, _ string) (*OAuth2Token, error) {
	if p.exchangeErr != nil {
		return nil, p.exchangeErr
	}
	return &OAuth2Token{AccessToken: "tok", Expiry: time.Now().Add(time.Hour)}, nil
}
func (p *stubOAuthProvider) FetchUserInfo(_ context.Context, _ string) (*OAuth2UserInfo, error) {
	return p.userInfo, nil
}
