package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// hookedUsers runs a hook right after the Nth FindByEmail has read its
// row and before the caller sees it: the hook's writes land between the
// read and whatever the caller does with the hash.
type hookedUsers struct {
	*EntityUserStore
	mu    sync.Mutex
	calls int
	at    int
	hook  func()
}

func (h *hookedUsers) FindByEmail(ctx context.Context, email string) (User, string, error) {
	u, hash, err := h.EntityUserStore.FindByEmail(ctx, email)
	h.mu.Lock()
	h.calls++
	var run func()
	if h.hook != nil && h.calls == h.at {
		run, h.hook = h.hook, nil
	}
	h.mu.Unlock()
	if run != nil {
		run()
	}
	return u, hash, err
}

func (h *hookedUsers) arm(at int, hook func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls, h.at, h.hook = 0, at, hook
}

// mustHaveFired fails a test whose hook never ran: the race it stages did
// not happen, so a pass would prove nothing.
func (h *hookedUsers) mustHaveFired(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hook != nil {
		t.Fatalf("hook armed for lookup %d never ran (%d lookups)", h.at, h.calls)
	}
}

// Login read the hash, spent bcrypt time on it and then minted a session.
// A reset completing in between purged the sessions before the new one
// existed, so the old password signed in after the reset that was meant
// to lock it out.
func TestLoginLosesRaceToReset(t *testing.T) {
	const email, oldPW, newPW = "racer@example.com", "old-password-1", "new-password-1"
	setup := func(t *testing.T) (*claimRig, *hookedUsers, func()) {
		var hooked *hookedUsers
		c := newClaimRig(t, func(s *EntityUserStore) UserStore {
			hooked = &hookedUsers{EntityUserStore: s}
			return hooked
		})
		if w := c.json("/auth/register", map[string]string{"email": email, "password": oldPW}, ""); w.Code != http.StatusAccepted {
			t.Fatalf("register: %d %s", w.Code, w.Body.String())
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
		return c, hooked, reset
	}

	t.Run("reset between the hash read and the session", func(t *testing.T) {
		c, hooked, reset := setup(t)
		hooked.arm(1, reset) // the login's own lookup
		w := c.json("/auth/login", map[string]string{"email": email, "password": oldPW}, "")
		hooked.mustHaveFired(t)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("login racing the reset = %d %s, want 401", w.Code, w.Body.String())
		}
		if ck := sessionCookie(w); ck != "" {
			if _, err := c.mgr.SessionStore().Get(context.Background(), ck); err == nil {
				t.Fatal("a session minted under the old password is live after the reset")
			}
		}
		if got := liveSessions(t, c, email); got != 0 {
			t.Fatalf("%d live sessions after the reset, want 0", got)
		}
	})

	t.Run("reset after the re-check", func(t *testing.T) {
		c, hooked, reset := setup(t)
		hooked.arm(2, reset) // the re-check after the session is stored
		w := c.json("/auth/login", map[string]string{"email": email, "password": oldPW}, "")
		hooked.mustHaveFired(t)
		if w.Code != http.StatusOK {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		// The session existed when the reset ran, so its purge took it.
		if _, err := c.mgr.SessionStore().Get(context.Background(), sessionCookie(w)); err == nil {
			t.Fatal("the session survived the reset's purge")
		}
		// The JWT is bound to the hash the login verified, not one read
		// after the reset.
		var resp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Token == "" {
			t.Fatalf("login answer carries no token: %s", w.Body.String())
		}
		if got := c.jwtStatus(resp.Token); got != http.StatusUnauthorized {
			t.Fatalf("JWT from the raced login = %d, want 401", got)
		}
	})

	t.Run("no reset", func(t *testing.T) {
		c, _, _ := setup(t)
		if ck := c.login(email, oldPW); ck == "" {
			t.Fatal("plain login refused")
		}
	})
}

// liveSessions counts the sessions the store still holds for email's
// user, by purging them: the purge reports how many it removed.
func liveSessions(t *testing.T, c *claimRig, email string) int {
	t.Helper()
	u, _, err := c.store.FindByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	purger, ok := c.mgr.SessionStore().(SessionUserPurger)
	if !ok {
		t.Fatal("rig session store cannot purge by user")
	}
	n, err := purger.DeleteByUser(context.Background(), u.GetID())
	if err != nil {
		t.Fatal(err)
	}
	return n
}
