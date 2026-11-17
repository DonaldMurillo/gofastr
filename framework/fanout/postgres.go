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
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	pq "github.com/lib/pq"
)

const (
	defaultPGTable       = "gofastr_fanout_msgs"
	pgInlineThreshold    = 7000
	pgPurgeAge           = 2 * time.Minute
	pgPurgeAgeSQL        = "2 minutes"
	pgPurgeInterval      = 30 * time.Second
	pgReconnectMin       = 1 * time.Second
	pgReconnectMax       = 30 * time.Second
	defaultListenTimeout = 15 * time.Second
)

type pgInlineMsg struct {
	Topic   string `json:"t"`
	Payload string `json:"p"`
}

// PostgresFanout is a lossy [cfanout.Fanout] on Postgres LISTEN/NOTIFY: one
// channel named after its fallback table, the topic inside the payload, and
// payloads too large to NOTIFY spilled to that table.
type PostgresFanout struct {
	dsn     string
	db      *sql.DB
	channel string
	table   string

	skipEnsure bool

	listenTimeout       time.Duration
	connectFailedLogged atomic.Bool

	listener *pq.Listener

	mu      sync.RWMutex
	subs    map[string]map[uint64]*pgSub
	nextSub uint64

	purgeMu    sync.Mutex
	lastPurge  time.Time
	purgeEvery time.Duration

	closed atomic.Bool
	done   chan struct{}

	fetchCtx   context.Context
	stopFetch  context.CancelFunc
	dispatched chan struct{}

	dropMalformedPtr atomic.Bool
	dropMissingRow   atomic.Bool
	dropUndecodable  atomic.Bool
}

// Option configures a [PostgresFanout].
type Option func(*PostgresFanout)

// WithTableName sets the fallback table, and so the channel; default
// "gofastr_fanout_msgs".
func WithTableName(name string) Option {
	return func(p *PostgresFanout) {
		if name != "" {
			p.table = name
		}
	}
}

// WithoutEnsureTable skips creating the fallback table; the host creates it
// before a large publish.
func WithoutEnsureTable() Option {
	return func(p *PostgresFanout) { p.skipEnsure = true }
}

// WithListenTimeout sets how long NewPostgres waits for the LISTEN connection
// (default 15s) before it errors.
func WithListenTimeout(d time.Duration) Option {
	return func(p *PostgresFanout) {
		if d > 0 {
			p.listenTimeout = d
		}
	}
}

// NewPostgres listens on dsn and sends through db. It errors on a nil db, an
// empty dsn, an unsafe or over-63-byte table name, a case-kept table, or a
// listener not up within the listen timeout.
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
		purgeEvery:    pgPurgeInterval,
	}
	for _, opt := range opts {
		opt(p)
	}
	if _, err := query.SafeIdent(p.table); err != nil {
		return nil, fmt.Errorf("fanout: invalid table name %q: %w", p.table, err)
	}
	if len(p.table) > 63 {
		return nil, fmt.Errorf("fanout: table name %q is %d bytes; Postgres caps identifiers (and NOTIFY channels) at 63", p.table, len(p.table))
	}
	p.channel = p.table
	if err := query.RefuseCaseKeptTables(context.Background(), db, p.table); err != nil {
		return nil, fmt.Errorf("fanout: %w", err)
	}
	if !p.skipEnsure {
		if err := p.ensureTable(); err != nil {
			return nil, fmt.Errorf("fanout: ensure table: %w", err)
		}
	}

	p.listener = pq.NewListener(dsn, pgReconnectMin, pgReconnectMax, p.onListenerEvent)
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
	p.fetchCtx, p.stopFetch = context.WithCancel(context.Background())
	p.dispatched = make(chan struct{})
	go p.dispatch()
	return p, nil
}

func (p *PostgresFanout) qt() string {
	return query.QuoteFolded(p.table)
}

