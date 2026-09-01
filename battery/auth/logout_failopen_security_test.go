package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// PASS-7 PIN (SWEEPSENTINELFAILOPEN-R1): a failed session revocation
// must not be reported as a successful logout.
//
// logoutHandler (core.go:354) discards the SessionStore().Delete error
// with `_ =` and then answers 204 (JSON) or a 303 success redirect
// (form) with the cookie cleared — while the server-side session stays
// fully valid until TTL. That violates the handler's own invariant
// (core.go:339-341: "logout must not leave a shadowed-but-valid session
// alive") on exactly the path the invariant exists for, and the
// `session.revoked` audit event (core.go:347-352) is emitted BEFORE and
// independent of the Delete, so the operator's revocation ledger
// overstates reality too. Every other auth-state mutation in this
// package propagates its failure (verifyHandler 500 on MarkEmailVerified,
// password_reset.go logs a Warn on failed post-reset revocation);
// core.go:354 is the only one that is neither answered nor logged.
//
// deleteFailingSessionStore simulates a session-store outage (DB down,
// lock timeout, read-only replica) at the moment of revocation: reads
// delegate, every Delete fails.

type deleteFailingSessionStore struct {
	SessionStore
}

func (d *deleteFailingSessionStore) Delete(_ context.Context, _ string) error {
	return errors.New("simulated session-store outage: delete failed")
}

func newLogoutFailopenFixture(t *testing.T) (*AuthManager, *recordingSink, *MemorySessionStore, *Session, http.HandlerFunc) {
	t.Helper()
	base := NewMemorySessionStore()
	rec := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret:     "logout-failopen-test",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newUserStoreWithPassword(),
		SessionStore:  &deleteFailingSessionStore{SessionStore: base},
		DevMode:       true,
		AuditSink:     rec,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("mgr.Init: %v", err)
	}
	sess, err := base.Create(context.Background(), "u-logout-failopen", time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}
	core := NewCorePlugin()
	if err := core.Init(mgr); err != nil {
		t.Fatalf("core init: %v", err)
	}
	return mgr, rec, base, sess, core.logoutHandler()
}

// premiseLive fails the test if the session is NOT still resolvable —
// the fixture's whole point is that the revocation genuinely failed, so
// a missing session would mean the premise (not the handler) is broken.
func premiseLive(t *testing.T, base *MemorySessionStore, token string) {
	t.Helper()
	if _, err := base.Get(context.Background(), token); err != nil {
		t.Fatalf("premise broken: session %q not live after failed delete: %v", token, err)
	}
}

func TestLogoutFailsClosedOnRevokeFailure(t *testing.T) {
	t.Run("json client must not get a 2xx while the revoke failed", func(t *testing.T) {
		mgr, _, base, sess, logout := newLogoutFailopenFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: sess.Token})
		rr := httptest.NewRecorder()

		logout(rr, req)

		premiseLive(t, base, sess.Token)
		if rr.Code >= 200 && rr.Code < 300 {
			t.Errorf("logout answered %d (2xx) while SessionStore().Delete failed; the session token remains server-side valid until TTL — a failed revocation must not be reported as logout success (package convention for failed auth-state writes is 500)", rr.Code)
		}
	})

	t.Run("form client must not be redirected to the success target while the revoke failed", func(t *testing.T) {
		mgr, _, base, sess, logout := newLogoutFailopenFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/auth/logout?next=/after-logout", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: sess.Token})
		rr := httptest.NewRecorder()

		logout(rr, req)

		premiseLive(t, base, sess.Token)
		if rr.Code == http.StatusSeeOther && rr.Header().Get("Location") == "/after-logout" {
			t.Errorf("logout redirected to success target %q (303) while SessionStore().Delete failed; the form path must not report logout success either — an error redirect or 5xx matches the package's writeFormAuthError/500 convention for failed auth-state writes", rr.Header().Get("Location"))
		}
	})

	t.Run("audit ledger must not record session.revoked for a still-live session", func(t *testing.T) {
		mgr, rec, base, sess, logout := newLogoutFailopenFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: sess.Token})

		logout(httptest.NewRecorder(), req)

		premiseLive(t, base, sess.Token)
		for _, ev := range rec.findByKindAll("session.revoked") {
			t.Errorf("audit event session.revoked (reason=%q, user=%s) recorded while the session token is still resolvable via SessionStore().Get — the revocation ledger overstates what actually happened; emit only after Delete succeeds, or emit a distinct revoke-failed kind", ev.Meta["reason"], ev.UserID)
		}
	})
}
