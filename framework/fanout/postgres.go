package fanout

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	cfanout "github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/query"
	pq "github.com/lib/pq"
)

const (
	// defaultPGTable holds payloads too large for an inline NOTIFY.
	defaultPGTable = "gofastr_fanout_msgs"
	// pgInlineThreshold: NOTIFY payloads are capped at 8000 bytes by Postgres.
	// We inline when the marshaled wrapper is at most this size (margin for
	// framing/escapes); anything larger goes through the fallback table.
	pgInlineThreshold = 7000
	// pgPurgeAge: fallback rows are purged once they are older than this —
	// long enough that a briefly-disconnected receiver can still SELECT them,
	// short enough that the table stays bounded.
	pgPurgeAge = 2 * time.Minute
	// pgPurgeAgeSQL is the same cutoff expressed for the DB clock. Purging
	// with `created_at < now() - interval` keeps a single clock (the DB's),
	// avoiding skew between the app's time.Now() and the DB's now() (which
	// set created_at on insert). MUST stay in sync with pgPurgeAge.
	pgPurgeAgeSQL   = "2 minutes"
	pgPurgeInterval = 30 * time.Second
	pgReconnectMin  = 1 * time.Second
	pgReconnectMax  = 30 * time.Second
	// defaultListenTimeout is how long NewPostgres waits for the initial
	// LISTEN connection to become ready. pq.Listener retries a bad/unroutable
	// DSN forever; the deadline turns that into a prompt construction error.
	defaultListenTimeout = 15 * time.Second
)

// pgInlineMsg is the NOTIFY payload for an inline message: the topic travels
// alongside the payload so the dispatcher can route it without a channel per
// topic. Payload is a string (payloads are JSON envelopes — valid UTF-8).
type pgInlineMsg struct {
	Topic   string `json:"t"`
	Payload string `json:"p"`
}

// PostgresFanout is a lossy-best-effort [cfanout.Fanout] backed by Postgres
// LISTEN/NOTIFY. One connection LISTENs on a single channel; topic routing is
// inside the payload. Large payloads spill to a fallback table and are
// re-fetched by id on receive.
type PostgresFanout struct {
	dsn     string
	db      *sql.DB
	channel string
	table   string

	skipEnsure bool

	// listenTimeout bounds the initial LISTEN readiness wait (see
	// WithListenTimeout). A bad/unroutable DSN makes pq.Listener retry
	// forever; the deadline turns that into a prompt construction error.
	listenTimeout time.Duration
	// connectFailedLogged preserves onset-once logging of repeated
	// ListenerEventConnectionAttemptFailed bursts during a bad-DSN retry storm.
	connectFailedLogged atomic.Bool

	listener *pq.Listener

	mu      sync.RWMutex
	subs    map[string]map[uint64]*pgSub
	nextSub uint64

	purgeMu   sync.Mutex
	lastPurge time.Time

	closed atomic.Bool
	done   chan struct{}

	// Onset-once guards for receive-path drops (lossy lane): each category
	// logs the first occurrence at Warn so a recurring symptom is observable
	// without flooding on every message.
	dropMalformedPtr atomic.Bool
	dropMissingRow   atomic.Bool
	dropUndecodable  atomic.Bool
}

// Option configures a [PostgresFanout].
type Option func(*PostgresFanout)

// WithTableName overrides the fallback table name (default
// "gofastr_fanout_msgs"). The name must be a valid SQL identifier.
func WithTableName(name string) Option {
	return func(p *PostgresFanout) {
		if name != "" {
			p.table = name
		}
	}
}

// WithoutEnsureTable suppresses the CREATE TABLE IF NOT EXISTS that New
// otherwise runs at construction, mirroring the outbox's option of the same
// name. You must then create the fallback table via your own migration
// pipeline before publishing a large payload.
func WithoutEnsureTable() Option {
	return func(p *PostgresFanout) { p.skipEnsure = true }
}

// WithListenTimeout overrides the deadline NewPostgres waits for the initial
// LISTEN connection to become ready (default 15s). pq.Listener retries a
// bad or unroutable DSN forever; this deadline turns that into a prompt,
// descriptive construction error instead of an indefinite hang. A timeout
// closes the listener and aborts construction.
func WithListenTimeout(d time.Duration) Option {
	return func(p *PostgresFanout) {
		if d > 0 {
			p.listenTimeout = d
		}
	}
}

