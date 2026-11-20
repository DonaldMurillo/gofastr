package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)


// SQLAPITokenStore is an APITokenStore in a SQLite or PostgreSQL table.
type SQLAPITokenStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect
}

// SQLAPITokenStoreOption tunes NewSQLAPITokenStore.
type SQLAPITokenStoreOption func(*sqlAPITokenStoreConfig)

type sqlAPITokenStoreConfig struct {
	table string
}

// WithAPITokenTable sets the token table name (default auth_api_tokens).
func WithAPITokenTable(name string) SQLAPITokenStoreOption {
	return func(c *sqlAPITokenStoreConfig) { c.table = name }
}

// NewSQLAPITokenStore creates the token table and returns the store.
func NewSQLAPITokenStore(db *sql.DB, opts ...SQLAPITokenStoreOption) (*SQLAPITokenStore, error) {
	c := sqlAPITokenStoreConfig{table: "auth_api_tokens"}
	for _, o := range opts {
		o(&c)
	}
	return initSQLStore(db, "NewSQLAPITokenStore", "api-token", c.table,
		func(db *sql.DB, table string) *SQLAPITokenStore { return &SQLAPITokenStore{db: db, table: table} },
		func(s *SQLAPITokenStore) error { return s.ensureSchema(context.Background()) },
	)
}

func (s *SQLAPITokenStore) ensureSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	tsType := "DATETIME"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		tsType = "TIMESTAMP"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (`+
			`id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, owner_kind TEXT NOT NULL, owner_id TEXT NOT NULL, `+
			`prefix TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, scopes TEXT NOT NULL DEFAULT '[]', `+
			`expires_at %s NULL, last_used_at %s NULL, revoked_at %s NULL, created_at %s NOT NULL)`,
		query.QuoteFolded(s.table), tsType, tsType, tsType, tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	idxName, _ := query.SafeIdent(s.table + "_owner_idx")
	idx := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s (owner_kind, owner_id)`,
		query.IndexOn(s.dialect.of(s.db) == migrate.DialectSQLite, query.QuoteIndexName(idxName), query.QuoteFolded(s.table)))
	if _, err := s.db.ExecContext(ctx, idx); err != nil {
		return err
	}
	registerAPITokens(s.table)
	return nil
}

func registerAPITokens(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("auth_api_tokens", table), Source: "auth", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "name", "owner_kind", "owner_id", "prefix", "hash", "scopes",
			"expires_at", "last_used_at", "revoked_at", "created_at"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("auth_api_tokens", table), Source: "auth", Table: table,
		Column: "owner_id", Mode: datexport.EraseDelete,
		Where: []datexport.Equals{{Column: "owner_kind", Value: OwnerKindUser}},
	})
}

func (s *SQLAPITokenStore) q(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteFolded(s.table))
}

// Create inserts a token row under its sha256 hash.
func (s *SQLAPITokenStore) Create(ctx context.Context, t APIToken, sha256Hash string) error {
	q := s.q(`INSERT INTO %s (id, name, owner_kind, owner_id, prefix, hash, scopes, expires_at, last_used_at, revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`)
	_, err := s.db.ExecContext(ctx, q,
		t.ID, t.Name, t.OwnerKind, t.OwnerID, t.Prefix, sha256Hash,
		marshalStringList(t.Scopes),
		nullableTime(t.ExpiresAt), nullableTime(t.LastUsedAt), nullableTime(t.RevokedAt),
		t.CreatedAt,
	)
	return err
}

