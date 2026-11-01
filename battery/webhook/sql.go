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
//	    created  TIMESTAMP NOT NULL,
//	    auth     TEXT NOT NULL,   -- Subscriber.Auth, sealed with the codec
//	)
//
//	webhook_deliveries(
//	    id              TEXT PRIMARY KEY,
//	    subscriber_id   TEXT NOT NULL,
//	    event           TEXT NOT NULL,
//	    payload         BLOB NOT NULL,           -- BYTEA on postgres
//	    attempts        INTEGER NOT NULL,
//	    status          TEXT NOT NULL,
//	    last_error      TEXT NOT NULL,
//	    next_attempt_at TIMESTAMP,
//	    created_at      TIMESTAMP NOT NULL,
//	    updated_at      TIMESTAMP NOT NULL,
//	    owner           TEXT NOT NULL,
//	    response_status INTEGER NOT NULL,
//	    response_body   TEXT NOT NULL
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
// explicitly, there is no plaintext default.
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
		// codec deliberately nil, callers must choose explicitly
		// (WithSQLSecretCodec for AES-GCM, or WithSQLAllowPlaintext
		// to acknowledge plaintext storage).
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
		// Postgres has no BLOB type; the byte-array spelling is BYTEA. The
		// timestamp column was already switched here, which is what made
		// leaving the payload column behind a construction-time failure on
		// every Postgres app rather than a subtle storage difference.
		ts, blob = "TIMESTAMPTZ", "BYTEA"
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id      TEXT PRIMARY KEY,
			url     TEXT NOT NULL,
			secret  TEXT NOT NULL,
			events  TEXT NOT NULL,
			active  INTEGER NOT NULL,
			created %s NOT NULL,
			auth    TEXT NOT NULL DEFAULT ''
		)`, s.subTable, ts),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id              TEXT PRIMARY KEY,
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
	// Columns later releases added, for a table an earlier one made.
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

// utcDueTimes rewrites in UTC the due times an earlier release stored in
// the writer's zone. SQLite keeps a timestamp as text with its offset and
// the claim compares that text, so a pending row written ten hours east of
// UTC read as due ten hours late. Only a pending row's due time is read by
// the claim. Each rewrite is fenced on the text it read, so a claim
// another process made in between keeps its lease.
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

// rewriteDueTime stores at in UTC as the row's due time, only while the
// row still holds the text read.
func (s *SQLStore) rewriteDueTime(id, read string, at time.Time) error {
	_, err := s.db.Exec(fmt.Sprintf(`UPDATE %s SET next_attempt_at = ? WHERE id = ? AND next_attempt_at = ?`,
		s.delTable), at.UTC(), id, read)
	return err
}

// addColumn upgrades a table an earlier release created without the
// column def declares (a constant "name TYPE ..." from ensureTables);
// existing rows take its default: an owner naming nobody, no recorded
// response, no auth.
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

// registerDeliveries declares the delivery table to App.ExportData and
// App.EraseUserData under its configured name. Erasure deletes the
// erased user's deliveries (Delivery.Owner), pending ones included: a
// payload carrying their data is not sent after they are erased.
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

// openSubscriber decodes the sealed secret scanned into sub and the
// sealed auth column beside it.
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

// encodeAuth seals a subscriber's credential with the secret codec;
// no credential is stored as "".
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

// ----- deliveries -----------------------------------------------------------

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

// UpdateDelivery persists a delivery's post-attempt state. The attempts
// column is OWNED BY THE CLAIM: ClaimDueDeliveries consumes one attempt per
// claim (attempts = attempts + 1, see claimPostgres), and this write never
// touches the counter — d.Attempts is a claim-time snapshot, and persisting
// it absolutely let a stale claimant's settle rewind the attempts a
// re-claimant's claim had consumed (two overlapping runners recorded one
// attempt instead of two, silently doubling the MaxAttempts budget). The
// one exception is a delivery the Manager's plain DueDeliveries fallback
// dequeued (a store reached without its LeasedStore method): no claim ran,
// so this settle consumes the attempt, as attempts = attempts + 1 in SQL.
func (s *SQLStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	var nextAt any
	if !d.NextAttemptAt.IsZero() {
		nextAt = d.NextAttemptAt.UTC()
	}
	q := s.deliveryUpdate(d.unclaimedAttempt)
	args := []any{string(d.Status), d.LastError, nextAt, d.UpdatedAt.UTC(), d.ResponseStatus, d.ResponseBody, d.ID}
	if fence := settleFence(d.Status); len(fence) > 0 {
		// Fence (settleFence): a worker that overran its lease had the
		// row re-claimed under it, and its late write must not re-queue
		// the delivered row nor a triaged dead-letter, nor dead-letter
		// a delivery. The WHERE clause matches no row, Exec reports
		// nil: the stale write is a no-op, not a failure, mirroring
		// the fenced completions in battery/queue.
		marks := make([]string, len(fence))
		for i, st := range fence {
			marks[i] = s.placeholder(len(args) + 1)
			args = append(args, string(st))
		}
		q += ` AND status NOT IN (` + strings.Join(marks, ",") + `)`
	}
	if leaseFenced(d) {
		// Claim fence (claimFenced): a retry or dead settle lands only
		// while the row still holds the lease its claim wrote. Without
		// it a stale claimant's backoff replaced the live claim's lease,
		// and a stale give-up dead-lettered an operator's replay. The bound value
		// is the one the claim wrote (sqlite) or read back (postgres),
		// so the driver encodes both sides identically.
		q += ` AND next_attempt_at = ` + s.placeholder(len(args)+1)
		args = append(args, d.claimedUntil)
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
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

// ListDeadDeliveries implements [ReplayableStore]: terminally-failed
// (StatusDead) deliveries, newest-first.
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

// ResetDelivery implements [ReplayableStore]: returns a dead delivery to
// pending (attempts + error cleared, due now). The `AND status = dead` clause
// makes it idempotent, resetting a non-dead/unknown delivery matches no row
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
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s
		WHERE status = %s AND next_attempt_at <= %s
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

// ClaimDueDeliveries atomically reserves up to `limit` pending rows
// whose next_attempt_at <= now, pushing them to now+leasePeriod so
// concurrent workers skip them. A leasePeriod of 0 keeps the 30s
// default; a negative one is rejected — a negative lease can only be a
// sign or unit error, and folding it onto the default would hand the
// caller a longer lease than they asked for.
//
// Postgres uses FOR UPDATE SKIP LOCKED inside a CTE so the inner
// SELECT only sees uncontested rows; SQLite serializes writers via
// BEGIN IMMEDIATE so the SELECT-then-UPDATE sequence inside one tx
// is safely exclusive.
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
	// SQLite keeps a timestamp as text with its offset and compares the
	// text, so every bind is UTC (see utcDueTimes); the lease written here
	// is also the value the settle and release fences match.
	now = now.UTC()
	if s.dialect == "postgres" {
		return s.claimPostgres(ctx, now, limit, leasePeriod)
	}
	return s.claimSqlite(ctx, now, limit, leasePeriod)
}

// ReapTerminalBefore implements [RetentionSweeper]: it deletes successful
// deliveries whose updated_at is older than cutoff. Dead rows are
// deliberately retained — Replay needs the body and its last_error, so
// retention is not a dead-letter TTL (the same boundary framework/outbox
// documents for WithRetention).
func (s *SQLStore) ReapTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return reapTerminalSQL(ctx, s.db, fmt.Sprintf(
		`DELETE FROM %s WHERE status = %s AND updated_at < %s`,
		s.delTable, s.placeholder(1), s.placeholder(2)),
		string(StatusSuccess), cutoff)
}

// releaseClaim hands back a claim the worker never started (see
// claimReleaser): pending, due at now, and the attempt the claim consumed
// refunded in SQL. The next_attempt_at predicate is the claim fence (see
// claimFenced): once the lease expired and another worker re-claimed the
// row, the release matches nothing and refunds nothing. Every settle and
// replay moves next_attempt_at off the lease too, so the fence alone also
// keeps the release off a settled row.
func (s *SQLStore) releaseClaim(ctx context.Context, d Delivery, now time.Time) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET
		next_attempt_at = %s, updated_at = %s, attempts = attempts - 1
		WHERE id = %s AND next_attempt_at = %s`,
		s.delTable, s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4)),
		now.UTC(), now.UTC(), d.ID, d.claimedUntil)
	return err
}