// NewPostgres creates a Postgres-backed fanout. dsn is used for the LISTEN
// connection (pq.NewListener); db is used for NOTIFY sends and the fallback
// table. Construction creates the fallback table unless [WithoutEnsureTable]
// is passed.
func NewPostgres(dsn string, db *sql.DB, opts ...Option) (*PostgresFanout, error) {
	if db == nil {
		return nil, fmt.Errorf("fanout: NewPostgres: nil db")
	}
	if dsn == "" {
		return nil, fmt.Errorf("fanout: NewPostgres: empty dsn")
	}
	p := &PostgresFanout{
		dsn:           dsn,
		db:            db,
		table:         defaultPGTable,
		listenTimeout: defaultListenTimeout,
		subs:          map[string]map[uint64]*pgSub{},
		done:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(p)
	}
	// Identifier injection guard (table names can't be $1-parameterised),
	// mirroring the outbox.
	if _, err := query.SafeIdent(p.table); err != nil {
		return nil, fmt.Errorf("fanout: invalid table name %q: %w", p.table, err)
	}
	// Postgres identifiers are capped at 63 bytes (NAMEDATALEN-1). CREATE
	// TABLE silently truncates a longer name while pg_notify errors on the
	// full-length channel — every Publish would fail at runtime. Fail at
	// construction instead.
	if len(p.table) > 63 {
		return nil, fmt.Errorf("fanout: table name %q is %d bytes; Postgres caps identifiers (and NOTIFY channels) at 63", p.table, len(p.table))
	}
	// Derive the NOTIFY/LISTEN channel from the (validated) table name so two
	// fanouts with different fallback tables on one database are fully
	// isolated: a hardcoded channel would resolve the other instance's
	// "t:<id>" pointers and re-deliver wrong/foreign payloads. The table
	// name is SafeIdent-validated, so it is a legal LISTEN channel name.
	p.channel = p.table
	if !p.skipEnsure {
		if err := p.ensureTable(); err != nil {
			return nil, fmt.Errorf("fanout: ensure table: %w", err)
		}
	}

	// The listener connects asynchronously; the event callback logs
	// disconnect/reconnect once on onset/recovery so a gap is observable
	// without flooding (mirrors the outbox's logClaimErr pattern).
	p.listener = pq.NewListener(dsn, pgReconnectMin, pgReconnectMax, p.onListenerEvent)
	// pq.Listener.Listen blocks until the connection is up and LISTEN has been
	// issued; on a bad/unroutable DSN it retries forever. Bound it so
	// construction fails fast instead of hanging.
	listenErr := make(chan error, 1)
	go func() { listenErr <- p.listener.Listen(p.channel) }()
	select {
	case err := <-listenErr:
		if err != nil {
			p.listener.Close()
			return nil, fmt.Errorf("fanout: listen %q: %w", p.channel, err)
		}
	case <-time.After(p.listenTimeout):
		p.listener.Close()
		return nil, fmt.Errorf("fanout: listen %q: timed out after %s waiting for the listener connection (is the DSN reachable and the database up?)",
			p.channel, p.listenTimeout)
	}
	go p.dispatch()
	return p, nil
}

// qt returns the validated, double-quoted fallback table name.
func (p *PostgresFanout) qt() string {
	return query.QuoteIdent(p.table)
}

