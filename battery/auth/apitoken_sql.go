package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ─── SQLAPITokenStore ───────────────────────────────────────────────────────

// SQLAPITokenStore is the database-backed APITokenStore. Tokens are stored
// as a sha256 hash (never the plaintext), with a display Prefix and JSON
// scopes. It works on both SQLite and PostgreSQL via the battery's dialect
// conventions ($N placeholders, portable column types).
//
// Schema (table auth_api_tokens, created at construction):
//
//	id            TEXT PRIMARY KEY
//	name          TEXT NOT NULL
//	owner_kind    TEXT NOT NULL          -- "user" | "service"
//	owner_id      TEXT NOT NULL
//	prefix        TEXT NOT NULL          -- first 12 chars of plaintext
//	hash          TEXT NOT NULL UNIQUE   -- sha256hex(plaintext)
//	scopes        TEXT NOT NULL DEFAULT '[]'
//	expires_at    <ts> NULL
//	last_used_at  <ts> NULL
//	revoked_at    <ts> NULL
//	created_at    <ts> NOT NULL
type SQLAPITokenStore struct {
	db    *sql.DB
	table string
}

// SQLAPITokenStoreOption tunes NewSQLAPITokenStore.
type SQLAPITokenStoreOption func(*sqlAPITokenStoreConfig)

type sqlAPITokenStoreConfig struct {
	table string
}

// WithAPITokenTable overrides the token table name (default
// "auth_api_tokens"). Validated like the battery's other stores.
func WithAPITokenTable(name string) SQLAPITokenStoreOption {
	return func(c *sqlAPITokenStoreConfig) { c.table = name }
}

// NewSQLAPITokenStore creates the token table (IF NOT EXISTS) and returns
// the store. Table-name options are validated like the battery's other
// stores; schema is ensured at construction.
func NewSQLAPITokenStore(db *sql.DB, opts ...SQLAPITokenStoreOption) (*SQLAPITokenStore, error) {
	if db == nil {
		return nil, fmt.Errorf("auth: NewSQLAPITokenStore: db is nil")
	}
	c := sqlAPITokenStoreConfig{table: "auth_api_tokens"}
	for _, o := range opts {
		o(&c)
	}
	if _, err := query.SafeIdent(c.table); err != nil {
		return nil, fmt.Errorf("auth: api-token table %q: %w", c.table, err)
	}
	s := &SQLAPITokenStore{db: db, table: c.table}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("auth: create api-token table: %w", err)
	}
	return s, nil
}

func (s *SQLAPITokenStore) ensureSchema(ctx context.Context) error {
	tsType := "DATETIME"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		tsType = "TIMESTAMP"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (`+
			`id TEXT PRIMARY KEY, name TEXT NOT NULL, owner_kind TEXT NOT NULL, owner_id TEXT NOT NULL, `+
			`prefix TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, scopes TEXT NOT NULL DEFAULT '[]', `+
			`expires_at %s NULL, last_used_at %s NULL, revoked_at %s NULL, created_at %s NOT NULL)`,
		query.QuoteIdent(s.table), tsType, tsType, tsType, tsType,
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	// Listing a single owner's tokens is the hot path; index it.
	idxName, _ := query.SafeIdent(s.table + "_owner_idx")
	idx := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_kind, owner_id)`,
		query.QuoteIdent(idxName), query.QuoteIdent(s.table))
	_, err := s.db.ExecContext(ctx, idx)
	return err
}

// q wraps a statement template with the validated table name.
func (s *SQLAPITokenStore) q(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteIdent(s.table))
}

// Create inserts a token row. sha256Hash is the hash of the plaintext;
// the plaintext itself never reaches this layer.
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

// FindByHash looks up a token by its sha256 hash. Returns (nil, nil) for
// unknown hashes.
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

// Revoke stamps RevokedAt on the token owned by (ownerKind, ownerID). It is
// idempotent (an already-revoked token is a no-op success) and owner-scoped
// (a foreign id returns ErrTokenNotFound so the handler can answer 404).
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