func (p *PostgresFanout) ensureTable() error {
	//gofastr:allow(GOFASTR1415) a spill row is an in-flight bus message with no user attribution, deleted pgPurgeAge after it is written by the purge every replica's dispatcher runs on a ticker
	_, err := p.db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id         BIGSERIAL PRIMARY KEY,
		payload    TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL
	)`, p.qt()))
	return err
}

// Publish sends payload to topic's subscribers on every replica, best-effort.
// It errors on a closed fanout or a topic or payload that is not valid UTF-8.
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

func (p *PostgresFanout) notify(ctx context.Context, payload string) error {
	if _, err := p.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, p.channel, payload); err != nil {
		return fmt.Errorf("fanout: pg_notify: %w", err)
	}
	return nil
}

func (p *PostgresFanout) publishViaTable(ctx context.Context, wrapped string) error {
	var id int64
	if err := p.db.QueryRowContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (payload, created_at) VALUES ($1, now()) RETURNING id`, p.qt()),
		wrapped).Scan(&id); err != nil {
		return fmt.Errorf("fanout: insert fallback row: %w", err)
	}
	p.maybePurge(ctx)
	if _, err := p.db.ExecContext(ctx, `SELECT pg_notify($1, $2)`, p.channel,
		fmt.Sprintf("t:%d", id)); err != nil {
		return fmt.Errorf("fanout: pg_notify fallback pointer: %w", err)
	}
	return nil
}

func (p *PostgresFanout) maybePurge(ctx context.Context) {
	p.purgeMu.Lock()
	if time.Since(p.lastPurge) < p.purgeEvery {
		p.purgeMu.Unlock()
		return
	}
	p.lastPurge = time.Now()
	p.purgeMu.Unlock()

	if _, err := p.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE created_at < now() - $1::interval`, p.qt()),
		pgPurgeAgeSQL); err != nil {
		slog.Default().Debug("fanout: purge of expired fallback rows failed", "table", p.table, "err", err)
	}
}

func (p *PostgresFanout) dispatch() {
	defer close(p.dispatched)
	notify := p.listener.NotificationChannel()
	purge := time.NewTicker(p.purgeEvery)
	defer purge.Stop()
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
		case <-purge.C:
			p.maybePurge(p.fetchCtx)
		case <-p.done:
			return
		}
	}
}

func (p *PostgresFanout) handleNotify(extra string) {
	wrapped := extra
	if after, ok := strings.CutPrefix(extra, "t:"); ok {
		idStr := after
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			p.logRecvDrop(&p.dropMalformedPtr, "dropped NOTIFY table-pointer with malformed id", err)
			return
		}
		var row string
		if err := p.db.QueryRowContext(p.fetchCtx, fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, p.qt()), id).Scan(&row); err != nil {
			if p.fetchCtx.Err() != nil {
				return
			}
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

type pgSub struct {
	send func([]byte)
	stop func()
}

func (p *PostgresFanout) deliver(topic string, payload []byte) {
	p.mu.RLock()
	subs := p.subs[topic]
	cbs := make([]*pgSub, 0, len(subs))
	for _, s := range subs {
		cbs = append(cbs, s)
	}
	p.mu.RUnlock()
	for _, s := range cbs {
		s.deliverOne(topic, payload)
	}
}

func (s *pgSub) deliverOne(topic string, payload []byte) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("fanout: subscriber panicked; delivery to it dropped", "topic", topic, "panic", textsafe.Recovered(rec))
		}
	}()
	s.send(payload)
}

// Subscribe runs fn for topic's payloads on its own goroutine with a
// drop-oldest queue. It errors on a nil fn, a non-UTF-8 topic or a closed
// fanout; cancel is idempotent.
func (p *PostgresFanout) Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error) {
	if fn == nil {
		return func() {}, fmt.Errorf("fanout: nil subscriber")
	}
	if !utf8.ValidString(topic) {
		return func() {}, fmt.Errorf("fanout: subscribe %q: topic is not valid UTF-8 (framework topics are plain identifiers)", topic)
	}
	send, stop := cfanout.SubscriberQueue(fn, 0)
	p.mu.Lock()
	if p.closed.Load() {
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

// Close stops the listener, the dispatcher and every subscriber queue; later
// Subscribe calls fail.
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
	p.stopFetch()
	p.listener.Close()
	<-p.dispatched
	return nil
}

var _ cfanout.Fanout = (*PostgresFanout)(nil)
