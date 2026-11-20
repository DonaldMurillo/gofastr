package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db/sqlbusy"
)

// SQLInboundStore is a SQL-backed InboundStore over one table, on SQLite or
// Postgres.
type SQLInboundStore struct {
	db      *sql.DB
	table   string
	dialect string
}

// InboundSQLOption configures SQLInboundStore.
type InboundSQLOption func(*SQLInboundStore)

// WithInboundTable overrides the default "webhook_inbound" table name.
func WithInboundTable(name string) InboundSQLOption {
	return func(s *SQLInboundStore) { s.table = name }
}

// NewSQLInboundStore constructs a SQLInboundStore and ensures its table
// exists.
func NewSQLInboundStore(db *sql.DB, opts ...InboundSQLOption) (*SQLInboundStore, error) {
	if db == nil {
		return nil, errors.New("webhook: nil DB")
	}
	s := &SQLInboundStore{
		db:      db,
		table:   "webhook_inbound",
		dialect: "sqlite",
	}
	for _, opt := range opts {
		opt(s)
	}
	if !query.SafeTableName(s.table) {
		return nil, errors.New("webhook: unsafe inbound table name")
	}
	if query.IsPostgres(db) {
		s.dialect = "postgres"
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure inbound table: %w", err)
	}
	s.registerData()
	return s, nil
}

