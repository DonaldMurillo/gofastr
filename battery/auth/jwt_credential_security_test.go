package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// loginJWT signs in with the password and returns the JWT the login answer
// carries.
func (c *claimRig) loginJWT(email, pw string) string {
	c.t.Helper()
	w := c.json("/auth/login", map[string]string{"email": email, "password": pw}, "")
	if w.Code != http.StatusOK {
		c.t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Token == "" {
		c.t.Fatalf("login answer carries no token: %s", w.Body.String())
	}
	return body.Token
}

// jwtStatus answers a request behind RequireAuth carrying token.
func (c *claimRig) jwtStatus(token string) int {
	h := RequireAuth(c.mgr.JWT())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

// A JWT checked only its signature, expiry and that its subject still
// existed. A squatter who registered the victim's address kept a working
// JWT after the owner claimed the account, and so did anyone holding one
// across a password reset or change. Each of the three must end every JWT
// issued before it.
func TestJWTDiesWithCredentialChange(t *testing.T) {
	const email, pw = "victim@example.com", "attacker-chosen-pw-1"
	register := func(c *claimRig) {
		if w := c.json("/auth/register", map[string]string{"email": email, "password": pw}, ""); w.Code != http.StatusAccepted {
			t.Fatalf("register: %d %s", w.Code, w.Body.String())
		}
	}
	t.Run("magic-link claim", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		tok := c.loginJWT(email, pw)
		if got := c.jwtStatus(tok); got != http.StatusOK {
			t.Fatalf("fresh JWT = %d, want 200", got)
		}
		c.magicLink(email)
		if got := c.jwtStatus(tok); got != http.StatusUnauthorized {
			t.Fatalf("squatter JWT after the claim = %d, want 401", got)
		}
	})
	t.Run("passwordless account claim", func(t *testing.T) {
		// An unverified IdP login creates the account with the shared
		// placeholder hash. A claim that wrote the same placeholder back
		// left the tag, and the squatter's JWT, unchanged.
		c := newClaimRig(t)
		u, err := c.store.CreateUserNoPassword(context.Background(), email, nil)
		if err != nil {
			t.Fatal(err)
		}
		tok, err := c.mgr.JWT().GenerateToken(u)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.jwtStatus(tok); got != http.StatusOK {
			t.Fatalf("fresh JWT = %d, want 200", got)
		}
		if got := c.magicLink(email); got != u.GetID() {
			t.Fatalf("magic link signed into %s, want %s", got, u.GetID())
		}
		if got := c.jwtStatus(tok); got != http.StatusUnauthorized {
			t.Fatalf("squatter JWT after the claim = %d, want 401", got)
		}
	})
	t.Run("password reset", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		tok := c.loginJWT(email, pw)
		if w := c.json("/auth/forgot-password", map[string]string{"email": email}, ""); w.Code != http.StatusOK {
			t.Fatalf("forgot-password: %d", w.Code)
		}
		_, body := c.resets.snapshot()
		if w := c.json("/auth/reset-password", map[string]string{"token": extractTokenFromBody(body), "password": "owner-new-pw-1"}, ""); w.Code != http.StatusOK {
			t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
		}
		if got := c.jwtStatus(tok); got != http.StatusUnauthorized {
			t.Fatalf("JWT after the reset = %d, want 401", got)
		}
		if got := c.jwtStatus(c.loginJWT(email, "owner-new-pw-1")); got != http.StatusOK {
			t.Fatalf("JWT from the new password = %d, want 200", got)
		}
	})
	t.Run("password change", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		uid := c.sessionUser(c.login(email, pw))
		if err := c.store.MarkEmailVerified(context.Background(), uid); err != nil {
			t.Fatal(err)
		}
		tok := c.loginJWT(email, pw)
		sess := c.login(email, pw)
		w := c.json("/auth/password", map[string]string{"current_password": pw, "password": "changed-pw-2"}, sess)
		if w.Code != http.StatusOK {
			t.Fatalf("password change: %d %s", w.Code, w.Body.String())
		}
		if got := c.jwtStatus(tok); got != http.StatusUnauthorized {
			t.Fatalf("JWT after the change = %d, want 401", got)
		}
	})
	t.Run("token without a credential tag", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		uid := c.sessionUser(c.login(email, pw))
		// Same key and issuer, no store: the token a deployment minted
		// before the tag existed, or one a store-less helper signs.
		bare := NewJWTAuth(c.mgr.JWT().Secret, time.Hour)
		bare.Issuer = c.mgr.JWT().Issuer
		tok, err := bare.GenerateToken(&BasicUser{ID: uid, Email: email})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.mgr.JWT().ValidateToken(tok); err != nil {
			t.Fatalf("signature should still verify: %v", err)
		}
		if got := c.jwtStatus(tok); got != http.StatusUnauthorized {
			t.Fatalf("untagged JWT = %d, want 401", got)
		}
	})
	t.Run("rotated key keeps the tag", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		tok := c.loginJWT(email, pw)
		j := c.mgr.JWT()
		j.PreviousSecrets = []string{j.Secret}
		j.Secret = "rotated-signing-key-0123456789abcdef"
		if got := c.jwtStatus(tok); got != http.StatusOK {
			t.Fatalf("JWT across a key rotation = %d, want 200", got)
		}
	})
	t.Run("mint for another user's address", func(t *testing.T) {
		c := newClaimRig(t)
		register(c)
		// A stale or forged User whose email resolves to a different row
		// must not borrow that row's credential.
		if _, err := c.mgr.JWT().GenerateToken(&BasicUser{ID: "someone-else", Email: email}); err == nil {
			t.Fatal("GenerateToken bound a token to another user's credential")
		}
	})
}
