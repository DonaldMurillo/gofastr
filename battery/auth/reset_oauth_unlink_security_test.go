package auth

// A password reset on a verified account kept its OAuth links: unlinking ran
// only inside claimAccount, which runs for unverified accounts. Someone with
// a stolen session links their own IdP identity, the owner resets, and the
// attacker signs back in through the IdP. A reset now removes every link and
// mails a confirmation naming the removed providers (issue #614, O12).

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResetUnlinksOAuthOnVerified(t *testing.T) {
	c := newClaimRig(t)
	uid, code := c.oauth("google")
	if uid == "" {
		t.Fatalf("google sign-in: code %d", code)
	}
	if !c.verified(uid) || !c.linked(uid, "google") {
		t.Fatal("setup: want a verified account with google linked")
	}
	if w := c.json("/auth/forgot-password", map[string]string{"email": "victim@example.com"}, ""); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", w.Code)
	}
	_, body := c.resets.snapshot()
	tok := extractTokenFromBody(body)
	if w := c.json("/auth/reset-password", map[string]string{"token": tok, "password": "owner-new-pw-1"}, ""); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
	}
	if c.linked(uid, "google") {
		t.Fatal("google link survived the reset of a verified account")
	}
	to, notice := c.resets.snapshot()
	if to != "victim@example.com" {
		t.Fatalf("confirmation went to %q", to)
	}
	if strings.Contains(notice, "token=") || !strings.Contains(notice, "google") { // not-a-secret: asserts the notice carries no reset link
		t.Fatalf("confirmation does not name the removed provider: %q", notice)
	}
}

// listHookUsers runs hook once, inside the reset's OAuth link listing.
type listHookUsers struct {
	*EntityUserStore
	once sync.Once
	hook func()
}

func (h *listHookUsers) ListAccounts(ctx context.Context, userID string) ([]Account, error) {
	if h.hook != nil {
		h.once.Do(h.hook)
	}
	return h.EntityUserStore.ListAccounts(ctx, userID)
}

// The attacker's IdP sign-in landing while the reset unlinks: the reset
// revoked sessions before unlinking, so the session that sign-in minted
// outlived the reset. Unlinking first leaves the revoke to catch it.
func TestResetRevokesIdPSessionMidUnlink(t *testing.T) {
	var hooked *listHookUsers
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &listHookUsers{EntityUserStore: s}
		return hooked
	})
	uid, code := c.oauth("google")
	if uid == "" {
		t.Fatalf("google sign-in: code %d", code)
	}
	if w := c.json("/auth/forgot-password", map[string]string{"email": "victim@example.com"}, ""); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", w.Code)
	}
	_, body := c.resets.snapshot()
	tok := extractTokenFromBody(body)
	fired := false
	hooked.hook = func() {
		fired = true
		if _, err := c.mgr.SessionStore().Create(context.Background(), uid, time.Hour); err != nil {
			t.Errorf("mint: %v", err)
		}
	}
	if w := c.json("/auth/reset-password", map[string]string{"token": tok, "password": "owner-new-pw-1"}, ""); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
	}
	if !fired {
		t.Fatal("the reset never listed OAuth links; the race was not staged")
	}
	if n := liveSessions(t, c, "victim@example.com"); n != 0 {
		t.Fatalf("SECURITY: %d session(s) minted during the unlink survived the reset", n)
	}
}
