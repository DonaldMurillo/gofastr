package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// SQLIdempotencyStore is an IdempotencyStore on a SQLite or Postgres table, created on first use and registered for App.ExportData and App.EraseUserData by its principal column.
type SQLIdempotencyStore struct {
	db            *sql.DB
	table         string
	ttl           time.Duration
	inFlightTTL   time.Duration
	dialect       string
	dialectPinned bool
	lastReapUnix  atomic.Int64
}

// SQLIdempotencyOption configures the SQL store.
type SQLIdempotencyOption func(*SQLIdempotencyStore)

// WithSQLIdempotencyTable overrides the default "idempotency_keys" table name.
func WithSQLIdempotencyTable(name string) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) { s.table = name }
}

// WithSQLIdempotencyTTL sets the cached-response TTL; d <= 0 keeps 24h.
func WithSQLIdempotencyTTL(d time.Duration) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) {
		if d > 0 {
			s.ttl = d
		}
	}
}

// WithSQLIdempotencyInFlightTTL sets how long an in-flight claim holds its key; d <= 0 keeps 30s.
func WithSQLIdempotencyInFlightTTL(d time.Duration) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) {
		if d > 0 {
			s.inFlightTTL = d
		}
	}
}

// InFlightTTL implements InFlightTTLStore.
func (s *SQLIdempotencyStore) InFlightTTL() time.Duration { return s.inFlightTTL }

// WithSQLIdempotencyDialect pins the dialect ("postgres" or "sqlite") instead of probing the DB.
func WithSQLIdempotencyDialect(dialect string) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) {
		s.dialect = dialect
		s.dialectPinned = true
	}
}

// NewSQLIdempotencyStore returns a SQL-backed IdempotencyStore, creating its table if needed.
func NewSQLIdempotencyStore(db *sql.DB, opts ...SQLIdempotencyOption) (*SQLIdempotencyStore, error) {
	if db == nil {
		return nil, errors.New("idempotency: nil DB")
	}
	s := &SQLIdempotencyStore{
		db:          db,
		table:       "idempotency_keys",
		ttl:         24 * time.Hour,
		inFlightTTL: 30 * time.Second,
		dialect:     "sqlite",
	}
	for _, opt := range opts {
		opt(s)
	}
	if !query.SafeTableName(s.table) {
		return nil, fmt.Errorf("idempotency: unsafe table name %q", s.table)
	}
	if s.dialect != "postgres" && s.dialect != "sqlite" {
		return nil, fmt.Errorf("idempotency: unsupported dialect %q (want postgres or sqlite)", s.dialect)
	}
	if !s.dialectPinned && query.IsPostgres(db) {
		s.dialect = "postgres"
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure table: %w", err)
	}
	s.registerData()
	return s, nil
}

func (s *SQLIdempotencyStore) registerData() {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("idempotency_keys", s.table), Source: "idempotency", Table: s.table, PrimaryKey: "key",
		Columns: []string{"key", "fingerprint", "status", "headers", "body", "expires_at", "created_at", "principal"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("idempotency_keys", s.table), Source: "idempotency", Table: s.table, Column: "principal",
		Mode: datexport.EraseDelete,
	})
}

