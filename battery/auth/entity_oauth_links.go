package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)


func (s *EntityUserStore) oauthLinksTable() string {
	return s.table + "_oauth_links"
}

// EnsureOAuthLinksSchema creates the provider-link table and its user_id index.
func (s *EntityUserStore) EnsureOAuthLinksSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.oauthLinksTable()); err != nil {
		return err
	}
	tsType := "DATETIME"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
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
		query.QuoteFolded(s.oauthLinksTable()),
		tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s (%s)",
		query.IndexOn(s.dialect.of(s.db) == migrate.DialectSQLite,
			indexName("", s.oauthLinksTable(), "_user_idx"), query.QuoteFolded(s.oauthLinksTable())),
		query.QuoteFolded("user_id"),
	)
	if _, err := s.db.ExecContext(ctx, idx); err != nil {
		return err
	}
	return nil
}

// FindByOAuth returns the user linked to (provider, providerID), or ErrUserNotFound. Implements OAuthLinker.
func (s *EntityUserStore) FindByOAuth(ctx context.Context, provider, providerID string) (User, error) {
	if provider == "" || providerID == "" {
		return nil, ErrUserNotFound
	}
	var userID string
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT user_id FROM %s WHERE provider = $1 AND provider_id = $2",
		query.QuoteFolded(s.oauthLinksTable()),
	), provider, providerID).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.FindByID(ctx, userID)
}

// LinkOAuth binds (provider, providerID) to userID; an existing binding keeps its owner. Implements OAuthLinker.
func (s *EntityUserStore) LinkOAuth(ctx context.Context, userID, provider, providerID string) error {
	return s.linkOAuth(ctx, userID, provider, providerID, OAuthAccountProfile{})
}

// LinkOAuthEnriched is LinkOAuth plus the profile snapshot at link time.
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
		query.QuoteFolded(s.oauthLinksTable()),
	)
	_, err := s.db.ExecContext(ctx, q, provider, providerID, userID,
		nullable(profile.Email), nullable(profile.Name), nullable(profile.AvatarURL))
	return err
}

// RefreshOAuthProfile updates the profile of a link bound to userID and never inserts one. Implements OAuthProfileRefresher.
func (s *EntityUserStore) RefreshOAuthProfile(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET email = $1, name = $2, avatar_url = $3 "+
			"WHERE provider = $4 AND provider_id = $5 AND user_id = $6",
		query.QuoteFolded(s.oauthLinksTable()),
	), nullable(profile.Email), nullable(profile.Name), nullable(profile.AvatarURL), provider, providerID, userID)
	return err
}

// ListAccounts returns userID's linked identities ordered by provider. Implements AccountLister.
func (s *EntityUserStore) ListAccounts(ctx context.Context, userID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT provider, provider_id, email, name, avatar_url, created_at FROM %s "+
			"WHERE user_id = $1 ORDER BY provider",
		query.QuoteFolded(s.oauthLinksTable()),
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

// UnlinkOAuth removes userID's links for provider; an absent link is not an error. Implements AccountUnlinker.
func (s *EntityUserStore) UnlinkOAuth(ctx context.Context, userID, provider string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2",
		query.QuoteFolded(s.oauthLinksTable()),
	), userID, provider)
	return err
}

// UnlinkOAuthGuarded removes the link unless it is the user's last login method, atomically. Implements AtomicUnlinker.
func (s *EntityUserStore) UnlinkOAuthGuarded(ctx context.Context, userID, provider string) (UnlinkOutcome, error) {
	links := query.QuoteFolded(s.oauthLinksTable())
	users := query.QuoteFolded(s.table)
	if s.dialect.of(s.db) == migrate.DialectPostgres {
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
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE user_id = $1 AND provider = $2 AND (EXISTS (SELECT 1 FROM %s l WHERE l.user_id = $1 AND l.provider <> $2) OR EXISTS (SELECT 1 FROM %s u WHERE u.%s = $1 AND u.%s = 1))",
		links, links, users,
		query.QuoteFolded(s.fieldMap.ID),
		query.QuoteFolded(s.fieldMap.PasswordSet),
	), userID, provider)
	if err != nil {
		return UnlinkNotLinked, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return UnlinkRemoved, nil
	}
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

func (s *EntityUserStore) txHasPassword(ctx context.Context, tx *sql.Tx, users, userID string) (bool, error) {
	var hasPW bool
	err := tx.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1",
		query.QuoteFolded(s.fieldMap.PasswordSet), users,
		query.QuoteFolded(s.fieldMap.ID),
	), userID).Scan(&hasPW)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return hasPW, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *EntityUserStore) countLinksForProvider(ctx context.Context, provider, providerID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE provider = $1 AND provider_id = $2",
		query.QuoteFolded(s.oauthLinksTable()),
	), provider, providerID).Scan(&n)
	return n, err
}

var (
	_ OAuthLinker         = (*EntityUserStore)(nil)
	_ OAuthEnrichedLinker = (*EntityUserStore)(nil)
	_ AccountLister       = (*EntityUserStore)(nil)
	_ AccountUnlinker     = (*EntityUserStore)(nil)
	_ PasswordChecker     = (*EntityUserStore)(nil)
	_ OAuthUserCreator    = (*EntityUserStore)(nil)
)
