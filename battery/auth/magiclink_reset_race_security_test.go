package auth

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// A magic link redeemed just before a password reset finished minted its
// session after the reset had purged the user's sessions and spent
// tokens, so the reset meant to lock everyone out left one session live.
func TestMagicLinkLosesRaceToReset(t *testing.T) {
	const email, oldPW, newPW = "racer@example.com", "old-password-1", "new-password-1"
	setup := func(t *testing.T) (*claimRig, *hookedUsers, func(), string) {
		var hooked *hookedUsers
		c := newClaimRig(t, func(s *EntityUserStore) UserStore {
			hooked = &hookedUsers{EntityUserStore: s}
			return hooked
		})
		if w := c.json("/auth/register", map[string]string{"email": email, "password": oldPW}, ""); w.Code != http.StatusAccepted {
			t.Fatalf("register: %d %s", w.Code, w.Body.String())
		}
		u, _, err := c.store.FindByEmail(context.Background(), email)
		if err != nil {
			t.Fatal(err)
		}
		// Verified, so the redeem signs in rather than claiming.
		if err := c.store.MarkEmailVerified(context.Background(), u.GetID()); err != nil {
			t.Fatal(err)
		}
		if w := c.json("/auth/forgot-password", map[string]string{"email": email}, ""); w.Code != http.StatusOK {
			t.Fatalf("forgot-password: %d", w.Code)
		}
		_, body := c.resets.snapshot()
		resetTok := extractTokenFromBody(body)
		reset := func() {
			if w := c.json("/auth/reset-password", map[string]string{"token": resetTok, "password": newPW}, ""); w.Code != http.StatusOK {
				t.Errorf("reset-password: %d %s", w.Code, w.Body.String())
			}
		}
		if w := c.json("/auth/magic-link/send", map[string]string{"email": email}, ""); w.Code != http.StatusOK {
			t.Fatalf("magic-link send: %d %s", w.Code, w.Body.String())
		}
		return c, hooked, reset, c.links.last(t)
	}
	verify := func(c *claimRig, tok string) *http.Response {
		form := url.Values{"token": {tok}}
		return c.do(http.MethodPost, "/auth/magic-link/verify", form.Encode(), "application/x-www-form-urlencoded", "").Result()
	}

	t.Run("reset between the redeem and the session", func(t *testing.T) {
		c, hooked, reset, tok := setup(t)
		hooked.arm(1, reset) // the verify's lookup, after the link is spent
		resp := verify(c, tok)
		hooked.mustHaveFired(t)
		if resp.StatusCode == http.StatusFound {
			t.Errorf("magic link racing the reset = %d, want a refusal", resp.StatusCode)
		}
		if got := liveSessions(t, c, email); got != 0 {
			t.Fatalf("%d live sessions after the reset, want 0", got)
		}
	})

	t.Run("no reset", func(t *testing.T) {
		c, _, _, tok := setup(t)
		if resp := verify(c, tok); resp.StatusCode != http.StatusFound {
			t.Fatalf("plain magic link = %d, want 302", resp.StatusCode)
		}
		if got := liveSessions(t, c, email); got != 1 {
			t.Fatalf("%d live sessions, want 1", got)
		}
	})
}