// TouchLastUsed stamps last_used_at. The middleware calls this throttled
// (≥60s since the previous stamp) and best-effort.
func (s *SQLAPITokenStore) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE %s SET last_used_at = $1 WHERE id = $2`), at, id)
	return err
}

// ─── SQLServiceAccountStore ─────────────────────────────────────────────────

// SQLServiceAccountStore is the database-backed ServiceAccountStore.
//
// Schema (table auth_service_accounts, created at construction):
//
//	id          TEXT PRIMARY KEY
//	name        TEXT NOT NULL UNIQUE
//	roles       TEXT NOT NULL DEFAULT '[]'   -- JSON array
//	disabled    <bool> NOT NULL DEFAULT false
//	created_at  <ts> NOT NULL
type SQLServiceAccountStore struct {
	db    *sql.DB
	table string
}

// SQLServiceAccountStoreOption tunes NewSQLServiceAccountStore.
type SQLServiceAccountStoreOption func(*sqlServiceAccountStoreConfig)

type sqlServiceAccountStoreConfig struct {
	table string
}

// WithServiceAccountTable overrides the service-account table name
// (default "auth_service_accounts").
func WithServiceAccountTable(name string) SQLServiceAccountStoreOption {
	return func(c *sqlServiceAccountStoreConfig) { c.table = name }
}

// NewSQLServiceAccountStore creates the service-account table (IF NOT
// EXISTS) and returns the store.
func NewSQLServiceAccountStore(db *sql.DB, opts ...SQLServiceAccountStoreOption) (*SQLServiceAccountStore, error) {
	if db == nil {
		return nil, fmt.Errorf("auth: NewSQLServiceAccountStore: db is nil")
	}
	c := sqlServiceAccountStoreConfig{table: "auth_service_accounts"}
	for _, o := range opts {
		o(&c)
	}
	if _, err := query.SafeIdent(c.table); err != nil {
		return nil, fmt.Errorf("auth: service-account table %q: %w", c.table, err)
	}
	s := &SQLServiceAccountStore{db: db, table: c.table}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("auth: create service-account table: %w", err)
	}
	return s, nil
}

func (s *SQLServiceAccountStore) ensureSchema(ctx context.Context) error {
	tsType, boolType, boolFalse := "DATETIME", "INTEGER", "0"
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		tsType, boolType, boolFalse = "TIMESTAMP", "BOOLEAN", "FALSE"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, roles TEXT NOT NULL DEFAULT '[]', disabled %s NOT NULL DEFAULT %s, created_at %s NOT NULL)`,
		query.QuoteIdent(s.table), boolType, boolFalse, tsType,
	)
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}

func (s *SQLServiceAccountStore) q(stmt string) string {
	return fmt.Sprintf(stmt, query.QuoteIdent(s.table))
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

// ─── shared helpers ─────────────────────────────────────────────────────────

var (
	_ APITokenStore       = (*SQLAPITokenStore)(nil)
	_ ServiceAccountStore = (*SQLServiceAccountStore)(nil)
)

// rowScanner is the common Scan surface of *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanAPIToken reads the 10-column token projection. Nullable timestamps
// are scanned as any and coerced — matching the battery's other stores,
// which see time.Time on Postgres and string/[]byte on SQLite.
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
	t.ExpiresAt = timePtrFromRaw(expiresAtRaw)
	t.LastUsedAt = timePtrFromRaw(lastUsedRaw)
	t.RevokedAt = timePtrFromRaw(revokedAtRaw)
	return &t, nil
}

// nullableTime renders a *time.Time as a driver value: nil for unset, the
// time otherwise. Used when binding INSERT args for NULL-able columns.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// timePtrFromRaw converts a scanned nullable timestamp into a *time.Time.
// A zero/missing value (nil driver value or zero time) yields nil.
func timePtrFromRaw(src any) *time.Time {
	t := coerceTime(src)
	if t.IsZero() {
		return nil
	}
	return &t
}

// marshalStringList serializes a string slice as a JSON array ("[]" for
// nil/empty). Distinct from parseRoles/formatRoles, which are user-roles
// specific (they default to ["user"]); scopes and service-account roles
// must stay empty when empty.
func marshalStringList(s []string) string {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// unmarshalStringList is the inverse; a blank/garbage value yields nil.
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
