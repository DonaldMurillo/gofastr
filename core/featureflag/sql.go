package featureflag

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SQLStore is a SQL-backed MutableStore. Flags are persisted as
// individual rows; allow lists (Users, Tenants, Envs) live in JSON
// columns so the schema stays a single table.
//
// Dialect is either set explicitly via WithSQLDialect or detected at
// construction via SELECT version() (matches Postgres; everything else
// is treated as sqlite-compatible).
//
// Schema:
//
//	feature_flags(
//	    key      TEXT PRIMARY KEY,
//	    enabled  INTEGER NOT NULL,
//	    rollout  INTEGER NOT NULL,
//	    users    TEXT NOT NULL,    -- JSON array
//	    tenants  TEXT NOT NULL,    -- JSON array
//	    envs     TEXT NOT NULL     -- JSON array
//	)
type SQLStore struct {
	db              *sql.DB
	table           string
	dialect         string
	dialectExplicit bool
}

// SQLOption configures the SQL store.
type SQLOption func(*SQLStore)

// WithSQLTable overrides the default "feature_flags" table name.
func WithSQLTable(name string) SQLOption {
	return func(s *SQLStore) { s.table = name }
}

// WithSQLDialect pins the dialect ("postgres" or "sqlite") instead of
// running the SELECT version() probe. Use this when the probe is known
// to fail (e.g. embedded sqlite with restricted features) or when you
// want construction to avoid any DB read at all.
func WithSQLDialect(dialect string) SQLOption {
	return func(s *SQLStore) {
		s.dialect = dialect
		s.dialectExplicit = true
	}
}

// NewSQLStore constructs a SQLStore and ensures the table exists.
func NewSQLStore(db *sql.DB, opts ...SQLOption) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("featureflag: nil DB")
	}
	s := &SQLStore{db: db, table: "feature_flags", dialect: "sqlite"}
	for _, opt := range opts {
		opt(s)
	}
	if !safeIdent(s.table) {
		return nil, fmt.Errorf("featureflag: unsafe table name %q", s.table)
	}
	if s.dialect != "postgres" && s.dialect != "sqlite" {
		return nil, fmt.Errorf("featureflag: unsupported dialect %q (want postgres or sqlite)", s.dialect)
	}
	if !s.dialectExplicit {
		var v string
		if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
			if strings.Contains(strings.ToLower(v), "postgresql") {
				s.dialect = "postgres"
			}
		}
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("featureflag: ensure table: %w", err)
	}
	return s, nil
}