// FindByHash returns the token with that hash, or nil for none.
func (s *SQLAPITokenStore) FindByHash(ctx context.Context, sha256Hash string) (*APIToken, error) {
	q := s.q(`SELECT id, name, owner_kind, owner_id, prefix, scopes, expires_at, last_used_at, revoked_at, created_at FROM %s WHERE hash = $1`)
	t, err := scanAPIToken(s.db.QueryRowContext(ctx, q, sha256Hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// List returns the tokens owned by (ownerKind, ownerID), newest first.
func (s *SQLAPITokenStore) List(ctx context.Context, ownerKind, ownerID string) ([]APIToken, error) {
	q := s.q(`SELECT id, name, owner_kind, owner_id, prefix, scopes, expires_at, last_used_at, revoked_at, created_at FROM %s WHERE owner_kind = $1 AND owner_id = $2 ORDER BY created_at DESC`)
	rows, err := s.db.QueryContext(ctx, q, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// Revoke stamps RevokedAt on the owner's token, idempotently, or returns ErrTokenNotFound.
func (s *SQLAPITokenStore) Revoke(ctx context.Context, id, ownerKind, ownerID string) error {
	var revokedRaw any
	err := s.db.QueryRowContext(ctx,
		s.q(`SELECT revoked_at FROM %s WHERE id = $1 AND owner_kind = $2 AND owner_id = $3`),
		id, ownerKind, ownerID,
	).Scan(&revokedRaw)
	if err == sql.ErrNoRows {
		return ErrTokenNotFound
	}
	if err != nil {
		return err
	}
	if !coerceTime(revokedRaw).IsZero() {
		return nil // idempotent: already revoked
	}
	_, err = s.db.ExecContext(ctx,
		s.q(`UPDATE %s SET revoked_at = $1 WHERE id = $2 AND owner_kind = $3 AND owner_id = $4`),
		time.Now().UTC(), id, ownerKind, ownerID)
	return err
}

// TouchLastUsed stamps last_used_at.
func (s *SQLAPITokenStore) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE %s SET last_used_at = $1 WHERE id = $2`), at, id)
	return err
}

// ListAll returns every owner's tokens, newest first; it is for host admin screens, not the plugin's routes.
func (s *SQLAPITokenStore) ListAll(ctx context.Context) ([]APIToken, error) {
	q := s.q(`SELECT id, name, owner_kind, owner_id, prefix, scopes, expires_at, last_used_at, revoked_at, created_at FROM %s ORDER BY created_at DESC`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// RevokeAny stamps RevokedAt whatever the owner, or returns ErrTokenNotFound; host admin use only.
func (s *SQLAPITokenStore) RevokeAny(ctx context.Context, id string) error {
	var revokedRaw any
	err := s.db.QueryRowContext(ctx,
		s.q(`SELECT revoked_at FROM %s WHERE id = $1`), id,
	).Scan(&revokedRaw)
	if err == sql.ErrNoRows {
		return ErrTokenNotFound
	}
	if err != nil {
		return err
	}
	if !coerceTime(revokedRaw).IsZero() {
		return nil // idempotent: already revoked
	}
	_, err = s.db.ExecContext(ctx,
		s.q(`UPDATE %s SET revoked_at = $1 WHERE id = $2`), time.Now().UTC(), id)
	return err
}


// SQLServiceAccountStore is the database-backed ServiceAccountStore.
type SQLServiceAccountStore struct {
	db      *sql.DB
	table   string
	dialect storeDialect
}

// SQLServiceAccountStoreOption tunes NewSQLServiceAccountStore.
type SQLServiceAccountStoreOption func(*sqlServiceAccountStoreConfig)

type sqlServiceAccountStoreConfig struct {
	table string
}

// WithServiceAccountTable sets the table name (default auth_service_accounts).
func WithServiceAccountTable(name string) SQLServiceAccountStoreOption {
	return func(c *sqlServiceAccountStoreConfig) { c.table = name }
}

// NewSQLServiceAccountStore creates the table and returns the store.
func NewSQLServiceAccountStore(db *sql.DB, opts ...SQLServiceAccountStoreOption) (*SQLServiceAccountStore, error) {
	c := sqlServiceAccountStoreConfig{table: "auth_service_accounts"}
	for _, o := range opts {
		o(&c)
	}
	return initSQLStore(db, "NewSQLServiceAccountStore", "service-account", c.table,
		func(db *sql.DB, table string) *SQLServiceAccountStore {
			return &SQLServiceAccountStore{db: db, table: table}
		},
		func(s *SQLServiceAccountStore) error { return s.ensureSchema(context.Background()) },
	)
}

func initSQLStore[S any](db *sql.DB, ctorName, kind, table string, build func(db *sql.DB, table string) S, ensure func(S) error) (S, error) {
	var zero S
	if db == nil {
		return zero, fmt.Errorf("auth: %s: db is nil", ctorName)
	}
	if _, err := query.SafeIdent(table); err != nil {
		return zero, fmt.Errorf("auth: %s table %q: %w", kind, table, err)
	}
	s := build(db, table)
	if err := ensure(s); err != nil {
		return zero, fmt.Errorf("auth: create %s table: %w", kind, err)
	}
	return s, nil
}

func (s *SQLServiceAccountStore) ensureSchema(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
	tsType, boolType, boolFalse := "DATETIME", "INTEGER", "0"
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		tsType, boolType, boolFalse = "TIMESTAMP", "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL UNIQUE, roles TEXT NOT NULL DEFAULT '[]', disabled %s NOT NULL DEFAULT %s, created_at %s NOT NULL)`,
		query.QuoteFolded(s.table), boolType, boolFalse, tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	registerServiceAccounts(s.table)
	return nil
}

func registerServiceAccounts(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("auth_service_accounts", table), Source: "auth", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "name", "roles", "disabled", "created_at"},
	})
}

func (s *SQLServiceAccountStore) q(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteFolded(s.table))
}

