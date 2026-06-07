package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLMagicLinkTokenStore is a database-backed MagicLinkTokenStore. Unlike
// MemoryMagicLinkTokenStore, tokens survive process restarts and are shared
// across replicas, so magic-link / passwordless login works in a horizontally
// scaled deployment. Tokens are single-use and time-limited.
//
// Schema (created on construction): a table with a TEXT primary-key token, the
// associated email, and an expiry stored as a unix timestamp (portable across
// SQLite and Postgres without time-format ambiguity).
type SQLMagicLinkTokenStore struct {
	db    *sql.DB
	table string
}

// NewSQLMagicLinkTokenStore creates the token table (IF NOT EXISTS) and returns
// the store. Pass an optional table name; defaults to "magic_link_tokens".
func NewSQLMagicLinkTokenStore(db *sql.DB, table ...string) (*SQLMagicLinkTokenStore, error) {
	t := "magic_link_tokens"
	if len(table) > 0 && table[0] != "" {
		t = table[0]
	}
	if _, err := query.SafeIdent(t); err != nil {
		return nil, fmt.Errorf("auth: magic-link token table %q: %w", t, err)
	}
	s := &SQLMagicLinkTokenStore{db: db, table: t}
	if err := s.ensureTable(context.Background()); err != nil {
		return nil, fmt.Errorf("auth: create magic-link token table: %w", err)
	}
	return s, nil
}

func (s *SQLMagicLinkTokenStore) ensureTable(ctx context.Context) error {
	q := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (token TEXT PRIMARY KEY, email TEXT NOT NULL, expires_at BIGINT NOT NULL)`,
		query.QuoteIdent(s.table),
	)
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// CreateToken generates a cryptographically random token, persists it with the
// email and TTL, and returns it.
func (s *SQLMagicLinkTokenStore) CreateToken(ctx context.Context, email string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	q := fmt.Sprintf(`INSERT INTO %s (token, email, expires_at) VALUES ($1, $2, $3)`, query.QuoteIdent(s.table))
	if _, err := s.db.ExecContext(ctx, q, token, email, time.Now().Add(ttl).Unix()); err != nil {
		return "", err
	}
	return token, nil
}

// RedeemToken atomically consumes a token (single-use) via DELETE … RETURNING,
// returning the associated email. Returns ErrTokenNotFound when the token is
// unknown, already redeemed, or expired — the row is removed regardless so a
// known-but-expired token can never be reused.
func (s *SQLMagicLinkTokenStore) RedeemToken(ctx context.Context, token string) (string, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE token = $1 RETURNING email, expires_at`, query.QuoteIdent(s.table))
	var email string
	var exp int64
	err := s.db.QueryRowContext(ctx, q, token).Scan(&email, &exp)
	if err == sql.ErrNoRows {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", err
	}
	if time.Now().Unix() > exp {
		return "", ErrTokenNotFound
	}
	return email, nil
}

// Cleanup deletes expired tokens and returns the count removed.
func (s *SQLMagicLinkTokenStore) Cleanup(ctx context.Context) (int, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteIdent(s.table))
	res, err := s.db.ExecContext(ctx, q, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
