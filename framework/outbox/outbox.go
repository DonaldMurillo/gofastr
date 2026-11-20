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
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
	"github.com/DonaldMurillo/gofastr/internal/settle"
)

// Row is a snapshot of one parent outbox row. Status is "pending" until every
// per-consumer [Delivery] has settled, then "dispatched".
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

// Outbox stores event rows in the caller's transaction and delivers each to
// its declared durable consumers, at least once. Construct it with [New],
// declare consumers with [Outbox.Consume], then run [Outbox.StartRelay].
type Outbox struct {
	db                 *sql.DB
	table              string
	dialect            dialect
	maxAttempts        int
	pollInterval       time.Duration
	batchSize          int
	skipEnsure         bool
	claimErrLogged     bool
	lease              time.Duration
	backoffBase        time.Duration
	backoffMax         time.Duration
	now                func() time.Time
	beforeReplayCommit func()
	nudge              chan struct{}
	deliveryTable      string
	consumers          map[string]map[string]event.EventHandler
	consumerMu         sync.RWMutex
	handlerGrace       time.Duration
	handlerTimeout     time.Duration
	settleTimeout      time.Duration
	retention          time.Duration
}

func leaseFloor(handlerTimeout, settleTimeout time.Duration) time.Duration {
	const maxLease = time.Duration(1<<63 - 1)
	budget := handlerTimeout + settleTimeout
	if budget < 0 || budget > maxLease/2 {
		return maxLease
	}
	return 2 * budget
}

// Option configures an Outbox.
type Option func(*Outbox)

// WithTable overrides the default "event_outbox" table name. [New] errors on a
// name with an unsafe identifier or a part longer than 63 bytes.
func WithTable(name string) Option {
	return func(o *Outbox) { o.table = name }
}

// WithMaxAttempts sets how many attempts a delivery gets before it is
// dead-lettered. Defaults to 10; n <= 0 keeps the default.
func WithMaxAttempts(n int) Option {
	return func(o *Outbox) {
		if n > 0 {
			o.maxAttempts = n
		}
	}
}

// WithoutEnsureTable skips the CREATE TABLE IF NOT EXISTS that New runs. The
// host then creates the tables (schema in events.md) before the first Append.
func WithoutEnsureTable() Option {
	return func(o *Outbox) { o.skipEnsure = true }
}

// WithPollInterval sets how often the relay polls when no Nudge arrives.
// Defaults to 1s; d <= 0 keeps the default.
func WithPollInterval(d time.Duration) Option {
	return func(o *Outbox) {
		if d > 0 {
			o.pollInterval = d
		}
	}
}

// WithBatchSize sets how many deliveries the relay claims per pump. Defaults
// to 100; n <= 0 keeps the default.
func WithBatchSize(n int) Option {
	return func(o *Outbox) {
		if n > 0 {
			o.batchSize = n
		}
	}
}

// WithHandlerGrace sets how long a delivery may go unhandled by every replica
// before it is abandoned, and how old a parent must be before it is completed
// or dropped. Set it above the rolling-deploy overlap plus clock skew between
// replicas. Defaults to 15m.
func WithHandlerGrace(d time.Duration) Option {
	//gofastr:allow(optzero) zero grace settles at once on purpose (single replica, and the outbox tests' knob); a negative acts as zero
	return func(o *Outbox) { o.handlerGrace = d }
}

// WithHandlerTimeout sets the budget for one handler invocation. At the
// deadline the handler's context is cancelled and the delivery settles as
// failed. Defaults to 30s; d <= 0 keeps the default. A long budget stretches
// the claim lease to cover it.
func WithHandlerTimeout(d time.Duration) Option {
	return func(o *Outbox) {
		if d > 0 {
			o.handlerTimeout = d
		}
	}
}

// WithRetention purges dispatched parents and their deliveries once older than
// d. Zero, the default, keeps every row. A parent holding a dead, abandoned or
// pending delivery is never purged.
func WithRetention(d time.Duration) Option {
	return func(o *Outbox) { o.retention = d }
}

