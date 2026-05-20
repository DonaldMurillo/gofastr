package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQLStore is a SQL-backed Store. Subscribers and deliveries each
// live in a single table; allow lists and payloads are persisted in
// place rather than normalised, since fan-out cardinality is small
// and replay needs the body intact.
//
// Dialect is detected at construction (sqlite vs postgres).
//
// Schemas:
//
//	webhook_subscribers(
//	    id       TEXT PRIMARY KEY,
//	    url      TEXT NOT NULL,
//	    secret   TEXT NOT NULL,
//	    events   TEXT NOT NULL,   -- JSON array
//	    active   INTEGER NOT NULL,
//	    created  TIMESTAMP NOT NULL
//	)
//
//	webhook_deliveries(
//	    id              TEXT PRIMARY KEY,
//	    subscriber_id   TEXT NOT NULL,
//	    event           TEXT NOT NULL,
//	    payload         BLOB NOT NULL,
//	    attempts        INTEGER NOT NULL,
//	    status          TEXT NOT NULL,
//	    last_error      TEXT NOT NULL,
//	    next_attempt_at TIMESTAMP,
//	    created_at      TIMESTAMP NOT NULL,
//	    updated_at      TIMESTAMP NOT NULL
//	)
type SQLStore struct {
	db          *sql.DB
	subTable    string
	delTable    string
	dialect     string
}

// SQLOption configures SQLStore.
type SQLOption func(*SQLStore)

// WithSQLSubscribersTable overrides the default "webhook_subscribers" table name.
func WithSQLSubscribersTable(name string) SQLOption {
	return func(s *SQLStore) { s.subTable = name }
}

// WithSQLDeliveriesTable overrides the default "webhook_deliveries" table name.
func WithSQLDeliveriesTable(name string) SQLOption {
	return func(s *SQLStore) { s.delTable = name }
}

// NewSQLStore constructs a SQL-backed Store and ensures both tables exist.
func NewSQLStore(db *sql.DB, opts ...SQLOption) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("webhook: nil DB")
	}
	s := &SQLStore{
		db:       db,
		subTable: "webhook_subscribers",
		delTable: "webhook_deliveries",
		dialect:  "sqlite",
	}
	for _, opt := range opts {
		opt(s)
	}
	if !safeIdent(s.subTable) || !safeIdent(s.delTable) {
		return nil, errors.New("webhook: unsafe table name")
	}
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		if strings.Contains(strings.ToLower(v), "postgresql") {
			s.dialect = "postgres"
		}
	}
	if err := s.ensureTables(); err != nil {
		return nil, fmt.Errorf("ensure tables: %w", err)
	}
	return s, nil
}

func (s *SQLStore) ensureTables() error {
	ts := "DATETIME"
	if s.dialect == "postgres" {
		ts = "TIMESTAMPTZ"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id      TEXT PRIMARY KEY,
			url     TEXT NOT NULL,
			secret  TEXT NOT NULL,
			events  TEXT NOT NULL,
			active  INTEGER NOT NULL,
			created %s NOT NULL
		)`, s.subTable, ts),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id              TEXT PRIMARY KEY,
			subscriber_id   TEXT NOT NULL,
			event           TEXT NOT NULL,
			payload         BLOB NOT NULL,
			attempts        INTEGER NOT NULL,
			status          TEXT NOT NULL,
			last_error      TEXT NOT NULL,
			next_attempt_at %s,
			created_at      %s NOT NULL,
			updated_at      %s NOT NULL
		)`, s.delTable, ts, ts, ts),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s_due_idx ON %s (status, next_attempt_at)",
			s.delTable, s.delTable),
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ----- subscribers ----------------------------------------------------------

func (s *SQLStore) AddSubscriber(ctx context.Context, sub Subscriber) error {
	events, err := json.Marshal(sub.Events)
	if err != nil {
		return err
	}
	active := 0
	if sub.Active {
		active = 1
	}
	upsert := s.subUpsert()
	_, err = s.db.ExecContext(ctx, upsert, sub.ID, sub.URL, sub.Secret, string(events), active, sub.Created)
	return err
}

