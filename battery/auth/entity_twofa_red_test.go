//go:build red

package auth

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// CONTRACT-QUESTION red: TwoFAState.Secret's own doc comment says the secret is
// "stored encrypted at rest in production", but EntityTwoFAStore binds it
// verbatim. Delete or promote per maintainer decision: seal the column (the
// battery already seals OAuth refresh tokens with AES-GCM and refuses to
// construct SQLOAuthTokenStore without an EncryptionKey), or fix the comment
// to document deliberately-plaintext storage.
// Property: a password-equivalent credential (the TOTP seed authenticates
// every 2FA-gated route) is not recoverable from a raw DB read.
// Surfaces: entity_twofa_store.go::SetTwoFA (INSERT binds state.Secret into
// the `secret` column), entity_twofa_store.go::EnsureSchema (column DDL).
// Finding: the raw auth_twofa.secret column holds the live base32 TOTP
// secret; any DB dump, backup, or row-level read compromises every enrolled
// account's second factor.
// Fix direction: AEAD-seal state.Secret under an EncryptionKey before the
// INSERT (and decrypt in GetTwoFA), mirroring oauth_token_store.go.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestTwoFAStoreRedSealsSecretAtRest(t *testing.T) {
	s := newTwoFAStore(t)
	ctx := context.Background()

	const secret = "JBSWY3DPEHPK3PXPSECRETSEED42"
	if err := s.SetTwoFA(ctx, "u-red", &TwoFAState{
		Enabled: true, Verified: true, Secret: secret,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	var raw sql.NullString
	if err := s.db.QueryRow("SELECT secret FROM "+s.table+" WHERE user_id = $1", "u-red").Scan(&raw); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if !raw.Valid {
		t.Fatal("no secret row stored — setup no longer reaches the sink")
	}
	stored := raw.String
	if stored == secret || strings.Contains(stored, secret) || strings.Contains(secret, stored) {
		t.Errorf("SECURITY: [twofa-at-rest] auth_twofa.secret stores the live TOTP seed recoverable by a raw read "+
			"(stored %q vs plaintext %q); TwoFAState.Secret documents encrypted-at-rest and the sibling OAuth token "+
			"store already enforces sealing", stored, secret)
	}
}
