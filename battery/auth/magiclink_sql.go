package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
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
//
// Anonymous writes mint rows: every POST /auth/magic-link/send,
// /auth/forgot-password or /auth/verify-email request stores a token whether
// or not it is ever redeemed, so the mint path reaps expired rows itself
// (amortized, magicLinkSweepInterval) instead of relying on a host-run
// Cleanup that nothing schedules — the SQLRateLimitStore.Allow posture.
type SQLMagicLinkTokenStore struct {
	db    *sql.DB
	table string

	// sweepMu + lastSweep amortize the expired-row sweep on the mint
	// path: at most one DELETE per magicLinkSweepInterval, shared by
	// every replica that talks to the same database only in the sense
	// that each keeps its own timer (the DELETE is idempotent, an
	// over-eager sweep costs one extra statement, never a lost token).
	sweepMu   sync.Mutex
	lastSweep time.Time
}

// magicLinkSweepInterval is how often the mint path reaps expired rows.
// It matches the default TokenTTL (15 minutes): the longest an abandoned
// row lingers after any later mint is one TTL window.
const magicLinkSweepInterval = 15 * time.Minute

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

// DeleteTokensForPayload removes every unredeemed token carrying payload.
// Implements [MagicLinkTokenPurger].
func (s *SQLMagicLinkTokenStore) DeleteTokensForPayload(ctx context.Context, payload string) (int, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE email = $1`, query.QuoteIdent(s.table))
	res, err := s.db.ExecContext(ctx, q, payload)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// CreateToken generates a cryptographically random token, persists it with the
// email and TTL, and returns it.
//
// Every mint also reaps expired rows, at most once per
// magicLinkSweepInterval: this table's growth surface is anonymous
// requests (magic-link send, password reset, email verification), so the
// write path owns its own garbage collection. The sweep runs AFTER the
// insert, so an already-expired row (ttl <= 0, the shape tests use to
// mint staleness) is swept by its own mint too.
func (s *SQLMagicLinkTokenStore) CreateToken(ctx context.Context, email string, ttl time.Duration) (string, error) {
	s.sweepMu.Lock()
	sweep := time.Since(s.lastSweep) >= magicLinkSweepInterval
	if sweep {
		s.lastSweep = time.Now()
	}
	s.sweepMu.Unlock()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	// The column stores sha256hex(token), never the token (the APIToken
	// grammar): the minted value is a password-equivalent takeover
	// credential (magic-link login, password reset, email verification
	// share this table), so a read-only dump of it must not be a live
	// redemption kit. Lookup keys on the digest; a presented token is
	// hashed then matched, uniform timing.
	q := fmt.Sprintf(`INSERT INTO %s (token, email, expires_at) VALUES ($1, $2, $3)`, query.QuoteIdent(s.table))
	if _, err := s.db.ExecContext(ctx, q, sha256hex(token), email, time.Now().Add(ttl).Unix()); err != nil {
		return "", err
	}
	if sweep {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteIdent(s.table)),
			time.Now().Unix()); err != nil {
			return "", fmt.Errorf("sweep expired tokens: %w", err)
		}
	}
	return token, nil
}

// RedeemToken atomically consumes a token (single-use) via DELETE … RETURNING,
func (s *SQLMagicLinkTokenStore) RedeemToken(ctx context.Context, token string) (string, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE token = $1 RETURNING email, expires_at`, query.QuoteIdent(s.table))
	var email string
	var exp int64
	err := s.db.QueryRowContext(ctx, q, sha256hex(token)).Scan(&email, &exp)
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

// PeekToken reads a token's email without consuming it. Implements
// [MagicLinkTokenPeeker] for the confirmation page.
func (s *SQLMagicLinkTokenStore) PeekToken(ctx context.Context, token string) (string, error) {
	q := fmt.Sprintf(`SELECT email, expires_at FROM %s WHERE token = $1`, query.QuoteIdent(s.table))
	var email string
	var exp int64
	err := s.db.QueryRowContext(ctx, q, sha256hex(token)).Scan(&email, &exp)
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
