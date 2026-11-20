package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLMagicLinkTokenStore is a MagicLinkTokenStore in a SQLite or PostgreSQL table, storing token digests.
type SQLMagicLinkTokenStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect

	sweepMu   sync.Mutex
	lastSweep time.Time
}

// NewSQLMagicLinkTokenStore creates the table (default magic_link_tokens) and returns the store.
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
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	q := fmt.Sprintf(
		//gofastr:allow(GOFASTR1415) erase.go erases these rows; export.go leaves them out on purpose, since a restored row is a live login or reset link
		`CREATE TABLE IF NOT EXISTS %s (token TEXT PRIMARY KEY NOT NULL, email TEXT NOT NULL, expires_at BIGINT NOT NULL)`,
		query.QuoteFolded(s.table),
	)
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// DeleteTokensForPayload removes every unredeemed token carrying payload.
func (s *SQLMagicLinkTokenStore) DeleteTokensForPayload(ctx context.Context, payload string) (int, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE email = $1`, query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, q, payload)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// CreateToken stores email under a new random token valid for ttl.
func (s *SQLMagicLinkTokenStore) CreateToken(ctx context.Context, email string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	q := fmt.Sprintf(`INSERT INTO %s (token, email, expires_at) VALUES ($1, $2, $3)`, query.QuoteFolded(s.table))
	if _, err := s.db.ExecContext(ctx, q, sha256hex(token), email, time.Now().Add(ttl).Unix()); err != nil {
		return "", err
	}
	s.sweepExpiredOnCreate(ctx)
	return token, nil
}

func (s *SQLMagicLinkTokenStore) sweepExpiredOnCreate(ctx context.Context) {
	now := time.Now()
	s.sweepMu.Lock()
	if now.Sub(s.lastSweep) < magicLinkSweepInterval {
		s.sweepMu.Unlock()
		return
	}
	s.lastSweep = now
	s.sweepMu.Unlock()
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteFolded(s.table)),
		now.Unix()); err != nil {
		slog.Warn("auth: expired magic-link token sweep failed", "err", err)
		s.sweepMu.Lock()
		if s.lastSweep.Equal(now) {
			s.lastSweep = now.Add(magicLinkSweepRetry - magicLinkSweepInterval)
		}
		s.sweepMu.Unlock()
	}
}

const magicLinkSweepRetry = time.Minute

// RedeemToken atomically consumes a token (single-use) via DELETE … RETURNING,
func (s *SQLMagicLinkTokenStore) RedeemToken(ctx context.Context, token string) (string, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE token = $1 RETURNING email, expires_at`, query.QuoteFolded(s.table))
	return s.tokenEmailFor(ctx, q, token)
}

// PeekToken returns a live token's payload without consuming it.
func (s *SQLMagicLinkTokenStore) PeekToken(ctx context.Context, token string) (string, error) {
	q := fmt.Sprintf(`SELECT email, expires_at FROM %s WHERE token = $1`, query.QuoteFolded(s.table))
	return s.tokenEmailFor(ctx, q, token)
}

func (s *SQLMagicLinkTokenStore) tokenEmailFor(ctx context.Context, q, token string) (string, error) {
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
	q := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, q, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
