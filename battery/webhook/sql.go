package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db/sqlbusy"
)

// SQLStore is a SQL-backed Store over a subscribers and a deliveries table, on
// SQLite or Postgres.
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

// WithSQLSecretCodec installs the SecretCodec that seals subscriber secrets
// and credentials.
func WithSQLSecretCodec(c SecretCodec) SQLOption {
	return func(s *SQLStore) {
		if c != nil {
			s.codec = c
		}
	}
}

// WithSQLAllowPlaintext opts into plaintext secret storage; without it or a
// codec NewSQLStore errors.
func WithSQLAllowPlaintext() SQLOption {
	return func(s *SQLStore) {
		s.codec = NoopSecretCodec{}
	}
}

// NewSQLStore constructs a SQLStore and ensures both tables exist.
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
	if s.codec == nil {
		return nil, errors.New("webhook: NewSQLStore requires WithSQLSecretCodec(...) or WithSQLAllowPlaintext(): refusing to silently store subscriber secrets in cleartext")
	}
	if !query.SafeTableName(s.subTable) || !query.SafeTableName(s.delTable) {
		return nil, errors.New("webhook: unsafe table name")
	}
	if query.IsPostgres(db) {
		s.dialect = "postgres"
	}
	if err := s.ensureTables(); err != nil {
		return nil, fmt.Errorf("ensure tables: %w", err)
	}
	registerDeliveries(s.delTable)
	return s, nil
}

func (s *SQLStore) ensureTables() error {
	ts, blob := "DATETIME", "BLOB"
	if s.dialect == "postgres" {
		ts, blob = "TIMESTAMPTZ", "BYTEA"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id      TEXT PRIMARY KEY NOT NULL,
			url     TEXT NOT NULL,
			secret  TEXT NOT NULL,
			events  TEXT NOT NULL,
			active  INTEGER NOT NULL,
			created %s NOT NULL,
			auth    TEXT NOT NULL DEFAULT ''
		)`, s.subTable, ts),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id              TEXT PRIMARY KEY NOT NULL,
			subscriber_id   TEXT NOT NULL,
			event           TEXT NOT NULL,
			payload         %s NOT NULL,
			attempts        INTEGER NOT NULL,
			status          TEXT NOT NULL,
			last_error      TEXT NOT NULL,
			next_attempt_at %s,
			created_at      %s NOT NULL,
			updated_at      %s NOT NULL,
			owner           TEXT NOT NULL DEFAULT '',
			response_status INTEGER NOT NULL DEFAULT 0,
			response_body   TEXT NOT NULL DEFAULT ''
		)`, s.delTable, blob, ts, ts, ts),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (status, next_attempt_at)",
			query.IndexOn(s.dialect == "sqlite", s.delTable+"_due_idx", s.delTable)),
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	for _, def := range []string{
		"owner TEXT NOT NULL DEFAULT ''",
		"response_status INTEGER NOT NULL DEFAULT 0",
		"response_body TEXT NOT NULL DEFAULT ''",
	} {
		if err := addColumn(s.db, s.dialect, s.delTable, def); err != nil {
			return err
		}
	}
	if err := addColumn(s.db, s.dialect, s.subTable, "auth TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if s.dialect == "postgres" {
		return nil
	}
	return s.utcDueTimes()
}

