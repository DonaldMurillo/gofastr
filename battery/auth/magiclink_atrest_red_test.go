//go:build red

package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Pinned sibling: TestOAuthTokenStore_StoredOpaque
// (oauth_token_store_test.go:88) — "a DB dump should not leak the live
// access/refresh secrets" — the aeadSealer family contract
// (oauth_token_store.go:21-23). TestAPIToken_PlaintextNeverStored
// (apitoken_security_test.go:68) is the sha256 twin, the 2FA store the
// sealed twin. The magic-link token table is the divergent family member:
// same battery, same password-equivalent secret class, plaintext column.
// Property: no password-equivalent secret in a plaintext SQL column.
// Surfaces: battery/auth/magiclink_sql.go::SQLMagicLinkTokenStore —
// ensureTable :66 `token TEXT PRIMARY KEY`, CreateToken :107 raw INSERT,
// RedeemToken/PeekToken look up by the raw token. One table carries
// magic-link + PASSWORD-RESET + email-verification tokens
// (createPurposeToken, token_purpose.go:50-52), and password_reset.go's
// own dev-log comment (:239-241) names the class: "the URL embeds the raw
// token, which is a takeover credential".
// Finding: the minted 64-hex token lands in the token column verbatim, so
// a read-only dump of the table (backup artifact, replica, SELECT-granted
// reporting role) is a live account-takeover kit for every unexpired mint
// across all three flows.
// Fix direction: store sha256(token) keyed for lookup (the APIToken
// grammar — the token is 256 random bytes, so the hash is both
// collision-safe and irreversible) or AEAD-seal the column (the OAuth
// grammar); RedeemToken/PeekToken then key on the digest/sealed value.

func TestMagicLinkRedStoredOpaque(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatalf("setup broken: NewSQLMagicLinkTokenStore: %v", err)
	}
	ctx := context.Background()

	tok, err := s.CreateToken(ctx, "alice@example.com", 15*time.Minute)
	if err != nil {
		t.Fatalf("setup broken: CreateToken: %v", err)
	}
	// Guard: the minted credential is a full 64-hex (256-bit) secret, so
	// "stored column != returned token" cannot pass vacuously.
	if len(tok) != 64 {
		t.Fatalf("setup broken: minted token is not the 64-hex shape: %q", tok)
	}

	var stored string
	if err := db.QueryRowContext(ctx, "SELECT token FROM magic_link_tokens").Scan(&stored); err != nil {
		t.Fatalf("setup broken: SELECT stored token: %v", err)
	}
	if stored == tok {
		t.Errorf("SECURITY: [magiclink-plaintext-at-rest] magic_link_tokens.token holds the raw takeover credential verbatim (stored == returned token): a read-only dump of the table redeems every unexpired magic-link, password-reset, and email-verification mint. OAuth tokens are sealed and API tokens hashed in this same battery; this column must not be the plaintext sibling.")
	}

	// Positive control: the returned token is a live credential the store
	// still redeems (the property is at-rest opacity, not broken minting).
	email, err := s.RedeemToken(ctx, tok)
	if err != nil || email != "alice@example.com" {
		t.Fatalf("setup broken: RedeemToken control = %q, %v; want alice@example.com", email, err)
	}

	// Same property through the password-reset flow's own mint path: the
	// purpose-prefixed token rides the same table and column.
	resetTok, err := createPurposeToken(ctx, s, purposeReset, "u-reset", time.Hour)
	if err != nil {
		t.Fatalf("setup broken: createPurposeToken(purposeReset): %v", err)
	}
	var resetStored string
	if err := db.QueryRowContext(ctx,
		"SELECT token FROM magic_link_tokens WHERE email = 'pwreset:u-reset'",
	).Scan(&resetStored); err != nil {
		t.Fatalf("setup broken: SELECT reset token: %v", err)
	}
	if resetStored == resetTok {
		t.Errorf("SECURITY: [magiclink-plaintext-at-rest] the PASSWORD-RESET token minted by createPurposeToken(purposeReset) is stored verbatim in magic_link_tokens.token: anyone with read access to the table resets any account's password.")
	}
}

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3 CONTRACT-QUESTION).
// CONTRACT-QUESTION: auth.md:1477-1478 documents a carve-out — "The
// session store is trusted. A compromise of the session table is game
// over: sessions are bearer tokens by design." This test asserts that
// hash-then-lookup nonetheless defends the READ-ONLY exposure class (dump,
// backup, replica, SELECT-granted role) — the exact rationale the OAuth
// sealing contract gives for its own column. If the maintainer reads the
// carve-out as "plaintext session column is fine", DELETE this test.
// Pinned sibling: TestOAuthTokenStore_StoredOpaque
// (oauth_token_store_test.go:88) — at-rest opacity of a bearer credential.
// Property: a session bearer token is not stored as a plaintext SQL
// column.
// Surfaces: battery/auth/entity_store.go::EntitySessionStore — EnsureSchema
// :484 `token TEXT UNIQUE NOT NULL`, Create :520-521 INSERT of the raw
// token, Get :535 lookup by the raw token.
// Finding: the column is byte-identical to the cookie value, so a read-only
// dump of the sessions table is a pile of live session cookies.
// Fix direction: store sha256(token) and key Get/Delete/MarkTwoFactorVerified
// on the digest (the APIToken grammar; the token is 256 random bytes).

func TestSessionRedStoredOpaque(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()

	store := NewEntitySessionStore(db, "auth_sessions")
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("setup broken: EnsureSchema: %v", err)
	}
	sess, err := store.Create(ctx, "u1", time.Hour)
	if err != nil {
		t.Fatalf("setup broken: Create: %v", err)
	}
	// Guard: the minted credential is a full 256-bit base64-URL token
	// (newSessionToken, session.go:231-235), so the comparison cannot pass
	// vacuously.
	if len(sess.Token) < 32 {
		t.Fatalf("setup broken: session token is not the 256-bit shape: %q", sess.Token)
	}

	var stored string
	if err := db.QueryRowContext(ctx,
		"SELECT token FROM auth_sessions WHERE user_id = 'u1'",
	).Scan(&stored); err != nil {
		t.Fatalf("setup broken: SELECT stored token: %v", err)
	}
	if stored == sess.Token {
		t.Errorf("SECURITY: [session-plaintext-at-rest] auth_sessions.token holds the raw session cookie value verbatim (stored == sess.Token): a read-only dump of the sessions table is a pile of live bearer cookies. auth.md:1477 trusts the session store, but the OAuth sibling column in this same battery is sealed precisely against the read-only dump class; hash-then-lookup costs nothing and closes it. Delete this test if the documented carve-out is meant to bless the plaintext column.")
	}

	// Positive controls: the returned token still round-trips and the 2FA
	// marker still keys on it (the property is at-rest opacity, not broken
	// session issuance).
	got, err := store.Get(ctx, sess.Token)
	if err != nil || got.UserID != "u1" {
		t.Fatalf("setup broken: Get control = %+v, %v; want u1", got, err)
	}
	if err := store.MarkTwoFactorVerified(ctx, sess.Token); err != nil {
		t.Fatalf("setup broken: MarkTwoFactorVerified control: %v", err)
	}
	verified, err := store.Get(ctx, sess.Token)
	if err != nil || !verified.TwoFactorVerified {
		t.Fatalf("setup broken: Get after MarkTwoFactorVerified = %+v, %v; want TwoFactorVerified=true", verified, err)
	}
}