func (s *SQLInboundStore) ensureTable() error {
	ts, blob := "DATETIME", "BLOB"
	if s.dialect == "postgres" {
		ts, blob = "TIMESTAMPTZ", "BYTEA"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY NOT NULL,
			source      TEXT NOT NULL,
			dedupe_key  TEXT NOT NULL DEFAULT '',
			headers     TEXT NOT NULL DEFAULT '{}',
			payload     %s NOT NULL,
			status      TEXT NOT NULL,
			attempts    INTEGER NOT NULL DEFAULT 0,
			last_error  TEXT NOT NULL DEFAULT '',
			received_at %s NOT NULL,
			updated_at  %s NOT NULL,
			owner       TEXT NOT NULL DEFAULT ''
		)`, s.table, blob, ts, ts),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (source, dedupe_key)",
			query.IndexOn(s.dialect == "sqlite", s.table+"_dedupe_idx", s.table)),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (status)",
			query.IndexOn(s.dialect == "sqlite", s.table+"_status_idx", s.table)),
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return addColumn(s.db, s.dialect, s.table, "owner TEXT NOT NULL DEFAULT ''")
}

func (s *SQLInboundStore) registerData() {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("webhook_inbound", s.table), Source: "webhook", Table: s.table, PrimaryKey: "id",
		Columns: strings.Split(strings.ReplaceAll(inboundColumns, " ", ""), ","),
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("webhook_inbound", s.table), Source: "webhook", Table: s.table, Column: "owner",
		Mode: datexport.EraseDelete,
	})
}

// AddEnvelope inserts e.
func (s *SQLInboundStore) AddEnvelope(ctx context.Context, e InboundEnvelope) error {
	headers, err := encodeHeaders(e.Headers)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.insert(), e.ID, e.Source, e.DedupeKey,
		headers, e.Payload, e.Status, e.Attempts, e.LastError, e.ReceivedAt.UTC(), e.UpdatedAt.UTC(), e.Owner)
	return err
}

// GetEnvelope returns (nil, nil) for an unknown id.
func (s *SQLInboundStore) GetEnvelope(ctx context.Context, id string) (*InboundEnvelope, error) {
	var e InboundEnvelope
	var headers string
	err := s.db.QueryRowContext(ctx, s.selectOne(), id).
		Scan(&e.ID, &e.Source, &e.DedupeKey, &headers, &e.Payload, &e.Status,
			&e.Attempts, &e.LastError, &e.ReceivedAt, &e.UpdatedAt, &e.Owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Headers = decodeHeaders(headers)
	return &e, nil
}

// UpdateEnvelope writes e's status, error and headers, never its attempts, and
// never regresses a processed envelope.
func (s *SQLInboundStore) UpdateEnvelope(ctx context.Context, e InboundEnvelope) error {
	headers, err := encodeHeaders(e.Headers)
	if err != nil {
		return err
	}
	q := s.update()
	args := []any{e.DedupeKey, headers, e.Status, e.LastError, e.UpdatedAt.UTC(), e.ID}
	if e.Status != InboundStatusProcessed {
		q += ` AND status <> ` + s.placeholder(7)
		args = append(args, InboundStatusProcessed)
	}
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := s.db.ExecContext(ctx, q, args...)
		return err
	})
}

// RegisterDedupeKey sets the envelope's dedupe key and nothing else; a missing
// id matches no row.
func (s *SQLInboundStore) RegisterDedupeKey(ctx context.Context, id, key string) error {
	q := fmt.Sprintf("UPDATE %s SET dedupe_key = %s WHERE id = %s", s.table, s.placeholder(1), s.placeholder(2))
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := s.db.ExecContext(ctx, q, key, id)
		return err
	})
}

// MarkEnvelopeProcessing marks an envelope processing and consumes one attempt
// in SQL; false for a missing or processed one.
func (s *SQLInboundStore) MarkEnvelopeProcessing(ctx context.Context, id string) (bool, error) {
	q := fmt.Sprintf(
		"UPDATE %s SET status = %s, attempts = attempts + 1, last_error = '', updated_at = %s WHERE id = %s",
		s.table, s.placeholder(1), s.placeholder(2), s.placeholder(3))
	args := []any{InboundStatusProcessing, time.Now().UTC(), id}
	q += " AND status <> " + s.placeholder(4)
	args = append(args, InboundStatusProcessed)
	var n int64
	err := sqlbusy.Retry(ctx, func(ctx context.Context) error {
		res, err := s.db.ExecContext(ctx, q, args...)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n == 1, err
}

// ListEnvelopes returns envelopes with status (empty for all), newest first,
// at most limit (0 for no cap).
func (s *SQLInboundStore) ListEnvelopes(ctx context.Context, status string, limit int) ([]InboundEnvelope, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s", inboundColumns, s.table)
	var args []any
	n := 1
	if status != "" {
		fmt.Fprintf(&b, " WHERE status = %s", s.placeholder(n))
		args = append(args, status)
		n++
	}
	b.WriteString(" ORDER BY received_at DESC")
	if limit > 0 {
		fmt.Fprintf(&b, " LIMIT %s", s.placeholder(n))
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InboundEnvelope
	for rows.Next() {
		var e InboundEnvelope
		var headers string
		if err := rows.Scan(&e.ID, &e.Source, &e.DedupeKey, &headers, &e.Payload,
			&e.Status, &e.Attempts, &e.LastError, &e.ReceivedAt, &e.UpdatedAt, &e.Owner); err != nil {
			return nil, err
		}
		e.Headers = decodeHeaders(headers)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SeenDedupeKey reports whether a (source, key) pair already exists.
func (s *SQLInboundStore) SeenDedupeKey(ctx context.Context, source, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	q := fmt.Sprintf("SELECT 1 FROM %s WHERE source = %s AND dedupe_key = %s LIMIT 1",
		s.table, s.placeholder(1), s.placeholder(2))
	var one int
	err := s.db.QueryRowContext(ctx, q, source, key).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReapTerminalBefore deletes processed envelopes older than cutoff.
func (s *SQLInboundStore) ReapTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return reapTerminalSQL(ctx, s.db, fmt.Sprintf(
		`DELETE FROM %s WHERE status = %s AND updated_at < %s`,
		s.table, s.placeholder(1), s.placeholder(2)),
		string(InboundStatusProcessed), cutoff)
}

const inboundColumns = "id, source, dedupe_key, headers, payload, status, attempts, last_error, received_at, updated_at, owner"

func (s *SQLInboundStore) insert() string {
	ph := make([]string, 11)
	for i := 1; i <= 11; i++ {
		ph[i-1] = s.placeholder(i)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", s.table, inboundColumns, strings.Join(ph, ", "))
}

func (s *SQLInboundStore) selectOne() string {
	return fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", inboundColumns, s.table, s.placeholder(1))
}

func (s *SQLInboundStore) update() string {
	return fmt.Sprintf(`UPDATE %s SET
		dedupe_key = COALESCE(NULLIF(CAST(%s AS TEXT), ''), dedupe_key), headers = %s, status = %s, last_error = %s, updated_at = %s
		WHERE id = %s`,
		s.table,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5),
		s.placeholder(6))
}

func (s *SQLInboundStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func encodeHeaders(h map[string]string) (string, error) {
	if len(h) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("webhook: marshal inbound headers: %w", err)
	}
	return string(b), nil
}

func decodeHeaders(s string) map[string]string {
	if s == "" || s == "{}" || s == "null" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

var _ InboundStore = (*SQLInboundStore)(nil)
