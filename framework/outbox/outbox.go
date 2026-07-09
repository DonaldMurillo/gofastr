package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Row is a snapshot of one parent outbox row. The parent status is now
// just "pending" → "dispatched": a row is "pending" until the relay has
// settled every per-consumer delivery (dispatched/dead/abandoned), then it
// flips to "dispatched". The parent's attempts/last_error columns are
// vestigial — per-attempt state lives in event_outbox_delivery (see
// [Delivery]).
type Row struct {
	ID           string
	Type         string
	Payload      []byte // JSON of event.Event.Data
	Status       string
	Attempts     int
	LastError    string
	CreatedAt    time.Time
	DispatchedAt *time.Time
}

// Outbox stores event rows transactionally and delivers each to a set of
// declared durable consumers, independently, with at-least-once semantics.
// Construct with [New], register consumers with [Outbox.Consume], then
// launch the relay with [Outbox.StartRelay].
type Outbox struct {
	db      *sql.DB
	table   string
	dialect dialect

	maxAttempts  int
	pollInterval time.Duration
	batchSize    int

	// skipEnsure suppresses the boot-time CREATE TABLE (WithoutEnsureTable).
	skipEnsure bool

	// claimErrLogged tracks whether the current claim-failure state has
	// already been logged, so relayLoop logs an outage once on onset (and
	// once on recovery) rather than every poll. Touched only by the single
	// relay goroutine.
	claimErrLogged bool

	// Lease held on a claimed row. Mirrors battery/queue's lease: a
	// Relay that dies mid-batch releases its rows after the lease
	// expires so another Relay (or a restart) reclaims them.
	lease time.Duration

	// Exponential backoff for failed Emits: base*2^(attempts-1), capped
	// at backoffMax. Applied via the next_attempt_at column.
	backoffBase time.Duration
	backoffMax  time.Duration

	// now is the clock used for created_at, claim timestamps, lease
	// cutoffs, and backoff math. Defaults to time.Now; tests override
	// it to assert lease-reclaim without wall-clock sleeps.
	now func() time.Time

	// nudge wakes the Relay immediately after a commit so delivery
	// latency is not bound to PollInterval. Buffered cap 1: extra
	// nudges coalesce — one wake per pump is enough.
	nudge chan struct{}

	// deliveryTable is the validated name of the child table holding
	// per-consumer delivery state (event_outbox_delivery by default).
	// Created alongside the parent table in ensureTable.
	deliveryTable string

	// consumers is the declared durable-consumer registry, keyed by
	// event type → consumer name → handler. Populated by Consume before
	// StartRelay; the relay is the sole reader. A pending parent row gets
	// one delivery row per declared consumer whose event type matches.
	consumers  map[string]map[string]event.EventHandler
	consumerMu sync.RWMutex

	// handlerGrace bounds how long a delivery may stay unhandled — no
	// consumer handler on ANY replica — before it is abandoned (settled
	// terminal). This is the time-based replacement for snapshot-driven
	// retirement: a genuinely-removed consumer's deliveries age past the
	// grace and abandon everywhere, while a newly-added consumer's fresh
	// deliveries are younger than the grace, so a lagging replica that lacks
	// the handler never abandons them — the up-to-date replica delivers them
	// first. MUST exceed the rolling-deploy overlap window. Default 15m.
	handlerGrace time.Duration

	// retention, when > 0, enables automatic purge of fully-settled
	// (dispatched) parent rows and their deliveries older than this from the
	// relay loop. 0 (default) keeps every row forever.
	retention time.Duration
}

// Option configures an Outbox.
type Option func(*Outbox)

// WithTable overrides the default "event_outbox" table name. The name is
// validated as a safe SQL identifier at construction; an invalid name
// makes [New] return an error.
func WithTable(name string) Option {
	return func(o *Outbox) { o.table = name }
}

// WithMaxAttempts sets how many Emit attempts a row gets before it is
// marked "dead". Defaults to 10.
func WithMaxAttempts(n int) Option {
	return func(o *Outbox) { o.maxAttempts = n }
}

// WithoutEnsureTable suppresses the CREATE TABLE IF NOT EXISTS that New
// otherwise runs at construction. Use it in deployments whose policy forbids
// unattended DDL (typically alongside framework.WithoutAutoMigrate): you must
// then create the outbox table via your own migration pipeline before the app
// stages any event, or the first Append fails. The table schema is documented
// in framework/docs/content/events.md.
func WithoutEnsureTable() Option {
	return func(o *Outbox) { o.skipEnsure = true }
}

// WithPollInterval sets how often the Relay polls for pending rows when
// no Nudge arrives. Defaults to 1s.
func WithPollInterval(d time.Duration) Option {
	return func(o *Outbox) { o.pollInterval = d }
}

// WithBatchSize sets the maximum number of rows the Relay claims per
// pump. Defaults to 100.
func WithBatchSize(n int) Option {
	return func(o *Outbox) { o.batchSize = n }
}

