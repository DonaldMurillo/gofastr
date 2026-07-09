package outbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db"
)

// Row is a snapshot of one outbox row. Status is one of "pending",
// "dispatched", or "dead". A row is "pending" while awaiting delivery
// (including the in-flight window, which is guarded by a lease rather
// than a separate status), "dispatched" after a successful Emit, and
// "dead" after exhausting MaxAttempts.
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

// Outbox stores event rows transactionally and relays them to an event
// bus. Construct with [New].
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
	}
	for _, opt := range opts {
		opt(o)
	}
	// Validate the table name once at construction (identifier
	// injection guard — table names can't be $1-parameterised).
	if _, err := query.SafeIdent(o.table); err != nil {
		return nil, fmt.Errorf("outbox: invalid table name %q: %w", o.table, err)
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

// qt returns the validated, double-quoted table name for interpolation.
func (o *Outbox) qt() string {
	return query.QuoteIdent(o.table)
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
	// Index serves the Relay's "claim oldest pending" ORDER BY and the
	// status filter together — the hot path runs every poll interval.
	idxName := o.table + "_status_created_idx"
	safeIdx, err := query.SafeIdent(idxName)
	if err != nil {
		return fmt.Errorf("outbox: invalid index name %q: %w", idxName, err)
	}
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (status, created_at)",
		query.QuoteIdent(safeIdx), o.qt(),
	)
	_, err = o.db.Exec(idx)
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

// Replay resets a dead row to pending so the Relay picks it up again —
// attempts cleared, scheduled immediately. The `AND status='dead'`
// clause makes it idempotent: replaying a pending, dispatched, or
// unknown row matches nothing and is a no-op (same contract as
// battery/queue's Replayable).
func (o *Outbox) Replay(ctx context.Context, id string) error {
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=0, last_error='', next_attempt_at=NULL, claimed_until=NULL
			WHERE id=$1 AND status='dead'`, o.qt()),
		id)
	return err
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
