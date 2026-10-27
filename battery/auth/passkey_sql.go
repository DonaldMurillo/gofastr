package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// SQLPasskeyStore is the database-backed PasskeyStore: passkeys survive
// restarts and every replica sees the same set.
//
// Schema (created by EnsureSchema, which PasskeyPlugin.Init calls): the
// credential id and COSE public key as unpadded base64url TEXT, the sign
// counter and timestamps as BIGINT (unix seconds; 0 is "never"), and the
// backup flags as 0/1 INTEGER, so one DDL runs on SQLite and Postgres.
// A public key is not a secret, so nothing is sealed.
type SQLPasskeyStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect
}

// NewSQLPasskeyStore returns a store on table (default "auth_passkeys").
func NewSQLPasskeyStore(db *sql.DB, table ...string) (*SQLPasskeyStore, error) {
	t := "auth_passkeys"
	if len(table) > 0 && table[0] != "" {
		t = table[0]
	}
	if _, err := query.SafeIdent(t); err != nil {
		return nil, fmt.Errorf("auth: passkey table %q: %w", t, err)
	}
	return &SQLPasskeyStore{db: db, table: t}, nil
}

// EnsureSchema creates the table and its user index if absent. The table
// is the folded name erasure and export read, as for the other auth stores.
func (s *SQLPasskeyStore) EnsureSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	t := query.QuoteFolded(s.table)
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, public_key TEXT NOT NULL, alg BIGINT NOT NULL, sign_count BIGINT NOT NULL DEFAULT 0, name TEXT NOT NULL DEFAULT '', backup_eligible INTEGER NOT NULL DEFAULT 0, backed_up INTEGER NOT NULL DEFAULT 0, created_at BIGINT NOT NULL, last_used_at BIGINT NOT NULL DEFAULT 0)`, t),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s (user_id)`,
			query.IndexOn(s.dialect.of(s.db) == migrate.DialectSQLite, indexName("", s.table, "_user_id"), t)),
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

const passkeyColumns = `id, user_id, public_key, alg, sign_count, name, backup_eligible, backed_up, created_at, last_used_at`

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// AddPasskey implements PasskeyStore.
func (s *SQLPasskeyStore) AddPasskey(ctx context.Context, c PasskeyCredential) error {
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, query.QuoteFolded(s.table), passkeyColumns)
	_, err := s.db.ExecContext(ctx, q,
		b64url.EncodeToString(c.ID), c.UserID, b64url.EncodeToString(c.PublicKey), c.Algorithm, int64(c.SignCount),
		c.Name, boolInt(c.BackupEligible), boolInt(c.BackedUp), unixOrZero(c.CreatedAt), unixOrZero(c.LastUsedAt))
	if err != nil && isUniqueViolation(err) {
		return ErrPasskeyExists
	}
	return err
}

func scanPasskey(row rowScanner) (PasskeyCredential, error) {
	var id, key string
	var count, created, used int64
	var be, bs int
	var c PasskeyCredential
	if err := row.Scan(&id, &c.UserID, &key, &c.Algorithm, &count, &c.Name, &be, &bs, &created, &used); err != nil {
		return PasskeyCredential{}, err
	}
	var err error
	if c.ID, err = b64url.DecodeString(id); err != nil {
		return PasskeyCredential{}, fmt.Errorf("passkey row id: %w", err)
	}
	if c.PublicKey, err = b64url.DecodeString(key); err != nil {
		return PasskeyCredential{}, fmt.Errorf("passkey row public key: %w", err)
	}
	if count < 0 || count > 1<<32-1 {
		return PasskeyCredential{}, fmt.Errorf("passkey row sign count %d out of range", count)
	}
	c.SignCount = uint32(count)
	c.BackupEligible, c.BackedUp = be != 0, bs != 0
	c.CreatedAt, c.LastUsedAt = timeOrZero(created), timeOrZero(used)
	return c, nil
}

// GetPasskey implements PasskeyStore.
func (s *SQLPasskeyStore) GetPasskey(ctx context.Context, id []byte) (PasskeyCredential, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE id = $1`, passkeyColumns, query.QuoteFolded(s.table))
	c, err := scanPasskey(s.db.QueryRowContext(ctx, q, b64url.EncodeToString(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return PasskeyCredential{}, ErrPasskeyNotFound
	}
	return c, err
}

// ListPasskeys implements PasskeyStore, oldest first.
func (s *SQLPasskeyStore) ListPasskeys(ctx context.Context, userID string) ([]PasskeyCredential, error) {
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE user_id = $1 ORDER BY created_at, id`, passkeyColumns, query.QuoteFolded(s.table))
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PasskeyCredential
	for rows.Next() {
		c, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecordPasskeyUse implements PasskeyStore as one conditional UPDATE.
func (s *SQLPasskeyStore) RecordPasskeyUse(ctx context.Context, id []byte, old, next uint32, backedUp bool, at time.Time) (bool, error) {
	q := fmt.Sprintf(`UPDATE %s SET sign_count = $1, backed_up = $2, last_used_at = $3 WHERE id = $4 AND sign_count = $5`, query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, q, int64(next), boolInt(backedUp), unixOrZero(at), b64url.EncodeToString(id), int64(old))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DeletePasskey implements PasskeyStore.
func (s *SQLPasskeyStore) DeletePasskey(ctx context.Context, userID string, id []byte) (bool, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND user_id = $2`, query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, q, b64url.EncodeToString(id), userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DeletePasskeysForUser implements PasskeyStore.
func (s *SQLPasskeyStore) DeletePasskeysForUser(ctx context.Context, userID string) (int, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE user_id = $1`, query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, q, userID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
