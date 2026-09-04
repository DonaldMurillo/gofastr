//go:build red

package auth

import (
	"context"
	"database/sql"
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide what an EncryptionKey
// rotation means for already-enrolled 2FA rows. Two sibling stores built on
// the SAME aeadSealer answer oppositely today: the SQL OAuth token store
// fails closed with "ciphertext failed to open (wrong EncryptionKey?)"
// (oauth_token_store.go), while EntityTwoFAStore.openSecret falls back to
// returning the stored bytes verbatim, so after a rotation every enrolled
// user's TOTP seed silently becomes the base64 ciphertext — codes computed
// from garbage, no operator-visible signal, users locked out of the factor.
// The read-both fallback is documented for LEGACY PLAINTEXT rows
// (entity_twofa_store.go openSecret); a rekeyed row is neither openable nor
// plaintext, and the fallback misclassifies it. If the maintainer keeps
// read-both, a previous-key drain window (like JWTAuth.PreviousSecrets and
// the CSRF AdditionalKeys idiom) is the alternative; this test only asserts
// that the ciphertext must never be SERVED AS the seed.

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F15 Secret lifecycle (at-rest EncryptionKey rotation for 2FA seeds)
// Property: rotating the at-rest EncryptionKey must never cause an enrolled
// 2FA row to be served with its sealed ciphertext standing in for the TOTP
// seed — the read must either open (previous-key drain) or fail closed with
// an explicit error, exactly as the OAuth token store does on the same
// sealer.
// Surfaces: entity_twofa_store.go::EntityTwoFAStore.GetTwoFA /
// getWithVersion / openSecret (the only reader of the secret column; the
// challenge handler's ValidateTOTPStep consumes whatever Secret it returns).
// Finding: after re-keying (store rebuilt over the same table with a new
// EncryptionKey), GetTwoFA returns err=nil with state.Secret equal to the
// raw sealed column value. Every TOTP verification for every enrolled user
// then computes codes from base64 ciphertext: all codes rejected, silently,
// with no error anywhere — the OAuth token store's explicit
// "ciphertext failed to open (wrong EncryptionKey?)" posture shows the
// sibling contract. Fixing this must preserve the pinned read-both for
// legacy PLAINTEXT rows (TestTwoFAStoreReadsLegacyPlaintext) and the same-key
// round trip (TestTwoFAStoreSealsSecretAtRest).
// Severity: medium — not a bypass (verification fails closed) but a silent,
// deployment-wide 2FA outage with zero operator signal, plus the risk that a
// future "verify or treat as unenrolled" refactor turns the garbage seed into
// a bypass.
// Fix direction: mirror oauth_token_store: open failure on a non-plaintext
// column value returns an error (challenge handler surfaces "2FA
// unavailable", fail closed), or support previous keys for a drain window.

// TestTwoFARekeyNotServedAsSeed enrolls under key K1, reopens the same table
// under K2, and asserts the rekeyed row is never served with its ciphertext
// as the TOTP seed.
func TestTwoFARekeyNotServedAsSeed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	s1, err := NewEntityTwoFAStore(db, "auth_twofa_rekey_red", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("rotation-key-one"),
	})
	if err != nil {
		t.Fatalf("store under K1: %v", err)
	}
	if err := s1.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	const seed = "JBSWY3DPEHPK3PXPSECRETSEED42"
	if err := s1.SetTwoFA(ctx, "u-rekey", &TwoFAState{
		Enabled: true, Verified: true, Secret: seed,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	// The operator rotates the at-rest key: same table, new key material.
	s2, err := NewEntityTwoFAStore(db, "auth_twofa_rekey_red", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("rotation-key-two"),
	})
	if err != nil {
		t.Fatalf("store under K2: %v", err)
	}

	st, err := s2.GetTwoFA(ctx, "u-rekey")
	switch {
	case err != nil:
		// Fail-closed posture (the oauth_token_store contract): accepted.
	case st == nil:
		t.Errorf("SECURITY: [twofa-rekey] after EncryptionKey rotation GetTwoFA returned (nil, nil): " +
			"an ENABLED row must not read as unenrolled — that turns a key rotation into a silent " +
			"2FA downgrade for every enrolled user")
	case st.Secret == seed:
		// Drain window: the previous key opened the row: accepted.
	default:
		// Grab the raw column so the failure message can prove the
		// served "seed" is the stored ciphertext.
		var stored sql.NullString
		if qerr := db.QueryRow("SELECT secret FROM auth_twofa_rekey_red WHERE user_id = $1", "u-rekey").Scan(&stored); qerr != nil {
			t.Fatalf("raw read: %v", qerr)
		}
		t.Errorf("SECURITY: [twofa-rekey] after EncryptionKey rotation GetTwoFA served the sealed "+
			"ciphertext as the TOTP seed (state.Secret=%q, stored column=%q, real seed=%q): every code "+
			"computed from it fails with no error surfaced anywhere, unlike the OAuth token store's "+
			"'ciphertext failed to open (wrong EncryptionKey?)' fail-closed answer on the same sealer",
			st.Secret, stored.String, seed)
	}
}