// WithHandlerGrace sets the grace window that governs two symmetric,
// time-based decisions: how long a delivery may stay unhandled (no consumer
// handler on any replica) before it is abandoned, and how long a parent stays
// pending before it may be completed/dropped. Both wait the grace so a
// consumer being rolled out on another replica has time to expand and deliver
// its rows before anything is settled.
//
// It therefore also bounds parent-completion latency: a fully-delivered parent
// is not marked dispatched until it is older than the grace (delivery to
// consumers is unaffected and prompt — only the parent bookkeeping/GC lags).
//
// The grace MUST comfortably exceed your rolling-deploy overlap window PLUS
// worst-case clock skew between replicas (delivery/parent timestamps are
// written by one replica and compared on another). Keep a floor of a few
// minutes even for fast deploys. Defaults to 15m.
func WithHandlerGrace(d time.Duration) Option {
	return func(o *Outbox) { o.handlerGrace = d }
}

// WithRetention enables automatic purge of fully-settled (dispatched) parent
// rows and their deliveries once they are older than d. The relay runs the
// purge as part of its poll cycle. Zero (the default) disables purging —
// rows are kept forever. Only dispatched parents are ever purged; pending or
// dead/abandoned deliveries are never deleted out from under a consumer.
func WithRetention(d time.Duration) Option {
	return func(o *Outbox) { o.retention = d }
}

// New constructs an Outbox backed by db. It detects the dialect
// (postgres or sqlite — mirroring battery/queue) and ensures the table
// and its (status, created_at) index exist.
func New(db *sql.DB, opts ...Option) (*Outbox, error) {
	o := &Outbox{
		db:           db,
		table:        "event_outbox",
		maxAttempts:  10,
		pollInterval: time.Second,
		batchSize:    100,
		lease:        5 * time.Minute,
		backoffBase:  time.Second,
		backoffMax:   time.Minute,
		now:          time.Now,
		nudge:        make(chan struct{}, 1),
		consumers:    map[string]map[string]event.EventHandler{},
		handlerGrace: 15 * time.Minute,
	}
	for _, opt := range opts {
		opt(o)
	}
	// Validate the table name once at construction (identifier
	// injection guard — table names can't be $1-parameterised).
	if _, err := query.SafeIdent(o.table); err != nil {
		return nil, fmt.Errorf("outbox: invalid table name %q: %w", o.table, err)
	}
	// The delivery table name derives from the parent table; validate it
	// the same way (identifier injection guard).
	o.deliveryTable = o.table + "_delivery"
	if _, err := query.SafeIdent(o.deliveryTable); err != nil {
		return nil, fmt.Errorf("outbox: invalid delivery table name %q: %w", o.deliveryTable, err)
	}
	o.dialect = detectDialect(db)
	if !o.skipEnsure {
		if err := o.ensureTable(); err != nil {
			return nil, fmt.Errorf("outbox: ensure table: %w", err)
		}
	}
	return o, nil
}

type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

// detectDialect probes the driver: Postgres answers SELECT version();
// anything else (SQLite errors on version()) falls back to the SQLite
// code path. Mirrors battery/queue's detectDBDialect.
func detectDialect(db *sql.DB) dialect {
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		if strings.Contains(strings.ToLower(v), "postgresql") {
			return dialectPostgres
		}
	}
	return dialectSQLite
}

// qt returns the validated, double-quoted parent table name for interpolation.
func (o *Outbox) qt() string {
	return query.QuoteIdent(o.table)
}

// qd returns the validated, double-quoted delivery table name.
func (o *Outbox) qd() string {
	return query.QuoteIdent(o.deliveryTable)
}

