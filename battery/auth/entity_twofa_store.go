package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"golang.org/x/crypto/bcrypt"
)


// EntityTwoFAStore is a TwoFAStore in a SQLite or PostgreSQL table, the TOTP secret sealed with AES-GCM.
type EntityTwoFAStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect
	sealer  *aeadSealer
	previousSealers []*aeadSealer

	casTestHook func()
}

// EntityTwoFAStoreConfig tunes the SQL-backed 2FA store.
type EntityTwoFAStoreConfig struct {
	// EncryptionKey seals the TOTP secret; required. Rows stored as plaintext before sealing still read.
	EncryptionKey []byte

	// PreviousEncryptionKeys open rows sealed under rotated-away keys; a read re-seals them under EncryptionKey.
	PreviousEncryptionKeys [][]byte
}

// NewEntityTwoFAStore creates a TwoFAStore backed by a database table.
func NewEntityTwoFAStore(db *sql.DB, table string, cfg EntityTwoFAStoreConfig) (*EntityTwoFAStore, error) {
	query.MustIdent(table)
	if len(cfg.EncryptionKey) == 0 {
		return nil, errors.New("auth: entity 2FA store: EncryptionKey is required (32 random bytes from a secret manager); the TOTP seed is stored sealed, never plaintext")
	}
	sealer, err := newAEADSealer(cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("auth: entity 2FA store: %w", err)
	}
	prev := make([]*aeadSealer, 0, len(cfg.PreviousEncryptionKeys))
	for i, k := range cfg.PreviousEncryptionKeys {
		if len(k) == 0 {
			return nil, fmt.Errorf("auth: entity 2FA store: PreviousEncryptionKeys[%d] is empty", i)
		}
		ps, err := newAEADSealer(k)
		if err != nil {
			return nil, fmt.Errorf("auth: entity 2FA store: PreviousEncryptionKeys[%d]: %w", i, err)
		}
		prev = append(prev, ps)
	}
	return &EntityTwoFAStore{db: db, table: table, sealer: sealer, previousSealers: prev}, nil
}

func (s *EntityTwoFAStore) sealSecret(secret string) (string, error) {
	if s.sealer == nil || secret == "" {
		return secret, nil
	}
	return s.sealer.seal(secret)
}

func (s *EntityTwoFAStore) openSecret(stored string) (plain string, viaPrevious bool, err error) {
	if stored == "" {
		return stored, false, nil
	}
	if pt, ok := s.sealer.open(stored); ok {
		return pt, false, nil
	}
	for _, prev := range s.previousSealers {
		if pt, ok := prev.open(stored); ok {
			return pt, true, nil
		}
	}
	if isLegacyPlaintextSeed(stored) {
		return stored, false, nil
	}
	return "", false, fmt.Errorf("auth: entity 2FA store: secret ciphertext failed to open (wrong EncryptionKey?)")
}

func isLegacyPlaintextSeed(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')) {
			return false
		}
	}
	return true
}

// EnsureSchema creates the table and adds columns an older table lacks.
func (s *EntityTwoFAStore) EnsureSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	boolType, boolFalse := "INTEGER", "0"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		boolType, boolFalse = "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (user_id TEXT PRIMARY KEY NOT NULL, enabled %s NOT NULL DEFAULT %s, secret TEXT NOT NULL DEFAULT '', backup_codes TEXT NOT NULL DEFAULT '[]', verified %s NOT NULL DEFAULT %s, last_used_step BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 0)",
		query.QuoteFolded(s.table), boolType, boolFalse, boolType, boolFalse,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	if err := ensurePostgresBoolColumns(ctx, s.db, s.dialect.of(s.db), s.table, "enabled", "verified"); err != nil {
		return err
	}
	if err := s.ensureBigIntColumn(ctx, "version"); err != nil {
		return err
	}
	return s.ensureBigIntColumn(ctx, "last_used_step")
}

func (s *EntityTwoFAStore) ensureBigIntColumn(ctx context.Context, col string) error {
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s BIGINT NOT NULL DEFAULT 0",
			query.QuoteFolded(s.table), query.QuoteFolded(col)))
		return err
	}
	has, err := s.sqliteHasColumn(ctx, col)
	if err != nil {
		return err
	}
	if !has {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s BIGINT NOT NULL DEFAULT 0",
			query.QuoteFolded(s.table), query.QuoteFolded(col))); err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityTwoFAStore) sqliteHasColumn(ctx context.Context, col string) (bool, error) {
	return sqliteTableHasColumn(ctx, s.db, s.table, col)
}

