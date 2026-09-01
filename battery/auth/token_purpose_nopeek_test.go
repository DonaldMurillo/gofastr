package auth

// Cross-flow token purpose binding, on a store WITHOUT MagicLinkTokenPeeker.
//
// redeemPurposeToken checks the purpose twice: once at peek time, and again
// on the payload RedeemToken returned. Every store shipped in this package
// peeks, so token_purpose_security_test.go only ever exercises the first
// check — removing the second leaves that whole file green.
//
// The second check is the one the design note is about: tagging lives in
// this package rather than in MagicLinkTokenStore so "a host wiring its own
// MagicLinkTokenStore gets the separation without implementing anything
// new". A host that implements the bare three-method interface is exactly
// the case the post-redeem check exists for, and it had no test.
//
// These run the same cross-flow scenarios against nonPeekingTokenStore, and
// pin the documented cost of that path: a misdirected token is burned.

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// nonPeekingTokenStore is the bare MagicLinkTokenStore a host gets from
// implementing the documented interface and nothing else. It forwards to a
// real store by hand rather than embedding it: embedding would promote
// PeekToken (and DeleteTokensForPayload) and quietly put these tests back on
// the peeking path.
type nonPeekingTokenStore struct{ inner *SQLMagicLinkTokenStore }

func (s *nonPeekingTokenStore) CreateToken(ctx context.Context, payload string, ttl time.Duration) (string, error) {
	return s.inner.CreateToken(ctx, payload, ttl)
}

func (s *nonPeekingTokenStore) RedeemToken(ctx context.Context, token string) (string, error) {
	return s.inner.RedeemToken(ctx, token)
}

func (s *nonPeekingTokenStore) Cleanup(ctx context.Context) (int, error) {
	return s.inner.Cleanup(ctx)
}

func newNonPeekingStore(t *testing.T) *nonPeekingTokenStore {
	t.Helper()
	s := &nonPeekingTokenStore{inner: newSQLStore(t)}
	// The whole point of the fixture. If a later refactor gives this type a
	// PeekToken (or switches it to embedding), every test below silently
	// moves to the peek branch and stops testing anything.
	if _, ok := any(s).(MagicLinkTokenPeeker); ok {
		t.Fatal("nonPeekingTokenStore implements MagicLinkTokenPeeker; the fallback path is no longer under test")
	}
	if _, ok := any(s).(MagicLinkTokenPurger); ok {
		t.Fatal("nonPeekingTokenStore implements MagicLinkTokenPurger; it is meant to be the bare interface")
	}
	return s
}

func newNonPeekingHarness(t *testing.T) *tokenPurposeHarness {
	t.Helper()
	return newTokenPurposeHarnessOn(t, newNonPeekingStore(t))
}

// Sanity, same role as TestSharedStoreStillServesOwnFlows: each flow still
// serves its own token when the store cannot peek. A RED below is then the
// purpose check, not the missing peeker breaking the wiring.
func TestNonPeekingStoreStillServesOwnFlows(t *testing.T) {
	h := newNonPeekingHarness(t)

	if code := h.attemptReset(h.mintResetToken()); code != http.StatusOK {
		t.Errorf("reset token at /auth/reset-password: got %d, want 200", code)
	}
	if code := h.attemptVerifyEmail(h.mintVerificationToken()); code != http.StatusOK {
		t.Errorf("verification token at /auth/verify-email: got %d, want 200", code)
	}
	if code := h.attemptMagicVerify(h.mintMagicLinkToken()); code != http.StatusFound {
		t.Errorf("magic-link token at /auth/magic-link/verify: got %d, want 302", code)
	}
}

// The account-takeover case on the fallback path: a verification token
// (24h TTL, minted behind only the victim's own session, delivered as a
// GET link) must not reach SetPassword.
func TestNonPeekingStoreRefusesVerificationTokenAtReset(t *testing.T) {
	h := newNonPeekingHarness(t)
	before := h.passwordHash()

	if code := h.attemptReset(h.mintVerificationToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] verification token at /auth/reset-password on a non-peeking store: got %d, want 401", code)
	}
	if after := h.passwordHash(); after != before {
		t.Error("SECURITY: [token-purpose] verification token changed the victim's password hash on a non-peeking store")
	}
}

// The other two directions, matching TestResetTokenCannotDriveOtherFlows.
func TestNonPeekingStoreRefusesResetTokenElsewhere(t *testing.T) {
	h := newNonPeekingHarness(t)

	if code := h.attemptVerifyEmail(h.mintResetToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] reset token at /auth/verify-email on a non-peeking store: got %d, want 401", code)
	}
	if h.store.verifiedIDs[h.victimID] {
		t.Error("SECURITY: [token-purpose] reset token marked the victim's email verified on a non-peeking store")
	}

	if code := h.attemptMagicVerify(h.mintResetToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] reset token at /auth/magic-link/verify on a non-peeking store: got %d, want 401", code)
	}
	if _, _, err := h.store.FindByEmail(context.Background(), h.victimID); err == nil {
		t.Error("SECURITY: [token-purpose] reset token auto-created an account keyed by the victim's userID as its email")
	}
}

// A magic-link token carries an EMAIL payload; at /auth/reset-password that
// email would be handed to SetPassword as a userID. Refused here too.
func TestNonPeekingStoreRefusesMagicLinkTokenAtReset(t *testing.T) {
	h := newNonPeekingHarness(t)
	before := h.passwordHash()

	if code := h.attemptReset(h.mintMagicLinkToken()); code != http.StatusUnauthorized {
		t.Errorf("SECURITY: [token-purpose] magic-link token at /auth/reset-password on a non-peeking store: got %d, want 401", code)
	}
	if after := h.passwordHash(); after != before {
		t.Error("SECURITY: [token-purpose] magic-link token changed the victim's password hash on a non-peeking store")
	}
}

// The documented cost of the fallback, pinned so it is a known trade and not
// a surprise: RedeemToken is atomic and single-use, so a store that cannot
// peek has already consumed the token by the time the purpose is checked.
// Anyone holding a victim's magic link can therefore burn it by replaying it
// at /auth/reset-password — refused there, and gone from its own flow.
//
// TestMagicLinkTokenCannotResetPassword asserts the OPPOSITE outcome (302)
// on a peeking store. That contrast is the argument for implementing
// MagicLinkTokenPeeker; if this test starts failing with a 302, the fallback
// grew a way to check before consuming and the doc comment on
// redeemPurposeToken should say so.
func TestNonPeekingStoreBurnsMisdirectedToken(t *testing.T) {
	h := newNonPeekingHarness(t)
	tok := h.mintMagicLinkToken()

	if code := h.attemptReset(tok); code != http.StatusUnauthorized {
		t.Fatalf("magic-link token at /auth/reset-password: got %d, want 401", code)
	}
	if code := h.attemptMagicVerify(tok); code != http.StatusUnauthorized {
		t.Errorf("magic-link token after a refused reset attempt on a non-peeking store: got %d, want 401 (the documented cost: it was consumed)", code)
	}
}
