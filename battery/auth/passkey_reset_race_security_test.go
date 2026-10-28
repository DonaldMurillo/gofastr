package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// racingPasskeyStore runs a hook at the two points a password reset can
// overtake a passkey sign-in.
type racingPasskeyStore struct {
	PasskeyStore
	afterUse    func()
	beforePurge func()
}

func (s *racingPasskeyStore) RecordPasskeyUse(ctx context.Context, id []byte, old, next uint32, backedUp bool, at time.Time) (bool, error) {
	wrote, err := s.PasskeyStore.RecordPasskeyUse(ctx, id, old, next, backedUp, at)
	if hook := s.afterUse; hook != nil {
		s.afterUse = nil
		hook()
	}
	return wrote, err
}

func (s *racingPasskeyStore) DeletePasskeysForUser(ctx context.Context, userID string) (int, error) {
	if hook := s.beforePurge; hook != nil {
		s.beforePurge = nil
		hook()
	}
	return s.PasskeyStore.DeletePasskeysForUser(ctx, userID)
}

// resetAlice runs alice's whole forgot/reset flow.
func (f *passkeyFixture) resetAlice(t *testing.T) *passkeyFixture {
	t.Helper()
	if w := f.do(t, "POST", "/forgot-password", "", map[string]string{"email": "alice@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d %s", w.Code, w.Body)
	}
	_, mail := f.mail.snapshot()
	if w := f.do(t, "POST", "/reset-password", "", map[string]string{"token": extractTokenFromBody(mail), "password": "brandnewpw1"}); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body)
	}
	return f
}

func (f *passkeyFixture) sessionAlive(token string) bool {
	_, err := f.mgr.SessionStore().Get(context.Background(), token)
	return token != "" && err == nil
}

// sessionsOf counts the stored sessions of userID, cookie or not.
func (f *passkeyFixture) sessionsOf(t *testing.T, userID string) int {
	t.Helper()
	mem, ok := f.mgr.SessionStore().(*MemorySessionStore)
	if !ok {
		t.Fatalf("setup: session store is %T", f.mgr.SessionStore())
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	n := 0
	for _, s := range mem.sessions {
		if s.UserID == userID {
			n++
		}
	}
	return n
}

// A reset that finishes after a passkey sign-in recorded its use but
// before it minted the session revoked nothing that existed yet and
// purged the passkey: the sign-in then minted a session for a key the
// reset had just removed (2026-10-27 A25). The sign-in re-reads the
// passkey after minting and drops the session when it is gone.
func TestPasskeyLoginOvertakenByReset(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	racer := &racingPasskeyStore{PasskeyStore: f.pk.store}
	f.pk.store = racer
	racer.afterUse = func() { f.resetAlice(t) }

	w := f.login(t, a, "u-alice")
	if w.Code == http.StatusOK {
		t.Errorf("SECURITY: a passkey sign-in overtaken by a reset answered 200")
	}
	if n := f.sessionsOf(t, "u-alice"); n != 0 {
		t.Errorf("SECURITY: a passkey sign-in overtaken by a reset left %d live session(s)", n)
	}
}

// The reset purges passkeys before it revokes sessions. The other order
// left a gap: a sign-in that minted after the revoke and re-read the key
// before the purge kept its session.
func TestPasskeyResetPurgesBeforeRevoke(t *testing.T) {
	f := newPasskeyFixture(t)
	a := f.enrol(t, "u-alice")
	racer := &racingPasskeyStore{PasskeyStore: f.pk.store}
	f.pk.store = racer
	var tok string
	racer.beforePurge = func() {
		w := f.login(t, a, "u-alice")
		if w.Code != http.StatusOK {
			t.Fatalf("setup: in-flight passkey sign-in = %d %s", w.Code, w.Body)
		}
		tok = passkeySessionCookie(w)
	}
	f.resetAlice(t)
	if tok == "" {
		t.Fatal("setup: the reset never purged passkeys")
	}
	if f.sessionAlive(tok) {
		t.Error("SECURITY: a passkey session minted during the reset survived it")
	}
}

// failingPurgeSessions refuses DeleteByUser, as a dropped connection or a
// lock timeout would.
type failingPurgeSessions struct{ SessionStore }

func (failingPurgeSessions) DeleteByUser(context.Context, string) (int, error) {
	return 0, errors.New("connection reset")
}

// A reset whose session purge failed answered {"updated": true}, and the
// attacker's live cookie kept working (2026-10-27 A9). It now answers 500
// like the sibling purges, so a fresh link retries.
func TestResetFailsWhenRevokeFails(t *testing.T) {
	f := newPasskeyFixture(t)
	f.mgr.sessionStore = failingPurgeSessions{f.mgr.sessionStore}
	if w := f.do(t, "POST", "/forgot-password", "", map[string]string{"email": "alice@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d %s", w.Code, w.Body)
	}
	_, mail := f.mail.snapshot()
	w := f.do(t, "POST", "/reset-password", "", map[string]string{"token": extractTokenFromBody(mail), "password": "brandnewpw1"})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("SECURITY: reset with a failed session purge = %d %s, want 500", w.Code, w.Body)
	}
}

// A password change whose purge of the other sessions failed answered
// success too, and a stolen cookie on another device kept working. It
// answers 500 now; the change itself stands.
func TestPasswordChangeFailsWhenRevokeFails(t *testing.T) {
	h := newChangeHarness(t, nil)
	h.mgr.sessionStore = failingPurgeSessions{h.mgr.sessionStore}
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("SECURITY: change with a failed session purge = %d %s, want 500", w.Code, w.Body)
	}
}