func (s *SQLServiceAccountStore) Create(ctx context.Context, sa ServiceAccount) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO %s (id, name, roles, disabled, created_at) VALUES ($1, $2, $3, $4, $5)`),
		sa.ID, sa.Name, marshalStringList(sa.Roles), sa.Disabled, sa.CreatedAt)
	return err
}

func (s *SQLServiceAccountStore) Get(ctx context.Context, id string) (*ServiceAccount, error) {
	var sa ServiceAccount
	var roles string
	err := s.db.QueryRowContext(ctx,
		s.q(`SELECT id, name, roles, disabled, created_at FROM %s WHERE id = $1`), id,
	).Scan(&sa.ID, &sa.Name, &roles, &sa.Disabled, &sa.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sa.Roles = unmarshalStringList(roles)
	return &sa, nil
}

func (s *SQLServiceAccountStore) List(ctx context.Context) ([]ServiceAccount, error) {
	rows, err := s.db.QueryContext(ctx,
		s.q(`SELECT id, name, roles, disabled, created_at FROM %s ORDER BY created_at DESC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceAccount
	for rows.Next() {
		var sa ServiceAccount
		var roles string
		if err := rows.Scan(&sa.ID, &sa.Name, &roles, &sa.Disabled, &sa.CreatedAt); err != nil {
			return nil, err
		}
		sa.Roles = unmarshalStringList(roles)
		out = append(out, sa)
	}
	return out, rows.Err()
}

func (s *SQLServiceAccountStore) SetDisabled(ctx context.Context, id string, disabled bool) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`UPDATE %s SET disabled = $1 WHERE id = $2`), disabled, id)
	return err
}


var (
	_ APITokenStore       = (*SQLAPITokenStore)(nil)
	_ ServiceAccountStore = (*SQLServiceAccountStore)(nil)
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAPIToken(row rowScanner) (*APIToken, error) {
	var t APIToken
	var scopes string
	var expiresAtRaw, lastUsedRaw, revokedAtRaw any
	err := row.Scan(&t.ID, &t.Name, &t.OwnerKind, &t.OwnerID, &t.Prefix, &scopes,
		&expiresAtRaw, &lastUsedRaw, &revokedAtRaw, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	t.Scopes = unmarshalStringList(scopes)
	t.ExpiresAt = timePtrFailClosed(expiresAtRaw)
	t.LastUsedAt = timePtrFromRaw(lastUsedRaw)
	t.RevokedAt = timePtrFailClosed(revokedAtRaw)
	return &t, nil
}

func timePtrFailClosed(src any) *time.Time {
	if src == nil {
		return nil
	}
	if t := coerceTime(src); !t.IsZero() {
		return &t
	}
	epoch := time.Unix(0, 0).UTC()
	return &epoch
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func timePtrFromRaw(src any) *time.Time {
	t := coerceTime(src)
	if t.IsZero() {
		return nil
	}
	return &t
}

func marshalStringList(s []string) string {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func unmarshalStringList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