func (s *SQLStore) utcDueTimes() error {
	rows, err := s.db.Query(fmt.Sprintf(`SELECT id, next_attempt_at, CAST(next_attempt_at AS TEXT) FROM %s
		WHERE status = ? AND next_attempt_at IS NOT NULL AND CAST(next_attempt_at AS TEXT) NOT LIKE '%%+00:00'`,
		s.delTable), string(StatusPending))
	if err != nil {
		return err
	}
	type zoned struct {
		id, text string
		at       time.Time
	}
	var todo []zoned
	for rows.Next() {
		var z zoned
		if err := rows.Scan(&z.id, &z.at, &z.text); err != nil {
			rows.Close()
			return err
		}
		if _, off := z.at.Zone(); off != 0 {
			todo = append(todo, z)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, z := range todo {
		if err := s.rewriteDueTime(z.id, z.text, z.at); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) rewriteDueTime(id, read string, at time.Time) error {
	_, err := s.db.Exec(fmt.Sprintf(`UPDATE %s SET next_attempt_at = ? WHERE id = ? AND next_attempt_at = ?`,
		s.delTable), at.UTC(), id, read)
	return err
}

func addColumn(db *sql.DB, dialect, table, def string) error {
	if dialect == "postgres" {
		_, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s`, table, def))
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s`, table, def))
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

func registerDeliveries(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("webhook_deliveries", table), Source: "webhook", Table: table, PrimaryKey: "id",
		Columns: []string{"id", "subscriber_id", "event", "payload", "attempts", "status",
			"last_error", "next_attempt_at", "created_at", "updated_at", "owner", "response_status", "response_body"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("webhook_deliveries", table), Source: "webhook", Table: table, Column: "owner",
		Mode: datexport.EraseDelete,
	})
}

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
	encAuth, err := s.encodeAuth(sub.Auth)
	if err != nil {
		return err
	}
	upsert := s.subUpsert()
	_, err = s.db.ExecContext(ctx, upsert, sub.ID, sub.URL, encSecret, string(events), active, sub.Created, encAuth)
	return err
}

func (s *SQLStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT id, url, secret, events, active, created, auth FROM %s WHERE id = %s",
			s.subTable, s.placeholder(1)),
		id,
	)
	sub, sealedAuth, err := scanSubscriber(row)
	if err != nil || sub == nil {
		return sub, err
	}
	if err := s.openSubscriber(sub, sealedAuth); err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *SQLStore) ListSubscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT id, url, secret, events, active, created, auth FROM %s ORDER BY id", s.subTable),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		sub, sealedAuth, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}
		if sub == nil {
			continue
		}
		if err := s.openSubscriber(sub, sealedAuth); err != nil {
			return nil, fmt.Errorf("webhook: subscriber %q: %w", sub.ID, err)
		}
		out = append(out, *sub)
	}
	return out, rows.Err()
}

func (s *SQLStore) openSubscriber(sub *Subscriber, sealedAuth string) error {
	plain, err := s.codec.Decode(sub.Secret)
	if err != nil {
		return fmt.Errorf("webhook: decode secret: %w", err)
	}
	sub.Secret = plain
	if sealedAuth == "" {
		return nil
	}
	plainAuth, err := s.codec.Decode(sealedAuth)
	if err != nil {
		return fmt.Errorf("webhook: decode auth: %w", err)
	}
	sub.Auth, err = decodeAuth(plainAuth)
	return err
}

func (s *SQLStore) encodeAuth(a Auth) (string, error) {
	plain, err := encodeAuth(a)
	if err != nil || plain == "" {
		return "", err
	}
	enc, err := s.codec.Encode(plain)
	if err != nil {
		return "", fmt.Errorf("webhook: encode auth: %w", err)
	}
	return enc, nil
}

func (s *SQLStore) DeleteSubscriber(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id = %s", s.subTable, s.placeholder(1)),
		id,
	)
	return err
}

func (s *SQLStore) AddDelivery(ctx context.Context, d Delivery) error {
	var nextAt any
	if !d.NextAttemptAt.IsZero() {
		nextAt = d.NextAttemptAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, s.deliveryInsert(),
		d.ID, d.SubscriberID, d.Event, d.Payload, d.Attempts,
		string(d.Status), d.LastError, nextAt, d.CreatedAt.UTC(), d.UpdatedAt.UTC(), d.Owner,
		d.ResponseStatus, d.ResponseBody,
	)
	return err
}

