package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Found by the 2026-11-01 battery/auth bug bash.
//
// Passing the 2FA challenge marked the judged pending session verified in
// place. An attacker who knows the password logs in (pending session T),
// plants T in the victim's browser (a sibling-subdomain cookie, a
// plain-HTTP hop) and waits: the victim's code upgraded T, a token the
// attacker already held, to a fully verified session. Password login
// rotates against fixation; the pending-to-verified step now does too.

// steppedUpSessionCookie returns the session cookie a successful challenge
// set, failing when it set none or kept the pre-challenge token.
func steppedUpSessionCookie(t *testing.T, w *httptest.ResponseRecorder, name, old string) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			if c.Value == old {
				t.Fatalf("challenge re-set the pre-challenge token")
			}
			return c.Value
		}
	}
	t.Fatalf("challenge set no %s cookie", name)
	return ""
}

func TestTwoFAChallengeRotatesTheSession(t *testing.T) {
	for _, viaBackup := range []bool{false, true} {
		t.Run(fmt.Sprintf("backup=%v", viaBackup), func(t *testing.T) {
			f := newChallengeFixture(t, TwoFAConfig{})
			planted := f.login() // the attacker's pending session
			ctx := context.Background()

			code := totpNow(f.secret)
			if viaBackup {
				code = f.backupCodes[0]
			}
			w := f.post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, code), planted, "")
			if w.Code != http.StatusOK {
				t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
			}
			fresh := steppedUpSessionCookie(t, w, "session_id", planted)

			if s, err := f.mgr.SessionStore().Get(ctx, planted); err == nil && s != nil {
				t.Errorf("SECURITY: the pre-challenge token survives the step-up (verified=%v)", s.TwoFactorVerified)
			}
			s, err := f.mgr.SessionStore().Get(ctx, fresh)
			if err != nil || s == nil || !s.TwoFactorVerified || s.PendingTwoFactor {
				t.Fatalf("fresh session not stepped up: %+v err=%v", s, err)
			}
			if cc := w.Header().Get("Cache-Control"); cc == "" {
				t.Error("challenge response that sets a session cookie carries no Cache-Control")
			}
		})
	}
}

// A reset or disable that deleted the judged session while the challenge
// ran must not be outlived by the fresh session.
func TestTwoFAChallengeRefusesWhenJudgedSessionVanished(t *testing.T) {
	f := newChallengeFixture(t, TwoFAConfig{})
	s := f.login()
	store := &deleteOnMarkStore{SessionStore: f.mgr.SessionStore(), victim: s}
	f.mgr.mu.Lock()
	f.mgr.sessionStore = store
	f.mgr.mu.Unlock()

	w := f.post("/auth/2fa/challenge", fmt.Sprintf(`{"code":%q}`, totpNow(f.secret)), s, "")
	if w.Code == http.StatusOK {
		t.Fatalf("challenge answered 200 although its session was deleted mid-request")
	}
	if store.fresh == "" {
		t.Fatal("setup: no fresh session was minted")
	}
	if got, err := store.SessionStore.Get(context.Background(), store.fresh); err == nil && got != nil {
		t.Errorf("SECURITY: the fresh session outlived the deletion of the session it replaced")
	}
}

// deleteOnMarkStore deletes victim when the fresh session is marked
// verified: a reset landing between the mint and the swap.
type deleteOnMarkStore struct {
	SessionStore
	victim string
	fresh  string
}

func (d *deleteOnMarkStore) MarkTwoFactorVerified(ctx context.Context, token string) error {
	d.fresh = token
	_ = d.SessionStore.Delete(ctx, d.victim)
	return d.SessionStore.(SessionTwoFAMarker).MarkTwoFactorVerified(ctx, token)
}

func (d *deleteOnMarkStore) MarkPendingTwoFactor(ctx context.Context, token string) error {
	return d.SessionStore.(SessionPendingMarker).MarkPendingTwoFactor(ctx, token)
}