func sqliteTableHasColumn(ctx context.Context, db *sql.DB, table, col string) (bool, error) {
	rows, err := db.QueryContext(ctx, query.SQLitePragma("table_info", query.QuoteFolded(table)))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, col) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *EntityTwoFAStore) qTable(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteFolded(s.table))
}

// GetTwoFA returns the user's state, or nil when not enrolled.
func (s *EntityTwoFAStore) GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error) {
	return s.getWithVersion(ctx, userID, nil, nil)
}

func (s *EntityTwoFAStore) getWithVersion(ctx context.Context, userID string, version *int64, codesRaw *string) (*TwoFAState, error) {
	q := s.qTable("SELECT enabled, secret, backup_codes, verified, last_used_step, version FROM %s WHERE user_id = $1")
	var enabled, verified bool
	var secret, codesJSON string
	var lastUsedStep int64
	var ver int64
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&enabled, &secret, &codesJSON, &verified, &lastUsedStep, &ver)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if version != nil {
		*version = ver
	}
	if codesRaw != nil {
		*codesRaw = codesJSON
	}
	var codes []string
	if err := json.Unmarshal([]byte(codesJSON), &codes); err != nil {
		return nil, fmt.Errorf("auth: EntityTwoFAStore: corrupt backup_codes row for user %s: %w", userID, err)
	}
	plain, viaPrevious, err := s.openSecret(secret)
	if err != nil {
		return nil, err
	}
	if viaPrevious {
		s.resealUnderCurrentKey(ctx, userID, plain, ver, codesJSON, enabled, verified, lastUsedStep)
	}
	return &TwoFAState{
		Enabled:      enabled,
		Secret:       plain,
		BackupCodes:  codes,
		Verified:     verified,
		LastUsedStep: uint64(lastUsedStep),
	}, nil
}

func (s *EntityTwoFAStore) resealUnderCurrentKey(ctx context.Context, userID, plain string, ver int64, codesJSON string, enabled, verified bool, lastUsedStep int64) {
	sealed, err := s.sealSecret(plain)
	if err != nil {
		slog.Warn("2FA rotation drain: re-seal failed",
			"store", "entity_twofa", "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	q := s.qTable("UPDATE %s SET secret = $1 WHERE user_id = $2 AND version = $3 AND backup_codes = $4 AND enabled = $5 AND verified = $6 AND last_used_step = $7")
	res, err := s.db.ExecContext(ctx, q, sealed, userID, ver, codesJSON, enabled, verified, lastUsedStep)
	if err != nil {
		slog.Warn("2FA rotation drain: re-seal write failed",
			"store", "entity_twofa", "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	slog.Info("2FA rotation drain: row re-sealed under current key",
		"store", "entity_twofa", "user_hash", hashedIdentifier(userID))
}

func marshalBackupCodes(codes []string) (string, error) {
	if codes == nil {
		codes = []string{}
	}
	b, err := json.Marshal(codes)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SetTwoFA upserts the state; nil deletes it.
func (s *EntityTwoFAStore) SetTwoFA(ctx context.Context, userID string, state *TwoFAState) error {
	if state == nil {
		return s.DeleteTwoFA(ctx, userID)
	}
	codesJSON, err := marshalBackupCodes(state.BackupCodes)
	if err != nil {
		return err
	}
	secret, err := s.sealSecret(state.Secret)
	if err != nil {
		return err
	}
	start, err := freshTwoFAVersion()
	if err != nil {
		return err
	}
	tbl := query.QuoteFolded(s.table)
	q := fmt.Sprintf("INSERT INTO %s (user_id, enabled, secret, backup_codes, verified, last_used_step, version) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled, secret = excluded.secret, backup_codes = excluded.backup_codes, verified = excluded.verified, last_used_step = excluded.last_used_step, version = %s.version + 1", tbl, tbl)
	_, err = s.db.ExecContext(ctx, q, userID, state.Enabled, secret, codesJSON, state.Verified, int64(state.LastUsedStep), start)
	return err
}

func freshTwoFAVersion() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])>>2) | 1, nil
}

