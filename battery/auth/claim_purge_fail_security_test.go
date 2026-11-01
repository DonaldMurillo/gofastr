package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mintThenFailPurgeStore mints a session for the user (a sign-in through
// a credential the claim has not removed yet) and then fails the passkey
// purge.
type mintThenFailPurgeStore struct {
	PasskeyStore
	mint func(userID string)
}

func (s mintThenFailPurgeStore) DeletePasskeysForUser(_ context.Context, userID string) (int, error) {
	s.mint(userID)
	return 0, errors.New("db down")
}

// A claim whose passkey purge fails still runs the later purges: the
// session minted since the first purge goes, and the account stays
// unverified so the next proof retries the whole claim.
func TestClaimPurgeFailStillRevokes(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	var minted string
	f.pk.store = mintThenFailPurgeStore{f.pk.store, func(userID string) {
		sess, _, err := f.mgr.MintSession(context.Background(), userID, time.Hour)
		if err != nil {
			t.Error(err)
			return
		}
		minted = sess.Token
	}}
	delete(f.mgr.UserStore().(*userStoreWithPassword).verifiedIDs, "u-alice")
	if err := f.mgr.claimAccount(context.Background(), "u-alice", "magic_link", "", true); err == nil {
		t.Fatal("claim with a failed passkey purge returned nil")
	}
	if minted == "" {
		t.Fatal("the fixture never minted its session")
	}
	if sess, err := f.mgr.SessionStore().Get(context.Background(), minted); err == nil && sess != nil {
		t.Fatal("a session minted during the claim survived a failed passkey purge")
	}
	if f.mgr.UserStore().(*userStoreWithPassword).verifiedIDs["u-alice"] {
		t.Fatal("a failed claim marked the account verified")
	}
}

// A claim whose first session purge fails still removes the passkeys
// the registrant attached, then fails and leaves the account
// unverified (2026-11-01 review): it used to return at step 1 and leave
// every other credential for the retry.
func TestClaimSessionFailStillPurges(t *testing.T) {
	f := newPasskeyFixture(t)
	f.enrol(t, "u-alice")
	f.mgr.sessionStore = failingPurgeSessions{f.mgr.sessionStore}
	delete(f.mgr.UserStore().(*userStoreWithPassword).verifiedIDs, "u-alice")
	if err := f.mgr.claimAccount(context.Background(), "u-alice", "magic_link", "", true); err == nil {
		t.Fatal("claim with a failed session purge returned nil")
	}
	if pks, err := f.pk.store.ListPasskeys(context.Background(), "u-alice"); err != nil || len(pks) != 0 {
		t.Fatalf("passkeys after a claim whose session purge failed: %d (err %v), want 0", len(pks), err)
	}
	if f.mgr.UserStore().(*userStoreWithPassword).verifiedIDs["u-alice"] {
		t.Fatal("a failed claim marked the account verified")
	}
}