func (p *PostgresFanout) ensureTable() error {
	_, err := p.db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id         BIGSERIAL PRIMARY KEY,
		payload    TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL
	)`, p.qt()))
	return err
}

// Publish broadcasts payload to all subscribers of topic. Inline when the
// wrapped message fits under the threshold; otherwise spills to the fallback
// table and NOTIFYs a short pointer. Best-effort.
//
// payload and topic must be valid UTF-8: NOTIFY payloads and the fallback
// table's TEXT column carry JSON envelopes, and Postgres would otherwise
// silently substitute U+FFFD for invalid bytes, corrupting the message.
func (p *PostgresFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	if p.closed.Load() {
		return fmt.Errorf("fanout: closed")
	}
	if !utf8.ValidString(topic) {
		return fmt.Errorf("fanout: publish %q: topic is not valid UTF-8 (framework topics are plain identifiers)", topic)
	}
	if !utf8.Valid(payload) {
		return fmt.Errorf("fanout: publish %q: payload is not valid UTF-8 (framework payloads are JSON envelopes)", topic)
	}
	wrapped, err := json.Marshal(pgInlineMsg{Topic: topic, Payload: string(payload)})
	if err != nil {
		return fmt.Errorf("fanout: marshal: %w", err)
	}
	if len(wrapped) <= pgInlineThreshold {
		return p.notify(ctx, string(wrapped))
	}
	return p.publishViaTable(ctx, string(wrapped))
}

// notify sends an inline NOTIFY payload.
func (p *PostgresFanout) notify(ctx context.Context, payload string) error {
	if _, err := p.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, p.channel, payload); err != nil {
		return fmt.Errorf("fanout: pg_notify: %w", err)
	}
	return nil
}

// publishViaTable stores the wrapped message in the fallback table and
// NOTIFYs a short "t:<id>" pointer. It also opportunistically purges rows
// older than the purge age so the table stays bounded.
func (p *PostgresFanout) publishViaTable(ctx context.Context, wrapped string) error {
	var id int64
	if err := p.db.QueryRowContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (payload, created_at) VALUES ($1, now()) RETURNING id`, p.qt()),
		wrapped).Scan(&id); err != nil {
		return fmt.Errorf("fanout: insert fallback row: %w", err)
	}
	// Purge opportunistically (throttled) — keeps the table bounded without a
	// dedicated admin goroutine.
	p.maybePurge(ctx)
	if _, err := p.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, p.channel,
		fmt.Sprintf("t:%d", id)); err != nil {
		return fmt.Errorf("fanout: pg_notify fallback pointer: %w", err)
	}
	return nil
}

