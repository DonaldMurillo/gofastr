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
// the configured user table via the "<table>_oauth_links" convention, hosts
// that pick a custom users table get a matching links table for free, and no
// two EntityUserStore instances on the same DB collide on the link table.
//
// All four methods here are the durable implementations of the optional
// interfaces declared in accounts.go (OAuthLinker, OAuthEnrichedLinker,
// AccountLister, AccountUnlinker). EntityUserStore is now a linker by
// default, production OAuth login requires it (see OAuth2Plugin.Init).

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
	// user_id index powers ListAccounts / UnlinkOAuth, without it both
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
// The lookup is two-step, read the user_id from the link table, then read
// the user, so the link table stays narrow and the user row stays the single
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
// this path, rebinding an identity to a different local account is an admin
// operation, not an OAuth callback. Implements OAuthLinker.
func (s *EntityUserStore) LinkOAuth(ctx context.Context, userID, provider, providerID string) error {
	return s.linkOAuth(ctx, userID, provider, providerID, OAuthAccountProfile{})
}

// LinkOAuthEnriched is LinkOAuth plus the profile snapshot at link time.
// Implements OAuthEnrichedLinker. The profile fields are informational, the
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
// link is not an error, the caller (AccountsPlugin) has already verified
// the link exists and that removing it leaves the user with a login method.
// Implements AccountUnlinker.
func (s *EntityUserStore) UnlinkOAuth(ctx context.Context, userID, provider string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2",
		query.QuoteIdent(s.oauthLinksTable()),
	), userID, provider)
	return err
}

// UnlinkOAuthGuarded removes the provider link unless that would leave the
// user with no login method, deciding and deleting in one atomic operation
// so the refuse-the-last invariant holds under concurrent unlinks.
// Implements AtomicUnlinker.
//
// On PostgreSQL the count, the password check, and the delete run in one
// transaction whose first statement locks the user's link rows (SELECT …
// FOR UPDATE): a concurrent unlink of the sibling provider blocks there,
// then re-reads the post-commit state and refuses. On SQLite (and any
// other dialect) the whole decision is one conditional DELETE — a single
// statement is atomic in SQLite, and the writers-serialize lock model
// makes the subquery and the delete one indivisible step — followed by a
// read-back that only distinguishes NotLinked from RefusedLast for the
// response code; the guard itself never depends on it.
func (s *EntityUserStore) UnlinkOAuthGuarded(ctx context.Context, userID, provider string) (UnlinkOutcome, error) {
	links := query.QuoteIdent(s.oauthLinksTable())
	users := query.QuoteIdent(s.table)
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		return s.unlinkGuardedPostgres(ctx, userID, provider, links, users)
	}
	return s.unlinkGuardedSingleStatement(ctx, userID, provider, links, users)
}

func (s *EntityUserStore) unlinkGuardedPostgres(ctx context.Context, userID, provider, links, users string) (UnlinkOutcome, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UnlinkNotLinked, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		"SELECT provider FROM %s WHERE user_id = $1 FOR UPDATE", links,
	), userID)
	if err != nil {
		return UnlinkNotLinked, err
	}
	var providers []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return UnlinkNotLinked, err
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return UnlinkNotLinked, err
	}
	rows.Close()

	linked := false
	otherRemains := false
	for _, p := range providers {
		if p == provider {
			linked = true
		} else {
			otherRemains = true
		}
	}
	if !linked {
		return UnlinkNotLinked, nil
	}
	if !otherRemains {
		hasPW, err := s.txHasPassword(ctx, tx, users, userID)
		if err != nil {
			return UnlinkNotLinked, err
		}
		if !hasPW {
			return UnlinkRefusedLast, nil
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2", links,
	), userID, provider); err != nil {
		return UnlinkNotLinked, err
	}
	if err := tx.Commit(); err != nil {
		return UnlinkNotLinked, err
	}
	return UnlinkRemoved, nil
}

func (s *EntityUserStore) unlinkGuardedSingleStatement(ctx context.Context, userID, provider, links, users string) (UnlinkOutcome, error) {
	// The guard rides inside the DELETE: the row is removed only when
	// another link for the user remains, or the user's row reports a
	// real password. RowsAffected decides.
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2 AND (EXISTS (SELECT 1 FROM %s l WHERE l.user_id = $1 AND l.provider <> $2) OR EXISTS (SELECT 1 FROM %s u WHERE u.%s = $1 AND u.%s = 1))",
		links, links, users,
		query.QuoteIdent(s.fieldMap.ID),
		query.QuoteIdent(s.fieldMap.PasswordSet),
	), userID, provider)
	if err != nil {
		return UnlinkNotLinked, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return UnlinkRemoved, nil
	}
	// Zero rows: either the link was never there (404) or the guard
	// refused it (409). Read-back is for the status code only — the
	// guard above is already decided and atomic.
	var linked bool
	err = s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT EXISTS (SELECT 1 FROM %s WHERE user_id = $1 AND provider = $2)", links,
	), userID, provider).Scan(&linked)
	if err != nil {
		return UnlinkNotLinked, err
	}
	if linked {
		return UnlinkRefusedLast, nil
	}
	return UnlinkNotLinked, nil
}

// txHasPassword reads the password_set flag on the given transaction so
// the PG guarded unlink decides on the same locked state it deletes in.
func (s *EntityUserStore) txHasPassword(ctx context.Context, tx *sql.Tx, users, userID string) (bool, error) {
	var hasPW bool
	err := tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1",
		query.QuoteIdent(s.fieldMap.PasswordSet), users,
		query.QuoteIdent(s.fieldMap.ID),
	), userID).Scan(&hasPW)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return hasPW, err
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
// provider_id) pair, used to pin the race-safety invariant: two concurrent
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