// New constructs an Outbox on db, detects Postgres or SQLite, and creates its
// tables unless [WithoutEnsureTable] is given. It errors on an unsafe table
// name, and on Postgres on a table whose name differs only in case.
func New(db *sql.DB, opts ...Option) (*Outbox, error) {
	o := &Outbox{
		db:             db,
		table:          "event_outbox",
		maxAttempts:    10,
		pollInterval:   time.Second,
		batchSize:      100,
		lease:          5 * time.Minute,
		backoffBase:    time.Second,
		backoffMax:     time.Minute,
		now:            time.Now,
		nudge:          make(chan struct{}, 1),
		consumers:      map[string]map[string]event.EventHandler{},
		handlerGrace:   15 * time.Minute,
		handlerTimeout: 30 * time.Second,
		settleTimeout:  settle.Timeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	o.lease = max(o.lease, leaseFloor(o.handlerTimeout, o.settleTimeout))
	if _, err := query.SafeIdent(o.table); err != nil {
		return nil, fmt.Errorf("outbox: invalid table name %q: %w", o.table, err)
	}
	for part := range strings.SplitSeq(o.table, ".") {
		if len(part) > query.MaxIdentBytes {
			return nil, fmt.Errorf("outbox: table name part %q exceeds %d bytes", part, query.MaxIdentBytes)
		}
	}
	o.deliveryTable = deliveryTableName(o.table)
	o.dialect = detectDialect(db)
	if o.dialect == dialectPostgres {
		if err := query.RefuseCaseKeptTables(context.Background(), db, o.table, o.deliveryTable); err != nil {
			return nil, fmt.Errorf("outbox: %w", err)
		}
	}
	if !o.skipEnsure {
		if err := o.ensureTable(); err != nil {
			return nil, fmt.Errorf("outbox: ensure table: %w", err)
		}
	}
	o.registerData()
	return o, nil
}

func deliveryTableName(table string) string {
	i := strings.LastIndexByte(table, '.') + 1
	base := table[i:]
	delivery := query.FitIdent(base + "_delivery")
	if asciicase.EqualFold(delivery, base) {
		delivery = delivery[:len(delivery)-1]
	}
	return table[:i] + delivery
}

type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

func detectDialect(db *sql.DB) dialect {
	if query.IsPostgres(db) {
		return dialectPostgres
	}
	return dialectSQLite
}

func (o *Outbox) qt() string {
	return query.QuoteFolded(o.table)
}

func (o *Outbox) qd() string {
	return query.QuoteFolded(o.deliveryTable)
}

func (o *Outbox) ensureTable() error {
	tsType := "DATETIME"
	if o.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id              TEXT PRIMARY KEY NOT NULL,
		type            TEXT NOT NULL,
		payload         TEXT,
		owner           TEXT NOT NULL DEFAULT '',
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
	if err := o.addOwnerColumn(); err != nil {
		return err
	}
	idxName := o.table + "_status_created_idx"
	safeIdx, err := query.SafeIdent(idxName)
	if err != nil {
		return fmt.Errorf("outbox: invalid index name %q: %w", idxName, err)
	}
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s (status, created_at)",
		query.IndexOn(o.dialect == dialectSQLite, query.QuoteIndexName(safeIdx), o.qt()),
	)
	if _, err := o.db.Exec(idx); err != nil {
		return err
	}
	return o.ensureDeliveryTable(tsType)
}

func (o *Outbox) addOwnerColumn() error {
	stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN owner TEXT NOT NULL DEFAULT ''`, o.qt())
	if o.dialect == dialectPostgres {
		stmt = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`, o.qt())
	}
	_, err := o.db.Exec(stmt)
	if err != nil && o.dialect != dialectPostgres && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

func (o *Outbox) registerData() {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("event_outbox", o.table), Source: "outbox", Table: o.table, PrimaryKey: "id",
		Columns: []string{"id", "type", "payload", "owner", "status", "attempts", "last_error",
			"created_at", "dispatched_at", "next_attempt_at", "claimed_until"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("event_outbox", o.table), Source: "outbox", Table: o.table, Column: "owner",
		Mode: datexport.EraseAnonymize, ScrubColumns: []string{"payload"}, Tombstone: "null",
	})
}

func (o *Outbox) ensureDeliveryTable(tsType string) error {
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
	claimIdx := o.deliveryTable + "_claim_idx"
	safeClaim, err := query.SafeIdent(claimIdx)
	if err != nil {
		return fmt.Errorf("outbox: invalid index name %q: %w", claimIdx, err)
	}
	_, err = o.db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s (status, next_attempt_at)",
		query.IndexOn(o.dialect == dialectSQLite, query.QuoteIndexName(safeClaim), o.qd()),
	))
	return err
}

// Append inserts a pending row through ex, so it commits or rolls back with
// the caller's transaction. data is stored as JSON. The returned row ID
// becomes [event.Event].ID on delivery. Use AppendFor for a payload that holds
// one user's data.
func (o *Outbox) Append(ctx context.Context, ex db.Executor, eventType string, data any) (string, error) {
	return o.AppendFor(ctx, ex, "", eventType, data)
}

// AppendFor is Append for a payload owned by the user owner, whose erasure
// writes JSON null over it. An empty owner is Append.
func (o *Outbox) AppendFor(ctx context.Context, ex db.Executor, owner, eventType string, data any) (string, error) {
	id := newID()
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("outbox: marshal payload: %w", err)
	}
	body := string(payload)
	_, err = ex.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(id, type, payload, owner, status, attempts, created_at)
			VALUES ($1, $2, $3, $4, 'pending', 0, $5)`, o.qt()),
		id, eventType, body, owner, o.now().UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("outbox: append: %w", err)
	}
	return id, nil
}

// List returns up to limit rows newest first, only those in status when it is
// not empty. limit <= 0 means 100.
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

func scanOutboxRow(row interface {
	Scan(dest ...any) error
}) (Row, error) {
	var r Row
	var payload sql.NullString
	var lastError sql.NullString
	var createdAt, dispatchedAt any
	if err := row.Scan(&r.ID, &r.Type, &payload, &r.Status, &r.Attempts,
		&lastError, &createdAt, &dispatchedAt); err != nil {
		return Row{}, err
	}
	var err error
	r.CreatedAt, err = query.ParseDBTime(createdAt)
	if err != nil {
		return Row{}, fmt.Errorf("outbox: decode row %q created_at: %w", r.ID, err)
	}
	if payload.Valid {
		r.Payload = []byte(payload.String)
	}
	if lastError.Valid {
		r.LastError = lastError.String
	}
	r.DispatchedAt, err = outboxTimePtr(dispatchedAt)
	if err != nil {
		return Row{}, fmt.Errorf("outbox: decode row %q dispatched_at: %w", r.ID, err)
	}
	return r, nil
}

func truncateError(err error) string {
	const max = 2000
	s := err.Error()
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut]
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("outbox: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
