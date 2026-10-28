package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// hookedLinks runs a hook right after the Nth FindByOAuth has resolved
// its link and before the callback sees it: the hook's writes land between
// the lookup and the session the callback mints from it.
type hookedLinks struct {
	*EntityUserStore
	mu    sync.Mutex
	calls int
	at    int
	hook  func()
}

func (h *hookedLinks) FindByOAuth(ctx context.Context, provider, providerID string) (User, error) {
	u, err := h.EntityUserStore.FindByOAuth(ctx, provider, providerID)
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
	return u, err
}

func (h *hookedLinks) arm(at int, hook func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls, h.at, h.hook = 0, at, hook
}

func (h *hookedLinks) mustHaveFired(t *testing.T) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hook != nil {
		t.Fatalf("hook armed for lookup %d never ran (%d lookups)", h.at, h.calls)
	}
}

// A squatter's OAuth sign-in that found its link just before the owner's
// claim removed it re-inserted the link through the profile-refresh
// upsert, then minted a session the claim's purge had already run past
// (2026-10-27 A3). The refresh is UPDATE-only now, and the callback
// re-resolves the link after minting and drops the session when it no
// longer names that account.
func TestOAuthLoginOvertakenByClaim(t *testing.T) {
	const email = "victim@example.com"
	var hooked *hookedLinks
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &hookedLinks{EntityUserStore: s}
		return hooked
	})
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: squatter's first sign-in = %d", code)
	}
	hooked.arm(1, func() {
		if got := c.magicLink(email); got != uid {
			t.Errorf("setup: magic link signed into %s, want the claimed account %s", got, uid)
		}
	})
	got, _ := c.oauth("corp")
	hooked.mustHaveFired(t)
	if c.linked(uid, "corp") {
		t.Error("SECURITY: the profile refresh re-inserted the link the claim removed")
	}
	if got != "" {
		t.Error("SECURITY: a sign-in overtaken by a claim answered with a session")
	}
	// The owner's magic-link session is the only one left.
	if n := liveSessions(t, c, email); n != 1 {
		t.Errorf("SECURITY: %d live sessions after the claim, want the owner's 1", n)
	}
}

// A link moved to a different account between the lookup and the mint
// no longer vouches for the first one: the session is dropped.
func TestOAuthLoginLinkMovedAway(t *testing.T) {
	var hooked *hookedLinks
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &hookedLinks{EntityUserStore: s}
		return hooked
	})
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: first sign-in = %d", code)
	}
	ctx := context.Background()
	other, err := c.store.CreateUser(ctx, "other@example.com", "irrelevant-hash", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	hooked.arm(1, func() {
		if err := c.store.UnlinkOAuth(ctx, uid, "corp"); err != nil {
			t.Error(err)
		}
		if err := c.store.LinkOAuth(ctx, other.GetID(), "corp", "attacker-sub"); err != nil {
			t.Error(err)
		}
	})
	got, _ := c.oauth("corp")
	hooked.mustHaveFired(t)
	if got != "" {
		t.Errorf("SECURITY: a sign-in whose link moved to %s kept a session for %s", other.GetID(), got)
	}
}

// A returning sign-in still refreshes the link's profile, through the
// UPDATE-only path.
func TestOAuthReturnRefreshesProfile(t *testing.T) {
	c := newClaimRig(t)
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: first sign-in = %d", code)
	}
	c.corp.userResp = &OAuth2UserInfo{ID: "attacker-sub", Email: "victim@example.com", Name: "Renamed"}
	if got, code := c.oauth("corp"); got != uid {
		t.Fatalf("returning sign-in = %q (%d), want %s", got, code, uid)
	}
	accts, err := c.store.ListAccounts(context.Background(), uid)
	if err != nil || len(accts) != 1 || accts[0].Name != "Renamed" {
		t.Fatalf("profile after a returning sign-in = %+v (%v), want Name Renamed", accts, err)
	}
}

// RefreshOAuthProfile never creates a link, and never edits a link bound
// to a different user.
func TestRefreshOAuthProfileUpdatesOnly(t *testing.T) {
	f := newOAuthLinkFixture(t)
	alice := f.createUser(t, "alice@example.com")
	bob := f.createUser(t, "bob@example.com")
	prof := OAuthAccountProfile{Name: "Mallory"}
	if err := f.store.RefreshOAuthProfile(f.ctx, alice.GetID(), "google", "g-1", prof); err != nil {
		t.Fatalf("refresh of an absent link: %v", err)
	}
	if _, err := f.store.FindByOAuth(f.ctx, "google", "g-1"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("SECURITY: refreshing an absent link created it (err %v)", err)
	}
	if err := f.store.LinkOAuthEnriched(f.ctx, bob.GetID(), "google", "g-2", OAuthAccountProfile{Name: "Bob"}); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := f.store.RefreshOAuthProfile(f.ctx, alice.GetID(), "google", "g-2", prof); err != nil {
		t.Fatalf("refresh of bob's link as alice: %v", err)
	}
	accts, err := f.store.ListAccounts(f.ctx, bob.GetID())
	if err != nil || len(accts) != 1 || accts[0].Name != "Bob" {
		t.Fatalf("SECURITY: bob's link after alice's refresh = %+v (%v), want Name Bob", accts, err)
	}
}
