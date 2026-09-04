package auth

// The auth audit funnel's coverage of identity mutations.
//
// Property: security-relevant identity mutations — the email verified-bit
// flip, an OAuth credential removal, a role change — emit an audit event
// through AuthManager.emitSecurity, so the trail can answer "who did this,
// when". The funnel is nil-safe and panic-isolated (audit_test.go pins
// that); these tests pin that the closed kind vocabulary actually covers
// the mutations an incident investigation needs.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sinkHasEvent reports whether the sink captured an event of the wanted kind.
func sinkHasEvent(c *capturingSink, kind string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// sinkEventKinds lists the captured kinds, for honest failure output.
func sinkEventKinds(c *capturingSink) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	kinds := make([]string, 0, len(c.events))
	for _, ev := range c.events {
		kinds = append(kinds, ev.Kind)
	}
	return strings.Join(kinds, ", ")
}

func TestVerifyEmailAuditsMutation(t *testing.T) {
	store := newUserStoreWithPassword()
	sink := &capturingSink{}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
		AuditSink:           sink,
	})
	mgr.Use(NewCorePlugin())
	sender := &stubEmailSender{}
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL:     "http://localhost",
		EmailSender: sender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	store.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["alice@example.com"]
	r := mountRoutes(mgr)

	session := loginP17(t, r)
	req := httptest.NewRequest(http.MethodPost, "/auth/send-verification", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("send-verification: %d (body=%s)", w.Code, w.Body.String())
	}
	_, body := sender.snapshot()
	tok := extractTokenFromBody(body)
	if tok == "" {
		t.Fatalf("no token in verification email body: %q", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+tok, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify-email: %d (body=%s)", w.Code, w.Body.String())
	}
	if !store.verifiedIDs[user.ID] {
		t.Fatalf("mutation did not land: user not marked verified")
	}

	if !sinkHasEvent(sink, "email.verified") {
		t.Errorf("SECURITY: [audit-gap] a successful email verification (MarkEmailVerified on %s) emitted no email.verified security event; the trust-bit flip gates OAuth account binding and must be visible to the trail (events captured: [%s])", user.ID, sinkEventKinds(sink))
	}
}

func TestOauthUnlinkAuditsMutation(t *testing.T) {
	store := newMemoryUserStore()
	sink := &capturingSink{}
	ctx := context.Background()

	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := store.CreateUser(ctx, "alice@example.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, l := range [][2]string{{"google", "g-1"}, {"github", "gh-1"}} {
		if err := store.LinkOAuth(ctx, user.GetID(), l[0], l[1]); err != nil {
			t.Fatalf("LinkOAuth %s: %v", l[0], err)
		}
	}

	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           store,
		AuditSink:           sink,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewAccountsPlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sess, err := mgr.SessionStore().Create(ctx, user.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	r := mountRoutes(mgr)

	req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/google", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unlink: %d (body=%s)", w.Code, w.Body.String())
	}
	accts, err := store.ListAccounts(ctx, user.GetID())
	if err != nil || len(accts) != 1 {
		t.Fatalf("mutation did not land: accounts after unlink = %v (err %v)", accts, err)
	}

	if !sinkHasEvent(sink, "oauth.unlinked") {
		t.Errorf("SECURITY: [audit-gap] a successful OAuth unlink of google from %s emitted no oauth.unlinked security event; credential removal is exactly the incident shape the trail exists for, and its counterpart oauth.linked is recorded (events captured: [%s])", user.GetID(), sinkEventKinds(sink))
	}
}

func TestSetUserRolesAuditsMutation(t *testing.T) {
	store := newMemoryUserStore()
	sink := &capturingSink{}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		UserStore:           store,
		AuditSink:           sink,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	user := seedUser(t, store, "alice@example.com", "password123")

	if err := mgr.SetUserRoles(context.Background(), user.GetID(), []string{"admin"}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}
	after, err := store.FindByID(context.Background(), user.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(after.GetRoles()) != 1 || after.GetRoles()[0] != "admin" {
		t.Fatalf("mutation did not land: roles after SetUserRoles = %v", after.GetRoles())
	}

	if !sinkHasEvent(sink, "roles.updated") {
		t.Errorf("SECURITY: [audit-gap] SetUserRoles promoted %s to [admin] and emitted no roles.updated security event; privilege changes driven through the documented server-side seam must leave the trail the admin back-office writes (events captured: [%s])", user.GetID(), sinkEventKinds(sink))
	}
}