// reapTerminalSQL runs the retention DELETE both SQL stores share: rows
// in the given terminal status whose updated_at is older than cutoff.
// The caller passes the fully built statement (the placeholder style is
// dialect-specific). It replaces the duplicated bodies of
// SQLStore.ReapTerminalBefore and SQLInboundStore.ReapTerminalBefore,
// which differed only in table and terminal status.
func reapTerminalSQL(ctx context.Context, db *sql.DB, stmt, status string, cutoff time.Time) (int64, error) {
	res, err := db.ExecContext(ctx, stmt, status, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *SQLStore) claimPostgres(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	// attempts = attempts + 1 at claim, matching the queue batteries
	// (DBQueue's claim UPDATE, RedisQueue's canonical-record bump): a
	// worker that dies between claim and settle has still consumed one
	// delivery, so a crash-looping worker trips MaxAttempts instead of
	// re-delivering forever with attempts pinned at 0. The RETURNING list
	// carries the post-bump value, so the claimed snapshot already counts
	// the attempt it is about to make, and the settle paths never need to
	// write the counter (an absolute settle write from a stale snapshot is
	// the lost-update that under-counted overlapping runners).
	q := fmt.Sprintf(`UPDATE %s SET next_attempt_at = $1, updated_at = $1, attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM %s
			WHERE status = $2 AND next_attempt_at <= $3
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
	// The lease as stored (microsecond precision), for the settle fence.
	for i := range claimed {
		claimed[i].claimedUntil = claimed[i].NextAttemptAt
	}
	return claimed, err
}

func (s *SQLStore) claimSqlite(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error) {
	// BEGIN IMMEDIATE takes the write lock before the SELECT, so two
	// concurrent workers serialize cleanly (busy_timeout applies to the
	// wait). database/sql's BeginTx issues a plain BEGIN, which SQLite reads
	// as DEFERRED whatever isolation level is asked for: both workers ran
	// the SELECT, and the loser's UPDATE could not upgrade its read snapshot
	// and failed with SQLITE_BUSY at once. The statement is issued on one
	// pinned connection, the battery/queue dequeueSQLite shape.
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
			// best-effort: the claim already failed with the error the caller gets; a failed rollback means a broken connection the pool discards on its own.
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()
	commit := func() error {
		_, err := conn.ExecContext(ctx, "COMMIT")
		committed = err == nil
		return err
	}

	selQ := fmt.Sprintf(`SELECT id, subscriber_id, event, payload, attempts, status,
		last_error, next_attempt_at, created_at, updated_at, owner, response_status, response_body FROM %s
		WHERE status = ? AND next_attempt_at <= ?
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
	// Push next_attempt_at forward for every claimed row and consume one
	// attempt per claim (see claimPostgres for the contract).
	placeholders := make([]string, len(claimed))
	args := []any{now.Add(leasePeriod)}
	for i, d := range claimed {
		placeholders[i] = "?"
		args = append(args, d.ID)
	}
	updQ := fmt.Sprintf("UPDATE %s SET next_attempt_at = ?, updated_at = ?, attempts = attempts + 1 WHERE id IN (%s)",
		s.delTable, strings.Join(placeholders, ","))
	// Insert the second timestamp arg after the first.
	args = append([]any{args[0], now.Add(leasePeriod)}, args[1:]...)
	if _, err := conn.ExecContext(ctx, updQ, args...); err != nil {
		return nil, err
	}
	// Reflect the new lease and the consumed attempt in the in-memory
	// copies we return, so the snapshot matches the row (the UPDATE bumps
	// each claimed row exactly once inside this tx).
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

// ----- scan + statements ----------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

// scanSubscriber reads one subscriber row. The secret is still sealed,
// and so is the auth column, returned beside it; openSubscriber
// decodes both.
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
	if events != "" {
		// A corrupt events column is an error, not "all events": the
		// zero-value sub.Events would silently make the subscriber
		// match nothing (or everything the caller defaults to), and the
		// parse failure marches on as zero-value data no longer.
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

// subUpsert aliases its target: a subscribers table named excluded
// otherwise collides with the EXCLUDED pseudo-row (Postgres refuses the
// reference as ambiguous; SQLite reads the target row and the update keeps
// the old values).
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
	// The settle never writes the attempts value: the claim consumes
	// attempts (see UpdateDelivery), and a settle writing its claim-time
	// snapshot absolutely is the lost-update that under-counted
	// overlapping runners. A fallback dequeue's attempt is counted in SQL,
	// relative to the row.
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