func (s *SQLIdempotencyStore) ensureTable() error {
	tsType, blobType := "DATETIME", "BLOB"
	if s.dialect == "postgres" {
		tsType, blobType = "TIMESTAMPTZ", "BYTEA"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key         TEXT PRIMARY KEY NOT NULL,
		fingerprint TEXT NOT NULL,
		status      INTEGER,
		headers     TEXT,
		body        %s,
		expires_at  %s NOT NULL,
		created_at  %s NOT NULL,
		principal   TEXT NOT NULL DEFAULT ''
	)`, s.table, blobType, tsType, tsType)
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	if err := s.addPrincipalColumn(); err != nil {
		return fmt.Errorf("idempotency: add principal column: %w", err)
	}
	if _, err := s.db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s (expires_at)",
		query.IndexOn(s.dialect == "sqlite", s.table+"_expires_idx", s.table),
	)); err != nil {
		return fmt.Errorf("idempotency: create expiry index: %w", err)
	}
	return nil
}

// Begin implements IdempotencyStore.
func (s *SQLIdempotencyStore) Begin(ctx context.Context, key, fingerprint string) (*IdempotentResponse, bool, error) {
	now := time.Now().UTC()
	principal, err := s.columnPrincipal(key)
	if err != nil {
		return nil, false, err
	}
	dbKey := s.columnKey(key)
	if last := s.lastReapUnix.Load(); now.Unix()-last > 60 {
		if s.lastReapUnix.CompareAndSwap(last, now.Unix()) {
			// best-effort: authoritative reads filter expired rows; this
			// bounded reap only controls storage growth.
			_, _ = s.db.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE expires_at <= %s", s.table, s.placeholder(1)),
				now,
			)
		}
	}

	res, err := s.db.ExecContext(ctx, s.upsertClaimStmt(),
		dbKey, fingerprint, now.Add(s.inFlightTTL), now, principal,
	)
	if err != nil {
		return nil, false, err
	}
	rows, _ := res.RowsAffected()
	if rows >= 1 {
		return nil, false, nil
	}

	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT fingerprint, status, headers, body FROM %s WHERE key = %s AND expires_at > %s",
			s.table, s.placeholder(1), s.placeholder(2)),
		dbKey, now,
	)
	var storedFP string
	var status sql.NullInt64
	var headers sql.NullString
	var body []byte
	err = row.Scan(&storedFP, &status, &headers, &body)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, deleteErr := s.db.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE key = %s AND expires_at <= %s", s.table, s.placeholder(1), s.placeholder(2)),
			dbKey, now,
		); deleteErr != nil {
			return nil, false, fmt.Errorf("idempotency: delete expired claim: %w", deleteErr)
		}
		res, err = s.db.ExecContext(ctx, s.upsertClaimStmt(),
			dbKey, fingerprint, now.Add(s.inFlightTTL), now, principal,
		)
		if err != nil {
			return nil, false, err
		}
		rows, _ = res.RowsAffected()
		if rows >= 1 {
			return nil, false, nil
		}
		return nil, false, ErrInFlight
	case err != nil:
		return nil, false, err
	}
	if storedFP != fingerprint {
		return nil, false, ErrFingerprintMismatch
	}
	if !status.Valid {
		return nil, false, ErrInFlight
	}
	hdr := http.Header{}
	if headers.Valid && headers.String != "" {
		if err := json.Unmarshal([]byte(headers.String), &hdr); err != nil {
			return nil, false, fmt.Errorf("idempotency: stored response headers for %q are corrupt: %w", key, err)
		}
	}
	return &IdempotentResponse{
		Status: int(status.Int64),
		Header: hdr,
		Body:   append([]byte(nil), body...),
	}, true, nil
}

// Finish implements IdempotencyStore.
func (s *SQLIdempotencyStore) Finish(ctx context.Context, key, fingerprint string, resp *IdempotentResponse) error {
	if resp == nil {
		_, err := s.db.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE key = %s AND fingerprint = %s", s.table, s.placeholder(1), s.placeholder(2)),
			s.columnKey(key), fingerprint,
		)
		return err
	}
	hdrJSON, err := json.Marshal(resp.Header)
	if err != nil {
		return err
	}
	expires := time.Now().UTC().Add(s.ttl)
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET status = %s, headers = %s, body = %s, expires_at = %s WHERE key = %s AND fingerprint = %s",
		s.table,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5), s.placeholder(6),
	), resp.Status, string(hdrJSON), resp.Body, expires, s.columnKey(key), fingerprint)
	return err
}

func (s *SQLIdempotencyStore) addPrincipalColumn() error {
	if s.dialect == "postgres" {
		_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS principal TEXT NOT NULL DEFAULT ''`, s.table))
		return err
	}
	_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN principal TEXT NOT NULL DEFAULT ''`, s.table))
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

func (s *SQLIdempotencyStore) upsertClaimStmt() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(
			"INSERT INTO %s (key, fingerprint, expires_at, created_at, principal) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (key) DO NOTHING",
			s.table,
		)
	}
	return fmt.Sprintf(
		"INSERT INTO %s (key, fingerprint, expires_at, created_at, principal) VALUES (?,?,?,?,?) ON CONFLICT(key) DO NOTHING",
		s.table,
	)
}

func (s *SQLIdempotencyStore) columnKey(key string) string {
	if s.dialect == "postgres" {
		return pgKey(key)
	}
	return key
}

var errPrincipalNUL = errors.New("idempotency: principal holds a NUL byte, which a postgres TEXT column cannot store")

var errKeyNotUTF8 = errors.New("idempotency: store key is not valid UTF-8, which a postgres TEXT column cannot store")

func (s *SQLIdempotencyStore) columnPrincipal(key string) (string, error) {
	p := idemPrincipal(key)
	if s.dialect == "postgres" && strings.IndexByte(p, 0) >= 0 {
		return "", errPrincipalNUL
	}
	if s.dialect == "postgres" && !utf8.ValidString(key) {
		return "", errKeyNotUTF8
	}
	return p, nil
}

func pgKey(s string) string {
	if strings.IndexByte(s, 0) < 0 && strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case 0:
			b.WriteString(`\0`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func (s *SQLIdempotencyStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}