func (s *SQLStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT id, url, secret, events, active, created FROM %s WHERE id = %s",
			s.subTable, s.placeholder(1)),
		id,
	)
	return scanSubscriber(row)
}

func (s *SQLStore) ListSubscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT id, url, secret, events, active, created FROM %s ORDER BY id", s.subTable),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		sub, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		if sub != nil {
			out = append(out, *sub)
		}
	}
	return out, rows.Err()
}

func (s *SQLStore) DeleteSubscriber(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id = %s", s.subTable, s.placeholder(1)),
		id,
	)
	return err
}

// ----- deliveries -----------------------------------------------------------

func (s *SQLStore) AddDelivery(ctx context.Context, d Delivery) error {
	var nextAt any
	if !d.NextAttemptAt.IsZero() {
		nextAt = d.NextAttemptAt
	}
	_, err := s.db.ExecContext(ctx, s.deliveryInsert(),
		d.ID, d.SubscriberID, d.Event, d.Payload, d.Attempts,
		string(d.Status), d.LastError, nextAt, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (s *SQLStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	var nextAt any
	if !d.NextAttemptAt.IsZero() {
		nextAt = d.NextAttemptAt
	}
	_, err := s.db.ExecContext(ctx, s.deliveryUpdate(),
		d.Attempts, string(d.Status), d.LastError, nextAt, d.UpdatedAt, d.ID,
	)
	return err
}

func (s *SQLStore) ListDeliveries(ctx context.Context, subscriberID string, limit int) ([]Delivery, error) {
	q := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at FROM %s`, s.delTable)
	var args []any
	if subscriberID != "" {
		q += " WHERE subscriber_id = " + s.placeholder(1)
		args = append(args, subscriberID)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

func (s *SQLStore) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	q := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at FROM %s
		WHERE status = %s AND next_attempt_at <= %s
		ORDER BY next_attempt_at ASC`,
		s.delTable, s.placeholder(1), s.placeholder(2))
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, string(StatusPending), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// ----- scan + statements ----------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSubscriber(r rowScanner) (*Subscriber, error) {
	var sub Subscriber
	var events string
	var active int
	err := r.Scan(&sub.ID, &sub.URL, &sub.Secret, &events, &active, &sub.Created)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	sub.Active = active != 0
	if events != "" {
		_ = json.Unmarshal([]byte(events), &sub.Events)
	}
	return &sub, nil
}

func scanDeliveries(rows *sql.Rows) ([]Delivery, error) {
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var status, lastErr string
		var nextAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.SubscriberID, &d.Event, &d.Payload, &d.Attempts,
			&status, &lastErr, &nextAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Status = DeliveryStatus(status)
		d.LastError = lastErr
		if nextAt.Valid {
			d.NextAttemptAt = nextAt.Time
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLStore) subUpsert() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(`INSERT INTO %s (id, url, secret, events, active, created)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (id) DO UPDATE SET
				url     = EXCLUDED.url,
				secret  = EXCLUDED.secret,
				events  = EXCLUDED.events,
				active  = EXCLUDED.active`, s.subTable)
	}
	return fmt.Sprintf(`INSERT INTO %s (id, url, secret, events, active, created)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			url     = excluded.url,
			secret  = excluded.secret,
			events  = excluded.events,
			active  = excluded.active`, s.subTable)
}

func (s *SQLStore) deliveryInsert() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(`INSERT INTO %s
			(id, subscriber_id, event, payload, attempts, status, last_error, next_attempt_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, s.delTable)
	}
	return fmt.Sprintf(`INSERT INTO %s
		(id, subscriber_id, event, payload, attempts, status, last_error, next_attempt_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, s.delTable)
}

func (s *SQLStore) deliveryUpdate() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(`UPDATE %s SET
			attempts = $1, status = $2, last_error = $3, next_attempt_at = $4, updated_at = $5
			WHERE id = $6`, s.delTable)
	}
	return fmt.Sprintf(`UPDATE %s SET
		attempts = ?, status = ?, last_error = ?, next_attempt_at = ?, updated_at = ?
		WHERE id = ?`, s.delTable)
}

func (s *SQLStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func safeIdent(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !ok {
			return false
		}
	}
	return true
}
