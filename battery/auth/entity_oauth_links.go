package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ─── OAuth provider-link store (EntityUserStore extension) ───────────────────
//
// A user may link more than one OAuth provider (Google + GitHub + a corporate
// OIDC), so links live in their own table keyed by (provider, provider_id)
// rather than as columns on the users table. The table name is derived from
// the configured user table via the "<table>_oauth_links" convention — hosts
// that pick a custom users table get a matching links table for free, and no
// two EntityUserStore instances on the same DB collide on the link table.
//
// All four methods here are the durable implementations of the optional
// interfaces declared in accounts.go (OAuthLinker, OAuthEnrichedLinker,
// AccountLister, AccountUnlinker). EntityUserStore is now a linker by
// default — production OAuth login requires it (see OAuth2Plugin.Init).

// oauthLinksTable returns the link-table name derived from the user table.
func (s *EntityUserStore) oauthLinksTable() string {
	return s.table + "_oauth_links"
}

// EnsureOAuthLinksSchema creates the (provider, provider_id) → user_id link
// table if it does not already exist. Called from EnsureSchema so hosts never
// hand-roll the DDL. Idempotent. Timestamp type is chosen per dialect so the
// same battery boots on SQLite and Postgres.
func (s *EntityUserStore) EnsureOAuthLinksSchema(ctx context.Context) error {
	tsType := "DATETIME"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s ("+
			"provider TEXT NOT NULL,"+
			"provider_id TEXT NOT NULL,"+
			"user_id TEXT NOT NULL,"+
			"email TEXT,"+
			"name TEXT,"+
			"avatar_url TEXT,"+
			"created_at %s NOT NULL DEFAULT CURRENT_TIMESTAMP,"+
			"PRIMARY KEY (provider, provider_id)"+
			")",
		query.QuoteIdent(s.oauthLinksTable()),
		tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	// user_id index powers ListAccounts / UnlinkOAuth — without it both
	// degenerate to full scans of a table that grows with the user count.
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		query.QuoteIdent(s.oauthLinksTable()+"_user_idx"),
		query.QuoteIdent(s.oauthLinksTable()),
		query.QuoteIdent("user_id"),
	)
	if _, err := s.db.ExecContext(ctx, idx); err != nil {
		return err
	}
	return nil
}

// FindByOAuth returns the locally-linked user for a (provider, providerID)
// pair, or ErrUserNotFound when no link exists. Implements OAuthLinker.
//
// The lookup is two-step — read the user_id from the link table, then read
// the user — so the link table stays narrow and the user row stays the single
// source of truth for profile/roles. A link pointing at a since-deleted user
// resolves to ErrUserNotFound (FindByID's sentinel).
func (s *EntityUserStore) FindByOAuth(ctx context.Context, provider, providerID string) (User, error) {
	if provider == "" || providerID == "" {
		return nil, ErrUserNotFound
	}
	var userID string
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT user_id FROM %s WHERE provider = $1 AND provider_id = $2",
		query.QuoteIdent(s.oauthLinksTable()),
	), provider, providerID).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.FindByID(ctx, userID)
}

// LinkOAuth binds a (provider, providerID) pair to a user. Idempotent and
// race-safe via INSERT ... ON CONFLICT (provider, provider_id) DO UPDATE on
// the profile columns ONLY: the PRIMARY KEY is the serialization point, so
// two concurrent first-logins for the same external identity cannot create
// conflicting rows. The user_id of an existing binding is immutable from
// this path — rebinding an identity to a different local account is an admin
// operation, not an OAuth callback. Implements OAuthLinker.
func (s *EntityUserStore) LinkOAuth(ctx context.Context, userID, provider, providerID string) error {
	return s.linkOAuth(ctx, userID, provider, providerID, OAuthAccountProfile{})
}

// LinkOAuthEnriched is LinkOAuth plus the profile snapshot at link time.
// Implements OAuthEnrichedLinker. The profile fields are informational — the
// authoritative identity is still the (provider, provider_id) pair. On a
// pre-existing link the profile is refreshed in place (the email a user sees
// in /auth/accounts should match what the IdP says now, not what it said at
// the first link).
func (s *EntityUserStore) LinkOAuthEnriched(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error {
	return s.linkOAuth(ctx, userID, provider, providerID, profile)
}

func (s *EntityUserStore) linkOAuth(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error {
	if userID == "" || provider == "" || providerID == "" {
		return errors.New("auth: LinkOAuth requires userID, provider, providerID")
	}
	q := fmt.Sprintf(
		"INSERT INTO %s (provider, provider_id, user_id, email, name, avatar_url) "+
			"VALUES ($1, $2, $3, $4, $5, $6) "+
			"ON CONFLICT (provider, provider_id) DO UPDATE SET "+
			"email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url",
		query.QuoteIdent(s.oauthLinksTable()),
	)
	_, err := s.db.ExecContext(ctx, q, provider, providerID, userID,
		nullable(profile.Email), nullable(profile.Name), nullable(profile.AvatarURL))
	return err
}

// ListAccounts returns every OAuth identity linked to userID, ordered
// deterministically by provider so the /auth/accounts UI is stable across
// calls. Implements AccountLister.
func (s *EntityUserStore) ListAccounts(ctx context.Context, userID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT provider, provider_id, email, name, avatar_url, created_at FROM %s "+
			"WHERE user_id = $1 ORDER BY provider",
		query.QuoteIdent(s.oauthLinksTable()),
	), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Account, 0)
	for rows.Next() {
		var (
			a         Account
			emailName sql.NullString
			name      sql.NullString
			avatar    sql.NullString
			createdAt sql.NullTime
		)
		if err := rows.Scan(&a.Provider, &a.ProviderID, &emailName, &name, &avatar, &createdAt); err != nil {
			return nil, err
		}
		a.Email = emailName.String
		a.Name = name.String
		a.AvatarURL = avatar.String
		if createdAt.Valid {
			t := createdAt.Time
			a.LinkedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UnlinkOAuth removes every link for (userID, provider). Deleting an absent
// link is not an error — the caller (AccountsPlugin) has already verified
// the link exists and that removing it leaves the user with a login method.
// Implements AccountUnlinker.
func (s *EntityUserStore) UnlinkOAuth(ctx context.Context, userID, provider string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2",
		query.QuoteIdent(s.oauthLinksTable()),
	), userID, provider)
	return err
}

// nullable wraps a string for SQL NULL semantics: empty → NULL, else the
// string. Keeps the table free of empty-string profile columns and lets
// ListAccounts distinguish "no avatar" from "empty avatar".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// countLinksForProvider is a test-accessible row count for a (provider,
// provider_id) pair — used to pin the race-safety invariant: two concurrent
// LinkOAuth calls for the same pair yield exactly one row.
func (s *EntityUserStore) countLinksForProvider(ctx context.Context, provider, providerID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE provider = $1 AND provider_id = $2",
		query.QuoteIdent(s.oauthLinksTable()),
	), provider, providerID).Scan(&n)
	return n, err
}

// Compile-time assertions that EntityUserStore satisfies the optional
// interfaces it now claims. These catch a method-signature drift at build
// time rather than at the first OAuth callback in production.
var (
	_ OAuthLinker         = (*EntityUserStore)(nil)
	_ OAuthEnrichedLinker = (*EntityUserStore)(nil)
	_ AccountLister       = (*EntityUserStore)(nil)
	_ AccountUnlinker     = (*EntityUserStore)(nil)
	_ PasswordChecker     = (*EntityUserStore)(nil)
	_ OAuthUserCreator    = (*EntityUserStore)(nil)
)
