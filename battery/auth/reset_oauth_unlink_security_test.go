package auth

// A password reset on a verified account kept its OAuth links: unlinking ran
// only inside claimAccount, which runs for unverified accounts. Someone with
// a stolen session links their own IdP identity, the owner resets, and the
// attacker signs back in through the IdP. A reset now removes every link and
// mails a confirmation naming the removed providers (issue #614, O12).

import (
	"net/http"
	"strings"
	"testing"
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