// UpdateDelivery persists a delivery's post-attempt state, never writing
// attempts, fenced against a stale settle.
func (s *SQLStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	var nextAt any
	if !d.NextAttemptAt.IsZero() {
		nextAt = d.NextAttemptAt.UTC()
	}
	q := s.deliveryUpdate(d.unclaimedAttempt)
	args := []any{string(d.Status), d.LastError, nextAt, d.UpdatedAt.UTC(), d.ResponseStatus, d.ResponseBody, d.ID}
	if fence := settleFence(d.Status); len(fence) > 0 {
		marks := make([]string, len(fence))
		for i, st := range fence {
			marks[i] = s.placeholder(len(args) + 1)
			args = append(args, string(st))
		}
		q += ` AND status NOT IN (` + strings.Join(marks, ",") + `)`
	}
	if leaseFenced(d) {
		q += ` AND next_attempt_at = ` + s.placeholder(len(args)+1)
		args = append(args, d.claimedUntil)
	}
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := s.db.ExecContext(ctx, q, args...)
		return err
	})
}

func (s *SQLStore) ListDeliveries(ctx context.Context, subscriberID string, limit int) ([]Delivery, error) {
	q := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s`, s.delTable)
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

// ListDeadDeliveries implements ReplayableStore.
func (s *SQLStore) ListDeadDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	q := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s
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

// ResetDelivery implements ReplayableStore; only a dead row changes.
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
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s
		WHERE status = %s AND (next_attempt_at IS NULL OR next_attempt_at <= %s)
		ORDER BY next_attempt_at ASC`,
		s.delTable, s.placeholder(1), s.placeholder(2))
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, string(StatusPending), now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveries(rows)
}

// ClaimDueDeliveries implements LeasedStore. A zero leasePeriod keeps the 30s
// default; a negative one is an error.
func (s *SQLStore) ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	if leasePeriod < 0 {
		return nil, fmt.Errorf("webhook: SQLStore.ClaimDueDeliveries: leasePeriod must be >= 0 (got %v)", leasePeriod)
	}
	if leasePeriod == 0 {
		leasePeriod = 30 * time.Second
	}
	if limit <= 0 {
		limit = 32
	}
	now = now.UTC()
	if s.dialect == "postgres" {
		return s.claimPostgres(ctx, now, limit, leasePeriod)
	}
	return s.claimSqlite(ctx, now, limit, leasePeriod)
}

// ReapTerminalBefore deletes successful deliveries older than cutoff; dead
// ones are kept for Replay.
func (s *SQLStore) ReapTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return reapTerminalSQL(ctx, s.db, fmt.Sprintf(
		`DELETE FROM %s WHERE status = %s AND updated_at < %s`,
		s.delTable, s.placeholder(1), s.placeholder(2)),
		string(StatusSuccess), cutoff)
}