// maybePurge runs a throttled DELETE of rows older than the purge age.
func (p *PostgresFanout) maybePurge(ctx context.Context) {
	p.purgeMu.Lock()
	if time.Since(p.lastPurge) < pgPurgeInterval {
		p.purgeMu.Unlock()
		return
	}
	p.lastPurge = time.Now()
	p.purgeMu.Unlock()

	if _, err := p.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE created_at < now() - $1::interval`, p.qt()),
		pgPurgeAgeSQL); err != nil {
		// Best-effort housekeeping: the next publish retries it. Debug-level
		// — a failing purge is a symptom of the same DB trouble the publish
		// itself will surface loudly.
		slog.Default().Debug("fanout: purge of expired fallback rows failed", "table", p.table, "err", err)
	}
}

// dispatch reads notifications from the listener and routes them to topic
// subscribers. Exits when Close drains the listener.
func (p *PostgresFanout) dispatch() {
	notify := p.listener.NotificationChannel()
	for {
		select {
		case n, ok := <-notify:
			if !ok {
				return
			}
			if n == nil || n.Extra == "" {
				continue
			}
			p.handleNotify(n.Extra)
		case <-p.done:
			return
		}
	}
}

// handleNotify decodes one NOTIFY payload (inline wrapper or "t:<id>" table
// pointer) and delivers the routed payload to subscribers.
func (p *PostgresFanout) handleNotify(extra string) {
	wrapped := extra
	if strings.HasPrefix(extra, "t:") {
		// Table fallback pointer: SELECT the row by id.
		idStr := strings.TrimPrefix(extra, "t:")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			p.logRecvDrop(&p.dropMalformedPtr, "dropped NOTIFY table-pointer with malformed id", err)
			return
		}
		var row string
		if err := p.db.QueryRow(fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, p.qt()), id).Scan(&row); err != nil {
			p.logRecvDrop(&p.dropMissingRow, "dropped NOTIFY table-pointer: fallback row already purged or gone", err)
			return
		}
		wrapped = row
	}
	var msg pgInlineMsg
	if err := json.Unmarshal([]byte(wrapped), &msg); err != nil {
		p.logRecvDrop(&p.dropUndecodable, "dropped undecodable NOTIFY payload (not a valid fanout wrapper)", err)
		return
	}
	p.deliver(msg.Topic, []byte(msg.Payload))
}

// logRecvDrop logs a receive-path drop at Warn, onset-once per category. A
// dropped message is a more important signal than Debug-level purge noise but
// must not flood on every notification, so each kind surfaces once until the
// flag is reset by a process restart.
func (p *PostgresFanout) logRecvDrop(flag *atomic.Bool, msg string, err error) {
	if !flag.CompareAndSwap(false, true) {
		return
	}
	args := []any{"channel", p.channel, "table", p.table}
	if err != nil {
		args = append(args, "err", err)
	}
	slog.Default().Warn("fanout: "+msg, args...)
}

// pgSub is one registered subscriber: send is the non-blocking drop-oldest
// enqueue into the subscriber's dedicated-goroutine queue (cfanout.SubscriberQueue),
// stop tears that queue down.
type pgSub struct {
	send func([]byte)
	stop func()
}

// deliver routes payload to every subscriber of topic via each subscriber's
// non-blocking queue. Delivery does NOT happen inline on the dispatch
// goroutine: each subscriber runs on its own dedicated goroutine with a
// bounded, drop-oldest queue, so one slow subscriber cannot stall the single
// LISTEN/NOTIFY dispatcher (which would freeze delivery to every other topic).
func (p *PostgresFanout) deliver(topic string, payload []byte) {
	p.mu.RLock()
	subs := p.subs[topic]
	cbs := make([]*pgSub, 0, len(subs))
	for _, s := range subs {
		cbs = append(cbs, s)
	}
	p.mu.RUnlock()
	for _, s := range cbs {
		s.send(payload)
	}
}

// Subscribe registers fn for topic. fn is wrapped in a per-subscriber bounded
// queue (cfanout.SubscriberQueue) so it runs on a dedicated goroutine with
// drop-oldest overflow — honoring the [cfanout.Fanout] contract that a slow
// subscriber is dropped, not backpressured, and never stalls other topics.
// The returned cancel unregisters fn and stops the goroutine; safe to call
// multiple times.
func (p *PostgresFanout) Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error) {
	if fn == nil {
		return func() {}, fmt.Errorf("fanout: nil subscriber")
	}
	// Publish rejects invalid-UTF-8 topics; accepting one here would mint a
	// subscriber that can never be matched. Reject symmetrically.
	if !utf8.ValidString(topic) {
		return func() {}, fmt.Errorf("fanout: subscribe %q: topic is not valid UTF-8 (framework topics are plain identifiers)", topic)
	}
	send, stop := cfanout.SubscriberQueue(fn, 0)
	p.mu.Lock()
	if p.closed.Load() {
		// Close already stopped every registered queue; registering after it
		// would leak an unstoppable goroutine on a dead fanout.
		p.mu.Unlock()
		stop()
		return func() {}, fmt.Errorf("fanout: closed")
	}
	id := p.nextSub
	p.nextSub++
	if p.subs[topic] == nil {
		p.subs[topic] = map[uint64]*pgSub{}
	}
	p.subs[topic][id] = &pgSub{send: send, stop: stop}
	p.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			delete(p.subs[topic], id)
			p.mu.Unlock()
			stop()
		})
	}, nil
}

// onListenerEvent logs disconnect/reconnect/connect-failure once on
// onset/recovery so a listener gap is observable without flooding the log.
// pq fires ListenerEventConnectionAttemptFailed on every retry of a bad DSN,
// so that path is onset-once-guarded (reset on any successful connect).
func (p *PostgresFanout) onListenerEvent(ev pq.ListenerEventType, err error) {
	switch ev {
	case pq.ListenerEventDisconnected:
		if err != nil {
			slog.Default().Warn("fanout: postgres listener disconnected; reconnecting (messages during the gap are lost)",
				"channel", p.channel, "err", err)
		}
	case pq.ListenerEventReconnected:
		p.connectFailedLogged.Store(false)
		slog.Default().Info("fanout: postgres listener reconnected", "channel", p.channel)
	case pq.ListenerEventConnectionAttemptFailed:
		if err != nil && p.connectFailedLogged.CompareAndSwap(false, true) {
			slog.Default().Warn("fanout: postgres listener connection attempt failed; retrying (initial messages may be dropped until it connects)",
				"channel", p.channel, "err", err)
		}
	}
}

// Close tears down the listener, the dispatch goroutine, and every
// subscriber queue — including those whose cancel func the caller dropped,
// which would otherwise park their goroutines forever. Subscribe refuses new
// registrations after Close (it checks closed under the same mu, so no
// subscriber can slip in between the stop sweep and the flag).
func (p *PostgresFanout) Close() error {
	p.mu.Lock()
	if !p.closed.CompareAndSwap(false, true) {
		p.mu.Unlock()
		return nil
	}
	for _, topicSubs := range p.subs {
		for _, s := range topicSubs {
			s.stop()
		}
	}
	p.subs = map[string]map[uint64]*pgSub{}
	p.mu.Unlock()
	close(p.done)
	p.listener.Close()
	return nil
}

// Compile-time interface check.
var _ cfanout.Fanout = (*PostgresFanout)(nil)
