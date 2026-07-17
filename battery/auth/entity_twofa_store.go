package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"golang.org/x/crypto/bcrypt"
)

// Placeholder invariant: every statement below lists $1..$N in ascending
// order of first textual appearance. lib/pq binds $N positionally, but
// mattn/go-sqlite3 treats $N as a NAMED param indexed by first occurrence
// — so a statement that mentions $2 before $1 would bind args correctly on
// Postgres yet misbind on SQLite. Keep new SQL in ascending placeholder
// order (and lean on the Postgres tests to catch regressions).

// EntityTwoFAStore adapts a database table to the TwoFAStore interface —
// the durable sibling of MemoryTwoFAStore, mirroring EntitySessionStore.
// Without it, a restart (or a second replica) silently reverts every
// enrolled 2FA account to password-only auth, because enrollment lives
// only in process memory.
//
// Usage:
//
//	mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{
//	    Store: auth.NewEntityTwoFAStore(db, "auth_twofa"),
//	}))
//
// The plugin calls EnsureSchema at Init, so hosts never hand-roll the DDL.
type EntityTwoFAStore struct {
	db    *sql.DB
	table string
}

// NewEntityTwoFAStore creates a TwoFAStore backed by a database table.
// Panics if the table name contains unsafe characters.
func NewEntityTwoFAStore(db *sql.DB, table string) *EntityTwoFAStore {
	query.MustIdent(table)
	return &EntityTwoFAStore{db: db, table: table}
}

// EnsureSchema creates the 2FA table if it does not already exist. Called
// by TwoFAPlugin.Init so hosts never hand-roll the DDL. Idempotent. The
// boolean column type is chosen per dialect (SQLite vs PostgreSQL) so the
// same battery boots on either.
func (s *EntityTwoFAStore) EnsureSchema(ctx context.Context) error {
	boolType, boolFalse := "INTEGER", "0"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		boolType, boolFalse = "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (user_id TEXT PRIMARY KEY, enabled %s NOT NULL DEFAULT %s, secret TEXT NOT NULL DEFAULT '', backup_codes TEXT NOT NULL DEFAULT '[]', verified %s NOT NULL DEFAULT %s, version BIGINT NOT NULL DEFAULT 0)",
		query.QuoteIdent(s.table), boolType, boolFalse, boolType, boolFalse,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	if err := ensurePostgresBoolColumns(ctx, s.db, s.table, "enabled", "verified"); err != nil {
		return err
	}
	// Self-heal a table created before the version column existed (or by a
	// host that auto-migrated an older field set): CREATE TABLE IF NOT
	// EXISTS is a no-op on an existing table, so the column would be missing
	// and every optimistic-CAS query would error. Add it if absent.
	return s.ensureVersionColumn(ctx)
}

// ensureVersionColumn adds the version column to a pre-existing table that
// lacks it. On Postgres it uses ADD COLUMN IF NOT EXISTS, which is both
// schema-correct (resolved via search_path, so it can't be fooled by a
// same-named table in another schema) and race-safe (concurrent boots on a
// shared DB don't collide). SQLite has no ADD COLUMN IF NOT EXISTS, so it
// checks PRAGMA table_info first; a SQLite file is process-local so the
// check-then-add is not a multi-replica concern.
func (s *EntityTwoFAStore) ensureVersionColumn(ctx context.Context) error {
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0",
			query.QuoteIdent(s.table)))
		return err
	}
	has, err := s.sqliteHasColumn(ctx, "version")
	if err != nil {
		return err
	}
	if !has {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN version BIGINT NOT NULL DEFAULT 0",
			query.QuoteIdent(s.table))); err != nil {
			return err
		}
	}
	return nil
}

// sqliteHasColumn reports whether the store's table has the named column,
// via PRAGMA table_info (SQLite-only).
func (s *EntityTwoFAStore) sqliteHasColumn(ctx context.Context, col string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", query.QuoteIdent(s.table)))
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
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// qTable wraps a statement template with the validated table name.
func (s *EntityTwoFAStore) qTable(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteIdent(s.table))
}

// GetTwoFA retrieves the 2FA state for a user. Returns nil (not an error)
// when the user is not enrolled, matching MemoryTwoFAStore.
func (s *EntityTwoFAStore) GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error) {
	q := s.qTable("SELECT enabled, secret, backup_codes, verified FROM %s WHERE user_id = $1")
	var enabled, verified bool
	var secret, codesJSON string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&enabled, &secret, &codesJSON, &verified)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal([]byte(codesJSON), &codes); err != nil {
		return nil, fmt.Errorf("auth: EntityTwoFAStore: corrupt backup_codes row for user %s: %w", userID, err)
	}
	return &TwoFAState{Enabled: enabled, Secret: secret, BackupCodes: codes, Verified: verified}, nil
}

