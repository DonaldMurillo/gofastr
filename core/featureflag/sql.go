package featureflag

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLStore is a MutableStore on one table, allow lists as JSON arrays.
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

// WithSQLDialect pins the dialect, "postgres" or "sqlite", unprobed.
func WithSQLDialect(dialect string) SQLOption {
	return func(s *SQLStore) {
		s.dialect = dialect
		s.dialectExplicit = true
	}
}

// NewSQLStore opens a SQLStore, creating and registering its table.
func NewSQLStore(db *sql.DB, opts ...SQLOption) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("featureflag: nil DB")
	}
	s := &SQLStore{db: db, table: "feature_flags", dialect: "sqlite"}
	for _, opt := range opts {
		opt(s)
	}
	if !query.SafeTableName(s.table) {
		return nil, fmt.Errorf("featureflag: unsafe table name %q", s.table)
	}
	if s.dialect != "postgres" && s.dialect != "sqlite" {
		return nil, fmt.Errorf("featureflag: unsupported dialect %q (want postgres or sqlite)", s.dialect)
	}
	if !s.dialectExplicit && query.IsPostgres(db) {
		s.dialect = "postgres"
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("featureflag: ensure table: %w", err)
	}
	s.registerData()
	return s, nil
}

func (s *SQLStore) registerData() {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("feature_flags", s.table), Source: "featureflag", Table: s.table, PrimaryKey: "key",
		Columns: []string{"key", "enabled", "rollout", "users", "tenants", "envs"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("feature_flags", s.table), Source: "featureflag", Table: s.table,
		Column: "users", Mode: datexport.EraseRemoveMember, MemberSeparator: ":",
	})
}

func (s *SQLStore) ensureTable() error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key      TEXT PRIMARY KEY NOT NULL,
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
	f, err := scanFlag(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func scanFlag(row interface{ Scan(...any) error }) (Flag, error) {
	var f Flag
	var enabled int
	var users, tenants, envs string
	if err := row.Scan(&f.Key, &enabled, &f.Rollout, &users, &tenants, &envs); err != nil {
		return Flag{}, err
	}
	f.Enabled = enabled != 0
	for _, list := range []struct {
		name string
		raw  string
		dst  *[]string
	}{{"users", users, &f.Users}, {"tenants", tenants, &f.Tenants}, {"envs", envs, &f.Envs}} {
		if list.raw == "" {
			continue
		}
		if err := json.Unmarshal([]byte(list.raw), list.dst); err != nil {
			return Flag{}, fmt.Errorf("featureflag: malformed %s JSON for %q: %w", list.name, f.Key, err)
		}
	}
	return f, nil
}

// Set upserts f, checked and clamped as MemoryStore.Set does.
func (s *SQLStore) Set(f Flag) error {
	f, err := checkFlag(f)
	if err != nil {
		return err
	}
	usersJSON, _ := json.Marshal(orEmpty(f.Users))
	tenantsJSON, _ := json.Marshal(orEmpty(f.Tenants))
	envsJSON, _ := json.Marshal(orEmpty(f.Envs))
	enabled := 0
	if f.Enabled {
		enabled = 1
	}

	_, err = s.db.Exec(s.upsertStmt(), f.Key, enabled, f.Rollout, string(usersJSON), string(tenantsJSON), string(envsJSON))
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

// All returns every flag by key; gate it as [MemoryStore.All].
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
		f, err := scanFlag(rows)
		if err != nil {
			return nil, err
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
