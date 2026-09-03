//go:build red

// RED TESTS — open findings, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Family: security-relevant identity mutations emit audit/security events
// through the emitSecurity funnel. The funnel is good — nil-safe,
// panic-isolated, a closed taxonomy (audit.go) covering login/2FA/reset/
// oauth-link/magic-link/token lifecycle — but three identity mutations sit
// entirely outside it.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// redHasEvent reports whether the sink captured an event of the wanted kind.
func redHasEvent(c *capturingSink, kind string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// redEventKinds lists the captured kinds, for honest failure output.
func redEventKinds(c *capturingSink) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	kinds := make([]string, 0, len(c.events))
	for _, ev := range c.events {
		kinds = append(kinds, ev.Kind)
	}
	return strings.Join(kinds, ", ")
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: security-relevant identity mutations emit an audit event through
// AuthManager.emitSecurity (audit.go), so the trail can answer "who verified
// this address, when".
// Surfaces: email_verification.go — the whole file contains ZERO emitSecurity
// calls; verifyHandler:172-197 calls MarkEmailVerified (the trust-bit flip)
// and answers 200 with no event; sendHandler:99-170 mints the token with no
// event either.
// Finding: a successful GET /auth/verify-email flips the user's verified bit
// (the bit the OAuth auto-link decision table keys on — an unverified email
// never binds, auth.go:39-58) and the audit trail stays silent. Every sibling
// flow in the taxonomy records its mutation (register.succeeded,
// password.reset_completed, magiclink.consumed, oauth.linked, 2fa.enrolled);
// email verification is the one trust mutation with no trail, so an operator
// investigating a takeover cannot see when/whether the address was verified.
// Severity: P2 — no exploit, but the mutation gates OAuth account binding and
// is invisible to the security trail the package otherwise maintains.
// Fix direction: emitSecurity an email.verified event from verifyHandler
// (UserID from the redeemed token, Remote from the request) — and an
// email.verification_requested from sendHandler for the issuance half.
func TestVerifyEmailRedAuditsMutation(t *testing.T) {
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

	if !redHasEvent(sink, "email.verified") {
		t.Errorf("SECURITY: [audit-gap] a successful email verification (MarkEmailVerified on %s) emitted no email.verified security event; the trust-bit flip is invisible to the audit trail (events captured: [%s])", user.ID, redEventKinds(sink))
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: security-relevant identity mutations emit an audit event through
// AuthManager.emitSecurity.
// Surfaces: accounts.go:unlinkHandler:141-205 — the link removal at :198
// (UnlinkOAuth) is followed straight by the 200; no emitSecurity anywhere in
// the handler. Its counterpart exists: the OAuth link flow emits
// oauth.linked (oauth2.go), so the taxonomy covers adding a credential but
// not removing one.
// Finding: DELETE /auth/unlink/{provider} removes an OAuth login method —
// a credential-lifecycle mutation on par with the ones the trail records —
// and emits nothing. The unlink record matters for exactly the incident it
// enables: a hijacked session stripping the victim's recovery options.
// Severity: P3 — session-gated self-service (an attacker needs the victim's
// session), so this is a forensic-visibility gap, not an exploit.
// Fix direction: emitSecurity an oauth.unlinked event (UserID, Remote,
// Meta: provider) after a successful UnlinkOAuth, mirroring oauth.linked.
func TestOauthUnlinkRedAuditsMutation(t *testing.T) {
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

	if !redHasEvent(sink, "oauth.unlinked") {
		t.Errorf("SECURITY: [audit-gap] a successful OAuth unlink of google from %s emitted no oauth.unlinked security event; credential removal is invisible to the audit trail while its counterpart oauth.linked is recorded (events captured: [%s])", user.GetID(), redEventKinds(sink))
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: security-relevant identity mutations emit an audit event through
// AuthManager.emitSecurity.
// Surfaces: manager.go:SetUserRoles:434-436 — a bare UpdateRoles passthrough,
// no emitSecurity. The shipped admin surface is covered (battery/admin
// rbac_admin.go handleRBACAssign writes its own audit row after calling
// SetUserRoles); the documented entry point itself
// ("call it from trusted server code", manager.go:430-433) stays silent.
// Finding: a role change driven through AuthManager.SetUserRoles — the
// supported server-side seam — produces no security event from the auth
// funnel. Privilege changes are the canonical audit-trail entry; any caller
// other than the admin screen (scripts, migrations, future surfaces) changes
// roles invisibly.
// Severity: P3 — the one shipped HTTP caller audits around the gap; this pins
// the funnel so the next caller cannot inherit the silence.
// Fix direction: emitSecurity a roles.updated event inside SetUserRoles
// (UserID, Meta: the new roles) after a successful UpdateRoles.
func TestSetRolesRedAuditsMutation(t *testing.T) {
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

	if !redHasEvent(sink, "roles.updated") {
		t.Errorf("SECURITY: [audit-gap] SetUserRoles promoted %s to [admin] and emitted no roles.updated security event; privilege changes are invisible to the auth audit funnel (events captured: [%s])", user.GetID(), redEventKinds(sink))
	}
}