func (s *SQLStore) ensureTable() error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key      TEXT PRIMARY KEY,
		enabled  INTEGER NOT NULL,
		rollout  INTEGER NOT NULL,
		users    TEXT NOT NULL,
		tenants  TEXT NOT NULL,
		envs     TEXT NOT NULL DEFAULT '[]'
	)`, s.table)
	_, err := s.db.Exec(stmt)
	return err
}

// Get implements Store.
func (s *SQLStore) Get(ctx context.Context, key string) (*Flag, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT key, enabled, rollout, users, tenants, envs FROM %s WHERE key = %s",
			s.table, s.placeholder(1)),
		key,
	)
	var f Flag
	var enabled int
	var usersJSON, tenantsJSON, envsJSON string
	switch err := row.Scan(&f.Key, &enabled, &f.Rollout, &usersJSON, &tenantsJSON, &envsJSON); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	f.Enabled = enabled != 0
	if err := decodeJSONList(usersJSON, &f.Users); err != nil {
		return nil, fmt.Errorf("featureflag: malformed users JSON for %q: %w", key, err)
	}
	if err := decodeJSONList(tenantsJSON, &f.Tenants); err != nil {
		return nil, fmt.Errorf("featureflag: malformed tenants JSON for %q: %w", key, err)
	}
	if err := decodeJSONList(envsJSON, &f.Envs); err != nil {
		return nil, fmt.Errorf("featureflag: malformed envs JSON for %q: %w", key, err)
	}
	return &f, nil
}

// Set implements MutableStore. Upserts the flag definition.
func (s *SQLStore) Set(f Flag) error {
	if f.Key == "" {
		return errors.New("featureflag: empty key")
	}
	if f.Rollout < 0 {
		f.Rollout = 0
	}
	if f.Rollout > 100 {
		f.Rollout = 100
	}
	usersJSON, _ := json.Marshal(orEmpty(f.Users))
	tenantsJSON, _ := json.Marshal(orEmpty(f.Tenants))
	envsJSON, _ := json.Marshal(orEmpty(f.Envs))
	enabled := 0
	if f.Enabled {
		enabled = 1
	}

	upsert := s.upsertStmt()
	_, err := s.db.Exec(upsert, f.Key, enabled, f.Rollout, string(usersJSON), string(tenantsJSON), string(envsJSON))
	return err
}

// Delete implements MutableStore.
func (s *SQLStore) Delete(key string) error {
	_, err := s.db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE key = %s", s.table, s.placeholder(1)),
		key,
	)
	return err
}

// All returns every defined flag — used by admin tooling.
func (s *SQLStore) All(ctx context.Context) ([]Flag, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT key, enabled, rollout, users, tenants, envs FROM %s ORDER BY key", s.table),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		var enabled int
		var u, ten, env string
		if err := rows.Scan(&f.Key, &enabled, &f.Rollout, &u, &ten, &env); err != nil {
			return nil, err
		}
		f.Enabled = enabled != 0
		if err := decodeJSONList(u, &f.Users); err != nil {
			return nil, fmt.Errorf("featureflag: malformed users JSON for %q: %w", f.Key, err)
		}
		if err := decodeJSONList(ten, &f.Tenants); err != nil {
			return nil, fmt.Errorf("featureflag: malformed tenants JSON for %q: %w", f.Key, err)
		}
		if err := decodeJSONList(env, &f.Envs); err != nil {
			return nil, fmt.Errorf("featureflag: malformed envs JSON for %q: %w", f.Key, err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *SQLStore) upsertStmt() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(`INSERT INTO %s (key, enabled, rollout, users, tenants, envs)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (key) DO UPDATE SET
				enabled = EXCLUDED.enabled,
				rollout = EXCLUDED.rollout,
				users   = EXCLUDED.users,
				tenants = EXCLUDED.tenants,
				envs    = EXCLUDED.envs`, s.table)
	}
	return fmt.Sprintf(`INSERT INTO %s (key, enabled, rollout, users, tenants, envs)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET
			enabled = excluded.enabled,
			rollout = excluded.rollout,
			users   = excluded.users,
			tenants = excluded.tenants,
			envs    = excluded.envs`, s.table)
}

func (s *SQLStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func orEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// decodeJSONList accepts empty string as "no entries" but returns an
// error for malformed payloads — silent JSON failures previously left
// allow lists empty and silently changed flag behavior.
func decodeJSONList(s string, dst *[]string) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), dst)
}

// reservedSQLIdents are tokens we refuse as table names regardless of
// character class — naming a table after a reserved word or a real
// system table is almost always a configuration mistake and can
// silently no-op CREATE TABLE IF NOT EXISTS against a real table.
var reservedSQLIdents = map[string]struct{}{
	"select":     {},
	"insert":     {},
	"update":     {},
	"delete":     {},
	"drop":       {},
	"create":     {},
	"table":      {},
	"from":       {},
	"where":      {},
	"users":      {},
	"user":       {},
	"migrations": {},
	"sessions":   {},
	"accounts":   {},
}

func safeIdent(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	// Require a leading letter or underscore — leading-digit names like
	// "1tbl" survive otherwise and break some dialect parsers.
	first := rune(name[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !ok {
			return false
		}
	}
	if _, bad := reservedSQLIdents[strings.ToLower(name)]; bad {
		return false
	}
	return true
}
