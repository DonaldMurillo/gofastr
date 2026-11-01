package auth

import (
	"context"
	"net/http"
	"testing"
)

// A failed passkey purge used to return before the other purges ran, so a
// reset answered 500 with the attacker's session cookie still live. The
// reset carries on with every purge and only then answers 500.
func TestResetPurgeFailStillRevokes(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	stolen := f.session(t, "u-alice")
	f.pk.store = failingPurgeStore{f.pk.store}
	if w := f.do(t, "POST", "/forgot-password", "", map[string]string{"email": "alice@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d %s", w.Code, w.Body)
	}
	_, mail := f.mail.snapshot()
	w := f.do(t, "POST", "/reset-password", "", map[string]string{"token": extractTokenFromBody(mail), "password": "brandnewpw1"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("reset with a failed passkey purge: %d %s, want 500", w.Code, w.Body)
	}
	if sess, err := f.mgr.SessionStore().Get(context.Background(), stolen); err == nil && sess != nil {
		t.Fatal("session minted before the reset survived a failed passkey purge")
	}
}
