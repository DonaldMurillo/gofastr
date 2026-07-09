package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SQLInboundStore is a SQL-backed InboundStore. It mirrors SQLStore's
// conventions: dialect detected at construction (sqlite vs postgres),
// table created on first use, headers persisted as a JSON TEXT column.
//
// Schema:
//
//	webhook_inbound(
//	    id          TEXT PRIMARY KEY,
//	    source      TEXT NOT NULL,
//	    dedupe_key  TEXT NOT NULL DEFAULT '',  -- '' means no dedupe
//	    headers     TEXT NOT NULL DEFAULT '{}', -- JSON object, allowlisted subset
//	    payload     BLOB NOT NULL,
//	    status      TEXT NOT NULL,
//	    attempts    INTEGER NOT NULL DEFAULT 0,
//	    last_error  TEXT NOT NULL DEFAULT '',
//	    received_at TIMESTAMPTZ NOT NULL,
//	    updated_at  TIMESTAMPTZ NOT NULL
//	)
//
// Dedupe constraint: there is intentionally NO unique constraint on
// (source, dedupe_key). A portable partial index (Postgres) vs the
// NULL-coalescing trick (SQLite) can't be expressed in one DDL, and a
// plain unique index would forbid two legitimately-undedupe'd requests
// (empty key) from coexisting. Dedupe is enforced application-side via
// SeenDedupeKey; the (source, dedupe_key) index makes that lookup cheap.
// The check-then-insert race window is documented in SeenDedupeKey.
type SQLInboundStore struct {
	db      *sql.DB
	table   string
	dialect string // "sqlite" (default) | "postgres"
}

// InboundSQLOption configures SQLInboundStore.
type InboundSQLOption func(*SQLInboundStore)

// WithInboundTable overrides the default "webhook_inbound" table name.
func WithInboundTable(name string) InboundSQLOption {
	return func(s *SQLInboundStore) { s.table = name }
}

// NewSQLInboundStore constructs a SQL-backed InboundStore and ensures the
// table exists. Dialect is detected exactly like NewSQLStore: a Postgres
// version() string flips the dialect; everything else defaults to sqlite.
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
	if !safeIdent(s.table) {
		return nil, errors.New("webhook: unsafe inbound table name")
	}
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		if strings.Contains(strings.ToLower(v), "postgresql") {
			s.dialect = "postgres"
		}
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure inbound table: %w", err)
	}
	return s, nil
}

func (s *SQLInboundStore) ensureTable() error {
	ts := "DATETIME"
	if s.dialect == "postgres" {
		ts = "TIMESTAMPTZ"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY,
			source      TEXT NOT NULL,
			dedupe_key  TEXT NOT NULL DEFAULT '',
			headers     TEXT NOT NULL DEFAULT '{}',
			payload     BLOB NOT NULL,
			status      TEXT NOT NULL,
			attempts    INTEGER NOT NULL DEFAULT 0,
			last_error  TEXT NOT NULL DEFAULT '',
			received_at %s NOT NULL,
			updated_at  %s NOT NULL
		)`, s.table, ts, ts),
		// Lookup indexes — no unique constraint (see type doc).
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_dedupe_idx ON %s (source, dedupe_key)",
			s.table, s.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_status_idx ON %s (status)",
			s.table, s.table),
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ----- store methods --------------------------------------------------------

// AddEnvelope inserts e. Headers are JSON-encoded into the headers column.
func (s *SQLInboundStore) AddEnvelope(ctx context.Context, e InboundEnvelope) error {
	headers, err := encodeHeaders(e.Headers)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.insert(), e.ID, e.Source, e.DedupeKey,
		headers, e.Payload, e.Status, e.Attempts, e.LastError, e.ReceivedAt, e.UpdatedAt)
	return err
}

// GetEnvelope returns (nil, nil) for an unknown id.
func (s *SQLInboundStore) GetEnvelope(ctx context.Context, id string) (*InboundEnvelope, error) {
	var e InboundEnvelope
	var headers string
	err := s.db.QueryRowContext(ctx, s.selectOne(), id).
		Scan(&e.ID, &e.Source, &e.DedupeKey, &headers, &e.Payload, &e.Status,
			&e.Attempts, &e.LastError, &e.ReceivedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Headers = decodeHeaders(headers)
	return &e, nil
}

// UpdateEnvelope overwrites the mutable columns (status, attempts, last_error,
// headers, updated_at) for e.ID. Source/payload/received_at are immutable.
func (s *SQLInboundStore) UpdateEnvelope(ctx context.Context, e InboundEnvelope) error {
	headers, err := encodeHeaders(e.Headers)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.update(),
		e.DedupeKey, headers, e.Status, e.Attempts, e.LastError, e.UpdatedAt, e.ID)
	return err
}

// ListEnvelopes returns envelopes filtered by status (empty = all),
// newest-received first, capped at limit (0 = no cap).
func (s *SQLInboundStore) ListEnvelopes(ctx context.Context, status string, limit int) ([]InboundEnvelope, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT id, source, dedupe_key, headers, payload, status, attempts, last_error, received_at, updated_at FROM %s", s.table)
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
			&e.Status, &e.Attempts, &e.LastError, &e.ReceivedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Headers = decodeHeaders(headers)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SeenDedupeKey reports whether a (source, key) pair already exists. Empty
// key returns (false, nil) without hitting the DB.
//
// As noted on the type, there is no unique constraint, so two concurrent
// requests with the same key can both pass this check before either
// inserts — the same check-then-insert race as the memory store. A unique
// index would close it but can't be expressed portably across sqlite and
// postgres without dialect branches; callers that need strict exactly-once
// should serialize at the source or accept the duplicate-suppression done
// downstream (idempotent ProcessInbound handlers).
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

// ----- statements -----------------------------------------------------------

// columns lists the persisted column order, shared by insert/select so the
// two never drift out of sync.
const inboundColumns = "id, source, dedupe_key, headers, payload, status, attempts, last_error, received_at, updated_at"

func (s *SQLInboundStore) insert() string {
	ph := make([]string, 10)
	for i := 1; i <= 10; i++ {
		ph[i-1] = s.placeholder(i)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", s.table, inboundColumns, strings.Join(ph, ", "))
}

func (s *SQLInboundStore) selectOne() string {
	return fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", inboundColumns, s.table, s.placeholder(1))
}

func (s *SQLInboundStore) update() string {
	// dedupe_key is updatable: on the queue path the ingest handler writes
	// it only AFTER a successful enqueue (see IngestHandler), so the durably-
	// queued event can dedupe-ack the sender's redeliveries.
	return fmt.Sprintf(`UPDATE %s SET
		dedupe_key = %s, headers = %s, status = %s, attempts = %s, last_error = %s, updated_at = %s
		WHERE id = %s`,
		s.table,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5),
		s.placeholder(6), s.placeholder(7))
}

func (s *SQLInboundStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// ----- header codec ---------------------------------------------------------

// encodeHeaders marshals a header map to the JSON TEXT column value. A nil
// map is encoded as "{}" (not "null") so the column always holds an object.
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

// decodeHeaders is the inverse of encodeHeaders; "{}"/"null"/empty yield nil.
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

// Compile-time interface satisfaction.
var _ InboundStore = (*SQLInboundStore)(nil)
