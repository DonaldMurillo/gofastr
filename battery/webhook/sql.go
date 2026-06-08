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
	db       *sql.DB
	subTable string
	delTable string
	dialect  string
	codec    SecretCodec
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

// WithSQLSecretCodec installs a SecretCodec that encrypts subscriber
// secrets at write time and decrypts them at read time.
//
// Use [NewAESGCMSecretCodec] to wrap a 16/24/32-byte key. Callers that
// genuinely want plaintext storage (DB encrypted at rest, threat model
// excludes a DB-snapshot attacker) must use [WithSQLAllowPlaintext]
// explicitly — there is no plaintext default.
func WithSQLSecretCodec(c SecretCodec) SQLOption {
	return func(s *SQLStore) {
		if c != nil {
			s.codec = c
		}
	}
}

// WithSQLAllowPlaintext opts the SQLStore into plaintext secret
// storage using [NoopSecretCodec]. Without this (or an explicit
// [WithSQLSecretCodec]) NewSQLStore returns an error rather than
// silently storing subscriber secrets in cleartext.
func WithSQLAllowPlaintext() SQLOption {
	return func(s *SQLStore) {
		s.codec = NoopSecretCodec{}
	}
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
		// codec deliberately nil — callers must choose explicitly
		// (WithSQLSecretCodec for AES-GCM, or WithSQLAllowPlaintext
		// to acknowledge plaintext storage).
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.codec == nil {
		return nil, errors.New("webhook: NewSQLStore requires WithSQLSecretCodec(...) or WithSQLAllowPlaintext() — refusing to silently store subscriber secrets in cleartext")
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
	encSecret, err := s.codec.Encode(sub.Secret)
	if err != nil {
		return fmt.Errorf("webhook: encode secret: %w", err)
	}
	upsert := s.subUpsert()
	_, err = s.db.ExecContext(ctx, upsert, sub.ID, sub.URL, encSecret, string(events), active, sub.Created)
	return err
}

func (s *SQLStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT id, url, secret, events, active, created FROM %s WHERE id = %s",
			s.subTable, s.placeholder(1)),
		id,
	)
	sub, err := scanSubscriber(row)
	if err != nil || sub == nil {
		return sub, err
	}
	plain, err := s.codec.Decode(sub.Secret)
	if err != nil {
		return nil, fmt.Errorf("webhook: decode secret: %w", err)
	}
	sub.Secret = plain
	return sub, nil
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
		if sub == nil {
			continue
		}
		plain, err := s.codec.Decode(sub.Secret)
		if err != nil {
			return nil, fmt.Errorf("webhook: decode secret for %q: %w", sub.ID, err)
		}
		sub.Secret = plain
		out = append(out, *sub)
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

// ListDeadDeliveries implements [ReplayableStore]: terminally-failed
// (StatusDead) deliveries, newest-first.
func (s *SQLStore) ListDeadDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	q := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at FROM %s
		WHERE status = %s ORDER BY created_at DESC`, s.delTable, s.placeholder(1))
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, string(StatusDead))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// ResetDelivery implements [ReplayableStore]: returns a dead delivery to
// pending (attempts + error cleared, due now). The `AND status = dead` clause
// makes it idempotent — resetting a non-dead/unknown delivery matches no row
// and is a no-op, so it can never resurrect an in-flight or delivered one.
func (s *SQLStore) ResetDelivery(ctx context.Context, id string) error {
	now := time.Now().UTC()
	q := fmt.Sprintf(`UPDATE %s SET status = %s, attempts = 0, last_error = '',
		next_attempt_at = %s, updated_at = %s WHERE id = %s AND status = %s`,
		s.delTable, s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5))
	_, err := s.db.ExecContext(ctx, q, string(StatusPending), now, now, id, string(StatusDead))
	return err
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

// ClaimDueDeliveries atomically reserves up to `limit` pending rows
// whose next_attempt_at <= now, pushing them to now+leasePeriod so
// concurrent workers skip them.
//
// Postgres uses FOR UPDATE SKIP LOCKED inside a CTE so the inner
// SELECT only sees uncontested rows; SQLite serializes writers via
// BEGIN IMMEDIATE so the SELECT-then-UPDATE sequence inside one tx
// is safely exclusive.
func (s *SQLStore) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	if leasePeriod <= 0 {
		leasePeriod = 30 * time.Second
	}
	if limit <= 0 {
		limit = 32
	}
	if s.dialect == "postgres" {
		return s.claimPostgres(ctx, now, limit, leasePeriod)
	}
	return s.claimSqlite(ctx, now, limit, leasePeriod)
}

func (s *SQLStore) claimPostgres(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	q := fmt.Sprintf(`UPDATE %s SET next_attempt_at = $1, updated_at = $1
		WHERE id IN (
			SELECT id FROM %s
			WHERE status = $2 AND next_attempt_at <= $3
			ORDER BY next_attempt_at ASC
			LIMIT %d
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, subscriber_id, event, payload, attempts, status,
			last_error, next_attempt_at, created_at, updated_at`,
		s.delTable, s.delTable, limit)
	rows, err := s.db.QueryContext(ctx, q, now.Add(leasePeriod), string(StatusPending), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

func (s *SQLStore) claimSqlite(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	// BEGIN IMMEDIATE acquires the database write lock up-front so two
	// concurrent workers serialize cleanly. The SELECT-then-UPDATE
	// sequence inside the tx sees a consistent snapshot.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	selQ := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at FROM %s
		WHERE status = ? AND next_attempt_at <= ?
		ORDER BY next_attempt_at ASC LIMIT %d`, s.delTable, limit)
	rows, err := tx.QueryContext(ctx, selQ, string(StatusPending), now)
	if err != nil {
		return nil, err
	}
	claimed, err := scanDeliveries(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(claimed) == 0 {
		return nil, tx.Commit()
	}
	// Push next_attempt_at forward for every claimed row.
	placeholders := make([]string, len(claimed))
	args := []any{now.Add(leasePeriod)}
	for i, d := range claimed {
		placeholders[i] = "?"
		args = append(args, d.ID)
	}
	updQ := fmt.Sprintf("UPDATE %s SET next_attempt_at = ?, updated_at = ? WHERE id IN (%s)",
		s.delTable, strings.Join(placeholders, ","))
	// Insert the second timestamp arg after the first.
	args = append([]any{args[0], now.Add(leasePeriod)}, args[1:]...)
	if _, err := tx.ExecContext(ctx, updQ, args...); err != nil {
		return nil, err
	}
	// Reflect the new lease in the in-memory copies we return.
	for i := range claimed {
		claimed[i].NextAttemptAt = now.Add(leasePeriod)
		claimed[i].UpdatedAt = now.Add(leasePeriod)
	}
	return claimed, tx.Commit()
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
