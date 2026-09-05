package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Pins the at-rest EncryptionKey rotation contract for 2FA seeds, found
// by the 2026-09-04 red-probe round (twofa_keyrotation_red_test.go);
// fixed by making EntityTwoFAStore fail closed with an explicit
// "wrong EncryptionKey?" error (the oauth_token_store posture on the
// same sealer) and by adding PreviousEncryptionKeys as a drain window
// that re-seals rows under the current key on read.
//
// Property: rotating the at-rest EncryptionKey must never cause an
// enrolled 2FA row to be served with its sealed ciphertext standing in
// for the TOTP seed — the read must either open (previous-key drain) or
// fail closed with an explicit error, exactly as the OAuth token store
// does on the same sealer.
// Surfaces: entity_twofa_store.go::EntityTwoFAStore.GetTwoFA /
// getWithVersion / openSecret (the only reader of the secret column).
// The pinned read-both for legacy PLAINTEXT rows lives beside it in
// TestTwoFAStoreReadsLegacyPlaintext, and the same-key round trip in
// TestTwoFAStoreSealsSecretAtRest.

func TestTwoFARekeyNotServedAsSeed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	s1, err := NewEntityTwoFAStore(db, "auth_twofa_rekey", EntityTwoFAStoreConfig{
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
	// The sealed-at-rest pin (TestTwoFAStoreSealsSecretAtRest) proves the
	// column holds ciphertext, not the seed.

	// The operator rotates the at-rest key: same table, new key material,
	// NO drain window configured.
	s2, err := NewEntityTwoFAStore(db, "auth_twofa_rekey", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("rotation-key-two"),
	})
	if err != nil {
		t.Fatalf("store under K2: %v", err)
	}

	st, err := s2.GetTwoFA(ctx, "u-rekey")
	switch {
	case err != nil:
		// Fail-closed posture (the oauth_token_store contract): accepted,
		// and the error must name the rotation so the operator sees it.
		if !strings.Contains(err.Error(), "EncryptionKey") {
			t.Errorf("rekeyed read failed closed but the error does not name the rotation: %v", err)
		}
	case st == nil:
		t.Errorf("SECURITY: [twofa-rekey] after EncryptionKey rotation GetTwoFA returned (nil, nil): " +
			"an ENABLED row must not read as unenrolled — that turns a key rotation into a silent " +
			"2FA downgrade for every enrolled user")
	case st.Secret == seed:
		t.Errorf("SECURITY: [twofa-rekey] after EncryptionKey rotation GetTwoFA returned the seed " +
			"without a drain window — the row must not have opened under a key the store was not given")
	default:
		// Grab the raw column so the failure message can prove the
		// served "seed" is the stored ciphertext.
		var stored sql.NullString
		if qerr := db.QueryRow("SELECT secret FROM auth_twofa_rekey WHERE user_id = $1", "u-rekey").Scan(&stored); qerr != nil {
			t.Fatalf("raw read: %v", qerr)
		}
		t.Errorf("SECURITY: [twofa-rekey] after EncryptionKey rotation GetTwoFA served the sealed "+
			"ciphertext as the TOTP seed (state.Secret=%q, stored column=%q, real seed=%q): every code "+
			"computed from it fails with no error surfaced anywhere, unlike the OAuth token store's "+
			"'ciphertext failed to open (wrong EncryptionKey?)' fail-closed answer on the same sealer",
			st.Secret, stored.String, seed)
	}
}

// TestTwoFADrainResealsUnderCurrentKey pins the PreviousEncryptionKeys
// drain window (the JWTAuth.PreviousSecrets / CSRF AdditionalKeys
// idiom): a row sealed under a previous key opens, is re-sealed under
// the current key on that read, and the next read no longer needs the
// old key.
func TestTwoFADrainResealsUnderCurrentKey(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	s1, err := NewEntityTwoFAStore(db, "auth_twofa_drain", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("rotation-key-one"),
	})
	if err != nil {
		t.Fatalf("store under K1: %v", err)
	}
	if err := s1.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	const seed = "JBSWY3DPEHPK3PXDRAINSEED77"
	if err := s1.SetTwoFA(ctx, "u-drain", &TwoFAState{
		Enabled: true, Verified: true, Secret: seed,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	var underK1 string
	if err := db.QueryRow("SELECT secret FROM auth_twofa_drain WHERE user_id = $1", "u-drain").Scan(&underK1); err != nil {
		t.Fatalf("raw read: %v", err)
	}

	// Rotated store WITH the drain window.
	s2, err := NewEntityTwoFAStore(db, "auth_twofa_drain", EntityTwoFAStoreConfig{
		EncryptionKey:          []byte("rotation-key-two"),
		PreviousEncryptionKeys: [][]byte{[]byte("rotation-key-one")},
	})
	if err != nil {
		t.Fatalf("store under K2+prev: %v", err)
	}

	st, err := s2.GetTwoFA(ctx, "u-drain")
	if err != nil {
		t.Fatalf("drain read: %v", err)
	}
	if st == nil || st.Secret != seed {
		t.Fatalf("drain read: state=%v err=%v, want the real seed %q (the previous key must open the row)", st, err, seed)
	}

	// The row was re-sealed under the CURRENT key on that read: the
	// column changed, and the next store — built WITHOUT the old key —
	// reads the same seed.
	var underK2 string
	if err := db.QueryRow("SELECT secret FROM auth_twofa_drain WHERE user_id = $1", "u-drain").Scan(&underK2); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if underK2 == underK1 {
		t.Fatalf("drain read left the row sealed under the PREVIOUS key; the drain must re-seal in place")
	}

	s3, err := NewEntityTwoFAStore(db, "auth_twofa_drain", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("rotation-key-two"),
	})
	if err != nil {
		t.Fatalf("store under K2 only: %v", err)
	}
	st3, err := s3.GetTwoFA(ctx, "u-drain")
	if err != nil {
		t.Fatalf("post-drain read without the old key: %v (the drain window should have retired it)", err)
	}
	if st3 == nil || st3.Secret != seed {
		t.Fatalf("post-drain read: state=%v, want the real seed %q", st3, seed)
	}
}