// CompareAndSwapTwoFA implements TwoFAStateSwapper with a predicate on the version and every compared column.
func (s *EntityTwoFAStore) CompareAndSwapTwoFA(ctx context.Context, userID string, expect, next *TwoFAState) (bool, error) {
	if expect == nil {
		if next == nil {
			return true, nil
		}
		codesJSON, err := marshalBackupCodes(next.BackupCodes)
		if err != nil {
			return false, err
		}
		secret, err := s.sealSecret(next.Secret)
		if err != nil {
			return false, err
		}
		start, err := freshTwoFAVersion()
		if err != nil {
			return false, err
		}
		tbl := query.QuoteFolded(s.table)
		q := fmt.Sprintf("INSERT INTO %s (user_id, enabled, secret, backup_codes, verified, last_used_step, version) SELECT $1, $2, $3, $4, $5, $6, $7 WHERE NOT EXISTS (SELECT 1 FROM %s WHERE user_id = $1)", tbl, tbl)
		res, err := s.db.ExecContext(ctx, q, userID, next.Enabled, secret, codesJSON, next.Verified, int64(next.LastUsedStep), start)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n == 1, nil
	}

	var version int64
	var rawCodes string
	cur, err := s.getWithVersion(ctx, userID, &version, &rawCodes)
	if err != nil {
		return false, err
	}
	if cur == nil || !twoFAStateEqual(cur, expect) {
		return false, nil
	}
	if s.casTestHook != nil {
		s.casTestHook()
	}
	if next == nil {
		q := s.qTable("DELETE FROM %s WHERE user_id = $1 AND version = $2 AND backup_codes = $3 AND enabled = $4 AND verified = $5 AND last_used_step = $6")
		res, err := s.db.ExecContext(ctx, q, userID, version, rawCodes, expect.Enabled, expect.Verified, int64(expect.LastUsedStep))
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n == 1, nil
	}
	codesJSON, err := marshalBackupCodes(next.BackupCodes)
	if err != nil {
		return false, err
	}
	secret, err := s.sealSecret(next.Secret)
	if err != nil {
		return false, err
	}
	q := s.qTable("UPDATE %s SET enabled = $1, secret = $2, backup_codes = $3, verified = $4, last_used_step = $5, version = version + 1 WHERE user_id = $6 AND version = $7 AND backup_codes = $8 AND enabled = $9 AND verified = $10 AND last_used_step = $11")
	res, err := s.db.ExecContext(ctx, q, next.Enabled, secret, codesJSON, next.Verified, int64(next.LastUsedStep), userID, version, rawCodes, expect.Enabled, expect.Verified, int64(expect.LastUsedStep))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DeleteTwoFA removes the state; an absent row is not an error.
func (s *EntityTwoFAStore) DeleteTwoFA(ctx context.Context, userID string) error {
	q := s.qTable("DELETE FROM %s WHERE user_id = $1")
	_, err := s.db.ExecContext(ctx, q, userID)
	return err
}

// ConsumeBackupCode removes one matching backup code with a compare-and-swap, so concurrent replicas spend a code once.
func (s *EntityTwoFAStore) ConsumeBackupCode(ctx context.Context, userID string, code string) (bool, error) {
	maxRounds := 8
	for round := 0; ; round++ {
		var version int64
		var rawCodes string
		state, err := s.getWithVersion(ctx, userID, &version, &rawCodes)
		if err != nil {
			return false, err
		}
		if state == nil || len(state.BackupCodes) == 0 {
			return false, nil
		}
		if round == 0 {
			maxRounds = len(state.BackupCodes) + 2
		}
		if round >= maxRounds {
			return false, nil
		}

		if s.casTestHook != nil {
			s.casTestHook()
		}

		matched := -1
		for i, hashed := range state.BackupCodes {
			if bcrypt.CompareHashAndPassword([]byte(hashed), []byte(code)) == nil {
				matched = i
				break
			}
		}
		if matched == -1 {
			return false, nil
		}

		remaining := append(append([]string{}, state.BackupCodes[:matched]...), state.BackupCodes[matched+1:]...)
		newJSON, err := json.Marshal(remaining)
		if err != nil {
			return false, err
		}
		q := s.qTable("UPDATE %s SET backup_codes = $1, version = version + 1 WHERE user_id = $2 AND version = $3 AND backup_codes = $4")
		res, err := s.db.ExecContext(ctx, q, string(newJSON), userID, version, rawCodes)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return true, nil
		}
	}
}

// TwoFAEntityFields returns the 2FA table's fields for the entity system, the secret and codes hidden.
func TwoFAEntityFields() []schema.Field {
	return []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true, Unique: true},
		{Name: "enabled", Type: schema.Bool, Default: "false"},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "backup_codes", Type: schema.Text, Hidden: true},
		{Name: "verified", Type: schema.Bool, Default: "false"},
		{Name: "version", Type: schema.Int, RawType: "BIGINT", Default: 0, ReadOnly: true, Hidden: true},
	}
}