func (s *SQLStore) releaseClaim(ctx context.Context, d Delivery, now time.Time) error {
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET
			next_attempt_at = %s, updated_at = %s, attempts = attempts - 1
			WHERE id = %s AND next_attempt_at = %s`,
			s.delTable, s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4)),
			now.UTC(), now.UTC(), d.ID, d.claimedUntil)
		return err
	})
}

func reapTerminalSQL(ctx context.Context, db *sql.DB, stmt, status string, cutoff time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, stmt, status, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *SQLStore) claimPostgres(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	q := fmt.Sprintf(`UPDATE %s SET next_attempt_at = $1, updated_at = $1, attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM %s
			WHERE status = $2 AND (next_attempt_at IS NULL OR next_attempt_at <= $3)
			ORDER BY next_attempt_at ASC
			LIMIT %d
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, subscriber_id, event, payload, attempts, status,
			last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body`,
		s.delTable, s.delTable, limit)
	rows, err := s.db.QueryContext(ctx, q, now.Add(leasePeriod), string(StatusPending), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed, err := scanDeliveries(rows)
	for i := range claimed {
		claimed[i].claimedUntil = claimed[i].NextAttemptAt
	}
	return claimed, err
}

func (s *SQLStore) claimSqlite(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			//gofastr:allow(poisonedconn) SQLite BEGIN IMMEDIATE claim: since SQLite 3.7.11 ROLLBACK aborts pending reads instead of failing, so its error here means no transaction is left active, and retiring the conn would only drop a :memory: database
			if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
				slog.Default().Warn("webhook: rolling back an abandoned claim", "err", err)
			}
		}
	}()
	commit := func() error {
		_, err := conn.ExecContext(ctx, "COMMIT")
		committed = err == nil
		return err
	}

	selQ := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s
		WHERE status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		ORDER BY next_attempt_at ASC LIMIT %d`, s.delTable, limit)
	rows, err := conn.QueryContext(ctx, selQ, string(StatusPending), now)
	if err != nil {
		return nil, err
	}
	claimed, err := scanDeliveries(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(claimed) == 0 {
		return nil, commit()
	}
	placeholders := make([]string, len(claimed))
	args := []any{now.Add(leasePeriod)}
	for i, d := range claimed {
		placeholders[i] = "?"
		args = append(args, d.ID)
	}
	updQ := fmt.Sprintf("UPDATE %s SET next_attempt_at = ?, updated_at = ?, attempts = attempts + 1 WHERE id IN (%s)",
		s.delTable, strings.Join(placeholders, ","))
	args = append([]any{args[0], now.Add(leasePeriod)}, args[1:]...)
	if _, err := conn.ExecContext(ctx, updQ, args...); err != nil {
		return nil, err
	}
	for i := range claimed {
		claimed[i].Attempts++
		claimed[i].NextAttemptAt = now.Add(leasePeriod)
		claimed[i].UpdatedAt = now.Add(leasePeriod)
		claimed[i].claimedUntil = now.Add(leasePeriod)
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSubscriber(r rowScanner) (*Subscriber, string, error) {
	var sub Subscriber
	var sealedAuth string
	var events string
	var active int
	err := r.Scan(&sub.ID, &sub.URL, &sub.Secret, &events, &active, &sub.Created, &sealedAuth)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, "", nil
	case err != nil:
		return nil, "", err
	}
	sub.Active = active != 0
	sub.Paused = !sub.Active
	if events != "" {
		if err := json.Unmarshal([]byte(events), &sub.Events); err != nil {
			return nil, "", fmt.Errorf("webhook: subscriber %s: corrupt events column: %w", sub.ID, err)
		}
	}
	return &sub, sealedAuth, nil
}

func scanDeliveries(rows *sql.Rows) ([]Delivery, error) {
	var out []Delivery
	for rows.Next() {
		var d Delivery
		var status, lastErr string
		var nextAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.SubscriberID, &d.Event, &d.Payload, &d.Attempts,
			&status, &lastErr, &nextAt, &d.CreatedAt, &d.UpdatedAt, &d.Owner,
			&d.ResponseStatus, &d.ResponseBody); err != nil {
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
		return fmt.Sprintf(`INSERT INTO %s AS t (id, url, secret, events, active, created, auth)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (id) DO UPDATE SET
				url     = EXCLUDED.url,
				secret  = EXCLUDED.secret,
				events  = EXCLUDED.events,
				active  = EXCLUDED.active,
				auth    = EXCLUDED.auth`, s.subTable)
	}
	return fmt.Sprintf(`INSERT INTO %s AS t (id, url, secret, events, active, created, auth)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			url     = excluded.url,
			secret  = excluded.secret,
			events  = excluded.events,
			active  = excluded.active,
			auth    = excluded.auth`, s.subTable)
}

func (s *SQLStore) deliveryInsert() string {
	if s.dialect == "postgres" {
		return fmt.Sprintf(`INSERT INTO %s
			(id, subscriber_id, event, payload, attempts, status, last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, s.delTable)
	}
	return fmt.Sprintf(`INSERT INTO %s
		(id, subscriber_id, event, payload, attempts, status, last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.delTable)
}

func (s *SQLStore) deliveryUpdate(unclaimed bool) string {
	bump := ""
	if unclaimed {
		bump = ", attempts = attempts + 1"
	}
	if s.dialect == "postgres" {
		return fmt.Sprintf(`UPDATE %s SET
			status = $1, last_error = $2, next_attempt_at = $3, updated_at = $4,
			response_status = $5, response_body = $6%s
			WHERE id = $7`, s.delTable, bump)
	}
	return fmt.Sprintf(`UPDATE %s SET
		status = ?, last_error = ?, next_attempt_at = ?, updated_at = ?,
		response_status = ?, response_body = ?%s
		WHERE id = ?`, s.delTable, bump)
}

func (s *SQLStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}
