package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// AccountStore + /auth/accounts + /auth/unlink/:provider.
//
// The reviews flagged that the 409 collision message tells users to
// "link from settings" but no such endpoint exists. This pins the
// minimum viable version: list linked OAuth accounts, unlink one,
// refuse to unlink the last credential.

// linkingStore is a UserStore + OAuthLinker + AccountLister + AccountUnlinker.
type linkingStore struct {
	mu    sync.Mutex
	users map[string]User
	byID  map[string]User
	links map[string]map[string]string // userID → provider → providerID
}

func newLinkingStore() *linkingStore {
	return &linkingStore{
		users: map[string]User{},
		byID:  map[string]User{},
		links: map[string]map[string]string{},
	}
}

func (s *linkingStore) FindByEmail(_ context.Context, email string) (User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[email]; ok {
		return u, "h", nil
	}
	return nil, "", ErrUserNotFound
}
func (s *linkingStore) FindByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}
func (s *linkingStore) CreateUser(_ context.Context, email, _ string, roles []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "u-" + email
	u := &BasicUser{ID: id, Email: email, Roles: roles}
	s.users[email] = u
	s.byID[id] = u
	return u, nil
}

func (s *linkingStore) FindByOAuth(_ context.Context, provider, providerID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for uid, m := range s.links {
		if m[provider] == providerID {
			return s.byID[uid], nil
		}
	}
	return nil, ErrUserNotFound
}
func (s *linkingStore) LinkOAuth(_ context.Context, userID, provider, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.links[userID] == nil {
		s.links[userID] = map[string]string{}
	}
	s.links[userID][provider] = providerID
	return nil
}
func (s *linkingStore) ListAccounts(_ context.Context, userID string) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Account{}
	for prov, pid := range s.links[userID] {
		out = append(out, Account{Provider: prov, ProviderID: pid})
	}
	return out, nil
}
func (s *linkingStore) UnlinkOAuth(_ context.Context, userID, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.links[userID]; m != nil {
		delete(m, provider)
	}
	return nil
}

func setupAccountsTest(t *testing.T) (*AuthManager, *linkingStore, *router.Router, string) {
	t.Helper()
	store := newLinkingStore()
	user, _ := store.CreateUser(context.Background(), "alice@example.com", "", []string{"user"})
	_ = store.LinkOAuth(context.Background(), user.GetID(), "google", "g-1")
	_ = store.LinkOAuth(context.Background(), user.GetID(), "github", "gh-1")

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	sess, err := mgr.SessionStore().Create(context.Background(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return mgr, store, r, sess.Token
}

func TestAccounts_List(t *testing.T) {
	_, _, r, sess := setupAccountsTest(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/accounts", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Accounts []Account `json:"accounts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Accounts) != 2 {
		t.Fatalf("expected 2 linked accounts, got %d", len(resp.Accounts))
	}
}

func TestAccounts_Unlink(t *testing.T) {
	_, store, r, sess := setupAccountsTest(t)

	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/github", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unlink: %d (body=%s)", w.Code, w.Body.String())
	}

	accts, _ := store.ListAccounts(context.Background(), "u-alice@example.com")
	if len(accts) != 1 || accts[0].Provider != "google" {
		t.Fatalf("expected only google linked after unlink github; got %+v", accts)
	}
}

// linkingStoreWithPassword extends linkingStore with PasswordChecker so we
// can drive the HasPassword-aware unlink path.
type linkingStoreWithPassword struct {
	*linkingStore
	hasPassword map[string]bool
}

func newLinkingStoreWithPassword() *linkingStoreWithPassword {
	return &linkingStoreWithPassword{
		linkingStore: newLinkingStore(),
		hasPassword:  map[string]bool{},
	}
}

func (s *linkingStoreWithPassword) HasPassword(_ context.Context, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasPassword[userID], nil
}

func setupAccountsTestWithPassword(t *testing.T, hasPassword bool, providers ...string) (*linkingStoreWithPassword, *router.Router, string) {
	t.Helper()
	store := newLinkingStoreWithPassword()
	user, _ := store.CreateUser(context.Background(), "alice@example.com", "", []string{"user"})
	for i, prov := range providers {
		_ = store.LinkOAuth(context.Background(), user.GetID(), prov, prov+"-id-"+string(rune('1'+i)))
	}
	store.hasPassword[user.GetID()] = hasPassword

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sess, err := mgr.SessionStore().Create(context.Background(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return store, r, sess.Token
}

// TestAccounts_Unlink_OAuthOnlyUserCannotUnlinkLast pins the new HasPassword
// rule: a user whose only credential is a single OAuth link (no password,
// placeholder hash) cannot unlink that link. Pre-HasPassword the system
// also refused this (links-only rule = 0 remaining → refuse), so this is
// regression coverage for the equivalent path through PasswordChecker.
func TestAccounts_Unlink_OAuthOnlyUserCannotUnlinkLast(t *testing.T) {
	_, r, sess := setupAccountsTestWithPassword(t, false, "google")

	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/google", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("OAuth-only user must not unlink last provider; got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

// TestAccounts_Unlink_PasswordUserCanUnlinkAll pins the new rule: a user
// who has both a real password AND OAuth links can unlink down to zero
// OAuth without locking themselves out — they still have password login.
// This is the case the old links-only rule got wrong.
func TestAccounts_Unlink_PasswordUserCanUnlinkAll(t *testing.T) {
	_, r, sess := setupAccountsTestWithPassword(t, true, "google")

	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/google", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("password user with one OAuth must be allowed to unlink it; got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

// TestAccounts_Unlink_OAuthOnlyMultipleLinks_StillCanReduceToOne pins the
// halfway case: an OAuth-only user with two providers can unlink one of
// them (still has another way to log in). Without HasPassword the
// links-only rule already allowed this; HasPassword=false must not
// regress to disallow.
func TestAccounts_Unlink_OAuthOnlyMultipleLinks_StillCanReduceToOne(t *testing.T) {
	_, r, sess := setupAccountsTestWithPassword(t, false, "google", "github")

	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/github", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("OAuth-only user with 2 providers must be allowed to unlink down to 1; got %d (body=%s)",
			w.Code, w.Body.String())
	}
}

func TestAccounts_Unlink_RefusesLast(t *testing.T) {
	_, _, r, sess := setupAccountsTest(t)

	// Unlink one (still have google).
	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/github", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first unlink: %d", w.Code)
	}

	// Now unlink the last one — must refuse to avoid locking the user out.
	req2 := httptest.NewRequest(http.MethodDelete, "/auth/unlink/google", nil)
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 refusing to unlink the last linked account; got %d (body=%s)",
			w2.Code, w2.Body.String())
	}
}