func (o *Outbox) ensureTable() error {
	tsType := "DATETIME"
	if o.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id              TEXT PRIMARY KEY,
		type            TEXT NOT NULL,
		payload         TEXT,
		status          TEXT NOT NULL DEFAULT 'pending',
		attempts        INTEGER NOT NULL DEFAULT 0,
		last_error      TEXT,
		created_at      %s NOT NULL,
		dispatched_at   %s,
		next_attempt_at %s,
		claimed_until   %s
	)`, o.qt(), tsType, tsType, tsType, tsType)
	if _, err := o.db.Exec(stmt); err != nil {
		return err
	}
	// Index serves the parent completion scan: mark dispatched only
	// among pending rows, ordered by age.
	idxName := o.table + "_status_created_idx"
	safeIdx, err := query.SafeIdent(idxName)
	if err != nil {
		return fmt.Errorf("outbox: invalid index name %q: %w", idxName, err)
	}
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (status, created_at)",
		query.QuoteIdent(safeIdx), o.qt(),
	)
	if _, err := o.db.Exec(idx); err != nil {
		return err
	}
	return o.ensureDeliveryTable(tsType)
}

// ensureDeliveryTable creates the per-consumer delivery child table and
// its serving indexes. The parent's per-attempt columns (attempts,
// last_error, next_attempt_at, claimed_until) are left in place —
// vestigial now that state moved to the child, but dropping columns is
// destructive. DDL mirrors the parent's dialect switch.
func (o *Outbox) ensureDeliveryTable(tsType string) error {
	// created_at is when the DELIVERY row was inserted (not when the event was
	// staged — that's the parent's created_at). The abandonment grace is
	// measured from this so a freshly-expanded delivery for a newly-declared
	// consumer is never abandoned by a lagging replica.
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		row_id          TEXT NOT NULL,
		consumer        TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'pending',
		attempts        INTEGER NOT NULL DEFAULT 0,
		last_error      TEXT,
		created_at      %s NOT NULL,
		next_attempt_at %s,
		claimed_until   %s,
		dispatched_at   %s,
		PRIMARY KEY (row_id, consumer)
	)`, o.qd(), tsType, tsType, tsType, tsType)
	if _, err := o.db.Exec(stmt); err != nil {
		return err
	}
	// Serving index for the claim query: status + next_attempt_at filter.
	// The PK's leading row_id column already serves every row_id lookup
	// (completeParent's NOT EXISTS, ListDeliveries), so no separate row_id
	// index is created.
	claimIdx := o.deliveryTable + "_claim_idx"
	safeClaim, err := query.SafeIdent(claimIdx)
	if err != nil {
		return fmt.Errorf("outbox: invalid index name %q: %w", claimIdx, err)
	}
	_, err = o.db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (status, next_attempt_at)",
		query.QuoteIdent(safeClaim), o.qd(),
	))
	return err
}

// Append inserts a pending row using ex — callers hand in their *sql.Tx
// (it satisfies [db.Executor]) so the row commits or rolls back with the
// business write. data is JSON-marshalled into Payload. Returns the new
// row's ID, which becomes [event.Event].ID on delivery for consumer
// dedup.
func (o *Outbox) Append(ctx context.Context, ex db.Executor, eventType string, data any) (string, error) {
	id := newID()
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("outbox: marshal payload: %w", err)
	}
	// string() so both drivers store the JSON as TEXT, not BLOB.
	body := string(payload)
	if body == "" {
		body = "null"
	}
	_, err = ex.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(id, type, payload, status, attempts, created_at)
			VALUES ($1, $2, $3, 'pending', 0, $4)`, o.qt()),
		id, eventType, body, o.now().UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("outbox: append: %w", err)
	}
	return id, nil
}

// List returns up to limit rows, newest-first. An empty status returns
// rows regardless of state; otherwise only rows in that status.
// limit <= 0 defaults to 100.
func (o *Outbox) List(ctx context.Context, status string, limit int) ([]Row, error) {
	if limit <= 0 {
		limit = 100
	}
	base := fmt.Sprintf(`SELECT id, type, payload, status, attempts, last_error, created_at, dispatched_at FROM %s`, o.qt())
	var args []any
	if status != "" {
		base += " WHERE status = $1"
		args = append(args, status)
	}
	base += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	rows, err := o.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		r, err := scanOutboxRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanOutboxRow materialises a Row from a SELECT in the canonical column
// order used by List.
func scanOutboxRow(row interface {
	Scan(dest ...any) error
}) (Row, error) {
	var r Row
	var payload sql.NullString
	var lastError sql.NullString
	var dispatchedAt sql.NullTime
	if err := row.Scan(&r.ID, &r.Type, &payload, &r.Status, &r.Attempts,
		&lastError, &r.CreatedAt, &dispatchedAt); err != nil {
		return Row{}, err
	}
	if payload.Valid {
		r.Payload = []byte(payload.String)
	}
	if lastError.Valid {
		r.LastError = lastError.String
	}
	if dispatchedAt.Valid {
		t := dispatchedAt.Time
		r.DispatchedAt = &t
	}
	return r, nil
}

// backoffFor returns the delay before the next retry for the attempt
// that just failed (attempts >= 1): base*2^(attempts-1), capped at
// backoffMax when positive. Mirrors battery/queue's backoffFor so the
// two behave consistently.
func (o *Outbox) backoffFor(attempts int) time.Duration {
	exp := attempts - 1
	if exp < 0 {
		exp = 0
	}
	d := o.backoffBase
	for i := 0; i < exp; i++ {
		if o.backoffMax > 0 && d >= o.backoffMax {
			return o.backoffMax
		}
		if d > (1<<62)/2 {
			if o.backoffMax > 0 {
				return o.backoffMax
			}
			return d
		}
		d *= 2
	}
	if o.backoffMax > 0 && d > o.backoffMax {
		d = o.backoffMax
	}
	return d
}

// truncateError caps last_error so a pathological handler error can't
// bloat the row.
func truncateError(err error) string {
	const max = 2000
	s := err.Error()
	if len(s) > max {
		return s[:max]
	}
	return s
}

// newID returns a fresh 32-char hex identifier. Panics on entropy
// failure — an OS-level fault the Relay must not paper over by minting
// colliding all-zero IDs. Mirrors battery/webhook's newID without
// importing battery code (framework packages may not import batteries).
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("outbox: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