// getWithVersion is GetTwoFA plus the optimistic-concurrency version, used
// by ConsumeBackupCode's compare-and-swap. Returns nil state (and leaves
// *version untouched) when the user is not enrolled.
func (s *EntityTwoFAStore) getWithVersion(ctx context.Context, userID string, version *int64) (*TwoFAState, error) {
	q := s.qTable("SELECT enabled, secret, backup_codes, verified, version FROM %s WHERE user_id = $1")
	var enabled, verified bool
	var secret, codesJSON string
	err := s.db.QueryRowContext(ctx, q, userID).Scan(&enabled, &secret, &codesJSON, &verified, version)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal([]byte(codesJSON), &codes); err != nil {
		return nil, fmt.Errorf("auth: EntityTwoFAStore: corrupt backup_codes row for user %s: %w", userID, err)
	}
	return &TwoFAState{Enabled: enabled, Secret: secret, BackupCodes: codes, Verified: verified}, nil
}

// SetTwoFA upserts the 2FA state for a user. A nil state deletes the row,
// matching the semantics callers get from DeleteTwoFA.
func (s *EntityTwoFAStore) SetTwoFA(ctx context.Context, userID string, state *TwoFAState) error {
	if state == nil {
		return s.DeleteTwoFA(ctx, userID)
	}
	codes := state.BackupCodes
	if codes == nil {
		codes = []string{}
	}
	codesJSON, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	// ON CONFLICT ... DO UPDATE is supported by both PostgreSQL and
	// SQLite (3.24+), so one statement covers both dialects. Bump version
	// on every write so a ConsumeBackupCode CAS in flight against the old
	// state misses and re-reads (a full state replace must invalidate a
	// concurrent per-code mutation).
	tbl := query.QuoteIdent(s.table)
	q := fmt.Sprintf("INSERT INTO %s (user_id, enabled, secret, backup_codes, verified, version) VALUES ($1, $2, $3, $4, $5, 0) ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled, secret = excluded.secret, backup_codes = excluded.backup_codes, verified = excluded.verified, version = %s.version + 1", tbl, tbl)
	_, err = s.db.ExecContext(ctx, q, userID, state.Enabled, state.Secret, string(codesJSON), state.Verified)
	return err
}

// DeleteTwoFA removes the 2FA state for a user. Deleting an absent row is
// not an error.
func (s *EntityTwoFAStore) DeleteTwoFA(ctx context.Context, userID string) error {
	q := s.qTable("DELETE FROM %s WHERE user_id = $1")
	_, err := s.db.ExecContext(ctx, q, userID)
	return err
}

// ConsumeBackupCode checks the given code against the stored bcrypt hashes
// and, on a match, removes that code atomically. Concurrency-safe across
// replicas via an optimistic compare-and-swap on an integer version
// column (NOT the JSON bytes — so a non-canonically-formatted row can't
// wedge consumption): if two replicas race to consume the SAME code,
// exactly one CAS wins; the loser re-reads, no longer finds the code, and
// returns false.
func (s *EntityTwoFAStore) ConsumeBackupCode(ctx context.Context, userID string, code string) (bool, error) {
	// A lost CAS means the row changed under us (another code consumed, or a
	// SetTwoFA) — re-read and retry. Bound the loop by the initial code
	// count + slack: each failed CAS corresponds to one competing write, so
	// the code we're after either wins or is proven gone within that many
	// rounds. (A fixed 2-retry bound wrongly rejected a still-valid code
	// under 3-plus-way concurrent consumption.)
	maxRounds := 8
	for round := 0; ; round++ {
		var version int64
		state, err := s.getWithVersion(ctx, userID, &version)
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
			// Extreme contention: fail closed (the code is NOT burned — no
			// UPDATE we ran removed it — so a client retry still works).
			return false, nil
		}

		// Bcrypt comparisons happen against a snapshot, outside any
		// transaction, so slow hashing never holds a DB lock.
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
		q := s.qTable("UPDATE %s SET backup_codes = $1, version = version + 1 WHERE user_id = $2 AND version = $3")
		res, err := s.db.ExecContext(ctx, q, string(newJSON), userID, version)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return true, nil
		}
	}
}

// TwoFAEntityFields returns the standard field definitions for a 2FA
// state entity, for hosts that want the table visible to the entity
// system (admin screens, migrations). The secret and backup-code hashes
// are Hidden so they can never leak through an API response.
func TwoFAEntityFields() []schema.Field {
	return []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true, Unique: true},
		{Name: "enabled", Type: schema.Bool, Default: "false"},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "backup_codes", Type: schema.Text, Hidden: true},
		{Name: "verified", Type: schema.Bool, Default: "false"},
		// version backs ConsumeBackupCode's optimistic CAS; a host that
		// registers this entity for auto-migration MUST include it, or the
		// generated table lacks the column and every 2FA op errors. RawType
		// BIGINT matches the store's hand-written DDL; ReadOnly+Hidden keep
		// this internal counter out of client request/response bodies.
		{Name: "version", Type: schema.Int, RawType: "BIGINT", Default: 0, ReadOnly: true, Hidden: true},
	}
}
