package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
)

// MaxPayloadBytes caps webhook payload size. Larger payloads are
// rejected at Publish time to prevent unbounded queue/memory growth.
const MaxPayloadBytes = 1 << 20 // 1 MiB

// validateEventName rejects event names that would smuggle CR/LF/NUL
// or other control characters into outbound headers / persisted rows.
func validateEventName(name string) error {
	if name == "" {
		return errors.New("webhook: event name required")
	}
	for _, r := range name {
		if r == '\r' || r == '\n' || r == 0 {
			return fmt.Errorf("webhook: event name contains forbidden control character")
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("webhook: event name contains forbidden control character")
		}
	}
	return nil
}

// Subscriber is a registered HTTP endpoint that wants to receive
// signed POSTs for the listed events.
//
// Events accept simple glob patterns: `*` matches a single segment,
// `**` matches one-or-more segments. An entry of `*` alone (or `**`
// alone) matches every event.
type Subscriber struct {
	ID      string
	URL     string
	Secret  string
	Events  []string
	Active  bool
	Paused  bool // explicit opt-out; honored by Subscribe so callers can register paused
	Created time.Time
}

// DeliveryStatus is the lifecycle state of a single delivery attempt.
type DeliveryStatus string

const (
	StatusPending DeliveryStatus = "pending"
	StatusSuccess DeliveryStatus = "success"
	StatusFailed  DeliveryStatus = "failed"
	StatusDead    DeliveryStatus = "dead"
)

// Delivery is one attempt-set against a Subscriber for a single event.
type Delivery struct {
	ID            string
	SubscriberID  string
	Event         string
	Payload       []byte
	Attempts      int
	Status        DeliveryStatus
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Store is the persistence interface.
type Store interface {
	AddSubscriber(ctx context.Context, s Subscriber) error
	GetSubscriber(ctx context.Context, id string) (*Subscriber, error)
	ListSubscribers(ctx context.Context) ([]Subscriber, error)
	DeleteSubscriber(ctx context.Context, id string) error

	AddDelivery(ctx context.Context, d Delivery) error
	UpdateDelivery(ctx context.Context, d Delivery) error
	ListDeliveries(ctx context.Context, subscriberID string, limit int) ([]Delivery, error)
	DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error)
}

// LeasedStore is the optional Store upgrade for multi-instance
// deployments. Implementations atomically claim up to `limit` pending
// rows that are due at `now` and push their next_attempt_at to
// `now + leasePeriod`, so concurrent workers see those rows as not
// yet due and skip them.
//
// If the worker crashes mid-attempt, the lease expires and another
// worker re-claims. Pick a leasePeriod larger than your worst-case
// handler latency.
//
// The Manager probes for this interface at tick time; stores that
// don't implement it fall back to the plain DueDeliveries +
// best-effort behaviour, which is fine for single-instance setups.
type LeasedStore interface {
	Store
	ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error)
}

// ReplayableStore is the optional Store capability for dead-letter inspection
// and replay. Both shipped stores (SQLStore, MemoryStore) implement it;
// Manager.DeadDeliveries / Manager.Replay probe for it so admin tooling can
// surface a replay action only when the backend supports it.
type ReplayableStore interface {
	Store
	// ListDeadDeliveries returns up to limit terminally-failed (StatusDead)
	// deliveries, newest-first.
	ListDeadDeliveries(ctx context.Context, limit int) ([]Delivery, error)
	// ResetDelivery returns a dead delivery to pending so the worker retries
	// it (attempts + last error cleared, due immediately). Idempotent: it MUST
	// only touch StatusDead rows, so resetting a non-dead/unknown delivery is a
	// no-op — never resurrecting an in-flight or already-delivered one.
	ResetDelivery(ctx context.Context, id string) error
}

// DeadDeliveries lists terminally-failed deliveries when the store supports it.
func (m *Manager) DeadDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	rs, ok := m.store.(ReplayableStore)
	if !ok {
		return nil, fmt.Errorf("webhook: store %T does not support dead-letter listing", m.store)
	}
	return rs.ListDeadDeliveries(ctx, limit)
}

// Replay re-queues a dead delivery when the store supports it. The worker
// (Start) picks it up on the next tick.
func (m *Manager) Replay(ctx context.Context, id string) error {
	rs, ok := m.store.(ReplayableStore)
	if !ok {
		return fmt.Errorf("webhook: store %T does not support replay", m.store)
	}
	return rs.ResetDelivery(ctx, id)
}

// DefaultMaxResponseBodyBytes is the per-attempt response-body cap when
// Options.MaxResponseBodyBytes is unset. A malicious receiver returning
// gigabytes of body would otherwise exhaust manager memory.
const DefaultMaxResponseBodyBytes int64 = 64 << 10 // 64 KiB

// Options configures the Manager.
//
// MaxAttempts caps how many times a delivery will be retried before it
// becomes "dead". Default 6 (≈3h with the default backoff).
//
// Backoff is the wait between attempts. Position [i-1] selects the
// wait after attempt i. The last value is reused for any further
// attempts. Default: 30s, 1m, 5m, 15m, 1h, 3h.
//
// HTTPClient is the client used for outbound POSTs. Default has a
// 10s per-request timeout, no redirect following, and a dial-time SSRF
// guard (net.Dialer.Control re-checks the resolved IP at connect time
// to defeat DNS rebinding) unless AllowPrivateNetworks is set. A
// caller-supplied client is used as-is — it gets no dial-time guard.
//
// PollInterval controls how often the worker looks for due deliveries.
// Default 1 second.
//
// MaxResponseBodyBytes caps bytes read from each subscriber response.
// Default 64 KiB. Set < 0 to disable the cap (not recommended).
//
// AllowPrivateNetworks opts-out of the SSRF guard that rejects
// subscriber URLs targeting RFC1918, loopback, link-local, or cloud
// metadata endpoints. Default false — required production posture.
// Tests and explicit dev wiring may set true.
//
// SignatureTolerance bounds how far receivers may consider the
// signed timestamp drifting from "now" when verifying. The sender
// embeds the timestamp into the signature; this field is purely
// documentation here — receivers pass it to VerifyTimestamped.
//
// LeasePeriod controls how long a claimed delivery is hidden from
// other workers when the store implements LeasedStore. Must be
// longer than the worst-case handler latency; default 30s.
type Options struct {
	MaxAttempts          int
	Backoff              []time.Duration
	HTTPClient           *http.Client
	PollInterval         time.Duration
	MaxResponseBodyBytes int64
	AllowPrivateNetworks bool
	SignatureTolerance   time.Duration
	LeasePeriod          time.Duration

	// Logger receives operational warnings the worker can't surface to a
	// caller — chiefly a delivery-state UPDATE failing after an attempt. A DB
	// blip there leaves the row with its pre-attempt status, so the next tick
	// re-delivers (at-least-once) with zero signal that the state write was
	// lost. Default: log.Printf. Mirrors InboundConfig.Logger on IngestHandler;
	// set to a no-op func to silence.
	Logger func(format string, args ...any)
}

// Manager owns the worker goroutine and is the entry point for Publish
// and subscriber management.
type Manager struct {
	store Store
	opts  Options

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// runCtx is the parent context for in-flight HTTP attempts; runCancel
	// is invoked by Stop so the receiver's slow response cannot keep the
	// worker alive past App.Shutdown.
	runCtx    context.Context
	runCancel context.CancelFunc

	// nowFn lets tests inject a clock; defaults to time.Now.
	nowFn func() time.Time

	// deliveriesTotal counts every delivery attempt processed by attempt
	// (one per due delivery dequeued by tick). failuresTotal counts every
	// attempt that did not succeed (transport error, request-build error,
	// non-2xx response, or subscriber gone/inactive). Both are exposed via
	// MetricsCollector so operators can alert on delivery volume and failure
	// rate from the unified /metrics surface; failuresTotal is always <=
	// deliveriesTotal.
	deliveriesTotal atomic.Uint64
	failuresTotal   atomic.Uint64
}

// New constructs a Manager with defaults applied.
func New(s Store, opts Options) *Manager {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 6
	}
	if len(opts.Backoff) == 0 {
		opts.Backoff = []time.Duration{
			30 * time.Second,
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
			1 * time.Hour,
			3 * time.Hour,
		}
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 1 * time.Second
	}
	if opts.MaxResponseBodyBytes == 0 {
		opts.MaxResponseBodyBytes = DefaultMaxResponseBodyBytes
	}
	if opts.LeasePeriod <= 0 {
		opts.LeasePeriod = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = log.Printf
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: ssrfGuardedTransport(opts.AllowPrivateNetworks),
		}
	} else if !opts.AllowPrivateNetworks {
		// A caller-supplied client (proxy, tracing, custom timeout) must
		// not silently drop the SSRF guard — that reopens loopback /
		// RFC1918 / 169.254.169.254 via DNS rebinding for the most
		// common customizations. Wrap with a per-request target check
		// that leaves the caller's transport (proxy, tunnel, custom
		// dialer) untouched; only an explicit AllowPrivateNetworks
		// opts out.
		opts.HTTPClient = ssrfGuardClient(opts.HTTPClient)
	}
	return &Manager{
		store:  s,
		opts:   opts,
		stopCh: make(chan struct{}),
		nowFn:  time.Now,
	}
}

// logf writes an operational warning through the configured Logger. It
// exists so the delivery-attempt path never silently drops a store-update
// error: a lost state write re-delivers on the next tick (at-least-once),
// so the only correct response is to make the loss observable. The Manager
// is only built by New (which defaults Logger), so the fallback is defence
// in depth.
func (m *Manager) logf(format string, args ...any) {
	if m.opts.Logger != nil {
		m.opts.Logger(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Subscribe registers (or replaces) a Subscriber. The ID is assigned
// when empty. The URL must point at a public endpoint unless
// Options.AllowPrivateNetworks is set; otherwise the call returns an
// SSRF error.
//
// Active defaults to true for the zero-value struct (the common case
// "register and start delivering"). To register a paused subscriber,
// set Paused=true and Active will be forced false.
func (m *Manager) Subscribe(ctx context.Context, s Subscriber) (Subscriber, error) {
	if s.Secret == "" {
		return Subscriber{}, errors.New("webhook: subscriber Secret required")
	}
	if err := validateSubscriberURL(s.URL, m.opts.AllowPrivateNetworks); err != nil {
		return Subscriber{}, err
	}
	for _, p := range s.Events {
		if err := validateEventName(p); err != nil {
			return Subscriber{}, fmt.Errorf("webhook: event pattern: %w", err)
		}
	}
	if s.ID == "" {
		s.ID = newID()
	}
	if s.Created.IsZero() {
		s.Created = m.nowFn()
	}
	if len(s.Events) == 0 {
		s.Events = []string{"*"}
	}
	// Paused wins over Active. Otherwise: zero-value Active means
	// "I didn't say either way" → default to active. Callers who
	// want a paused subscriber set Paused=true explicitly.
	if s.Paused {
		s.Active = false
	} else if !s.Active {
		s.Active = true
	}
	if err := m.store.AddSubscriber(ctx, s); err != nil {
		return Subscriber{}, err
	}
	return s, nil
}

// Unsubscribe removes a Subscriber. Missing IDs are not an error.
func (m *Manager) Unsubscribe(ctx context.Context, id string) error {
	return m.store.DeleteSubscriber(ctx, id)
}

// Subscribers returns every registered Subscriber.
func (m *Manager) Subscribers(ctx context.Context) ([]Subscriber, error) {
	return m.store.ListSubscribers(ctx)
}

// Publish fans the event out to every active matching subscriber.
func (m *Manager) Publish(ctx context.Context, event string, payload []byte) (int, error) {
	if err := validateEventName(event); err != nil {
		return 0, err
	}
	if len(payload) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook: payload size %d exceeds max %d bytes", len(payload), MaxPayloadBytes)
	}
	if len(payload) > 0 && !json.Valid(payload) {
		return 0, errors.New("webhook: payload is not valid JSON")
	}
	subs, err := m.store.ListSubscribers(ctx)
	if err != nil {
		return 0, err
	}
	now := m.nowFn()
	queued := 0
	for _, s := range subs {
		if !s.Active {
			continue
		}
		if !matchesAny(event, s.Events) {
			continue
		}
		d := Delivery{
			ID:            newID(),
			SubscriberID:  s.ID,
			Event:         event,
			Payload:       cloneBytes(payload),
			Attempts:      0,
			Status:        StatusPending,
			NextAttemptAt: now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := m.store.AddDelivery(ctx, d); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

// Start launches the worker goroutine. Safe to call once per Manager;
// subsequent calls are no-ops.
func (m *Manager) Start() {
	m.startOnce.Do(func() {
		m.runCtx, m.runCancel = context.WithCancel(context.Background())
		m.wg.Add(1)
		go m.runWorker()
	})
}

// Stop signals the worker to exit, cancels any in-flight HTTP attempt
// via the worker's derived context, then waits for the worker to
// return. If ctx fires first, returns ctx.Err — but the cancellation
// of in-flight HTTP requests has already been signaled. Safe to call
// more than once.
func (m *Manager) Stop(ctx context.Context) error {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		if m.runCancel != nil {
			m.runCancel()
		}
	})
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) runWorker() {
	defer m.wg.Done()
	t := time.NewTicker(m.opts.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.tick(m.runCtx)
		}
	}
}

// tick processes any deliveries that are due now. Exposed for tests.
//
// When the store implements LeasedStore the worker uses the
// claim-and-lease path so concurrent Manager instances against the
// same SQL store don't double-deliver. Stores without that interface
// fall back to plain DueDeliveries — fine for single-instance setups.
func (m *Manager) tick(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := m.nowFn()
	var due []Delivery
	var err error
	if ls, ok := m.store.(LeasedStore); ok {
		due, err = ls.ClaimDueDeliveries(ctx, now, 32, m.opts.LeasePeriod)
	} else {
		due, err = m.store.DueDeliveries(ctx, now, 32)
	}
	if err != nil {
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		m.attempt(ctx, d)
	}
}

func (m *Manager) attempt(ctx context.Context, d Delivery) {
	// One delivery attempt is being processed: count it now. Every
	// non-success branch below bumps failuresTotal exactly once, so
	// failuresTotal stays <= deliveriesTotal. Success bumps nothing else.
	m.deliveriesTotal.Add(1)
	sub, err := m.store.GetSubscriber(ctx, d.SubscriberID)
	if err != nil || sub == nil || !sub.Active {
		m.failuresTotal.Add(1)
		d.Status = StatusDead
		d.LastError = "subscriber gone or inactive"
		d.UpdatedAt = m.nowFn()
		// Use background ctx so a canceled worker still records the
		// terminal state; the cancel signal is the WHY, not a reason
		// to lose the write.
		m.saveDelivery(d)
		return
	}
	d.Attempts++
	d.UpdatedAt = m.nowFn()
	body := bytes.NewReader(d.Payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, body)
	if err != nil {
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = err.Error()
		m.schedule(&d)
		m.saveDelivery(d)
		return
	}
	ts := m.nowFn().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoFastr-Webhook/1")
	req.Header.Set("X-GoFastr-Event", d.Event)
	req.Header.Set("X-GoFastr-Delivery-ID", d.ID)
	req.Header.Set("X-GoFastr-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set(SignatureHeader, SignWithTimestamp(sub.Secret, ts, d.Payload))

	resp, err := m.opts.HTTPClient.Do(req)
	if err != nil {
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = err.Error()
		m.schedule(&d)
		m.saveDelivery(d)
		return
	}
	cap := m.opts.MaxResponseBodyBytes
	if cap > 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, cap))
	} else if cap < 0 {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.Status = StatusSuccess
		d.LastError = ""
		d.NextAttemptAt = time.Time{}
		m.saveDelivery(d)
		return
	}
	d.Status = StatusFailed
	m.failuresTotal.Add(1)
	d.LastError = fmt.Sprintf("http %d", resp.StatusCode)
	m.schedule(&d)
	m.saveDelivery(d)
}

// saveDelivery persists a delivery's post-attempt state, logging (never
// swallowing) a store error. A lost state write leaves the row at its
// pre-attempt status, so the next tick re-delivers (at-least-once) with no
// signal that the prior attempt ever happened — the only correct response is
// to make the loss observable via the configured Logger. The attempt's HTTP
// side-effect already occurred, so there is nothing else to do here.
//
// A background context is used deliberately: a worker cancellation is the
// reason the attempt is winding down, not a reason to lose the terminal
// state write.
func (m *Manager) saveDelivery(d Delivery) {
	if err := m.store.UpdateDelivery(context.Background(), d); err != nil {
		m.logf("webhook: delivery %s: persist %s state failed (row may be stale → duplicate delivery): %v", d.ID, d.Status, err)
	}
}

// schedule sets NextAttemptAt or marks the delivery dead when the
// attempt budget is exhausted.
func (m *Manager) schedule(d *Delivery) {
	if d.Attempts >= m.opts.MaxAttempts {
		d.Status = StatusDead
		d.NextAttemptAt = time.Time{}
		return
	}
	wait := backoff.At(m.opts.Backoff, d.Attempts)
	d.NextAttemptAt = m.nowFn().Add(wait)
	d.Status = StatusPending
}

// DeliveriesTotal returns the number of delivery attempts that have been
// processed by the worker (one per due delivery dequeued by tick).
func (m *Manager) DeliveriesTotal() uint64 {
	return m.deliveriesTotal.Load()
}

// FailuresTotal returns the number of delivery attempts that did not
// succeed (transport error, request-build error, non-2xx response, or
// subscriber gone/inactive). It is always <= DeliveriesTotal.
func (m *Manager) FailuresTotal() uint64 {
	return m.failuresTotal.Load()
}

// MetricsCollector returns a Prometheus text-exposition collector for the
// manager's delivery counters. Register it on the app's *middleware.Metrics
// (via RegisterCollector) so the totals appear on the single /metrics surface.
func (m *Manager) MetricsCollector() func(w io.Writer) {
	return func(w io.Writer) {
		fmt.Fprintf(w, "# HELP webhook_deliveries_total Total webhook delivery attempts.\n")
		fmt.Fprintf(w, "# TYPE webhook_deliveries_total counter\n")
		fmt.Fprintf(w, "webhook_deliveries_total %d\n", m.deliveriesTotal.Load())
		fmt.Fprintf(w, "# HELP webhook_failures_total Webhook delivery attempts that did not succeed (transport error, non-2xx, dead-lettered).\n")
		fmt.Fprintf(w, "# TYPE webhook_failures_total counter\n")
		fmt.Fprintf(w, "webhook_failures_total %d\n", m.failuresTotal.Load())
	}
}

// ----- helpers --------------------------------------------------------------

// matchesAny returns true when event matches any of the supplied glob
// patterns.
func matchesAny(event string, patterns []string) bool {
	for _, p := range patterns {
		if matchOne(event, p) {
			return true
		}
	}
	return false
}

// matchOne supports two wildcards:
//
//   - `*`  matches exactly one segment (delimited by `.`)
//   - `**` matches one or more segments (greedy)
//
// `*` alone or `**` alone matches every event.
func matchOne(event, pattern string) bool {
	if pattern == "*" || pattern == "**" || pattern == event {
		return true
	}
	return matchSegments(strings.Split(event, "."), strings.Split(pattern, "."))
}

func matchSegments(event, pattern []string) bool {
	// Recursive matcher with `**` as a multi-segment wildcard. Iterative
	// loop with backtracking would also work; recursion is clear at this
	// size and only fires on segmented patterns.
	if len(pattern) == 0 {
		return len(event) == 0
	}
	if pattern[0] == "**" {
		// `**` must consume at least one segment, then can consume more.
		if len(event) == 0 {
			return false
		}
		// Try consuming 1..N event segments.
		for i := 1; i <= len(event); i++ {
			if matchSegments(event[i:], pattern[1:]) {
				return true
			}
		}
		return false
	}
	if len(event) == 0 {
		return false
	}
	if pattern[0] != "*" && pattern[0] != event[0] {
		return false
	}
	return matchSegments(event[1:], pattern[1:])
}

func cloneBytes(b []byte) []byte {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// newID returns a fresh 32-character hex identifier. Panics on entropy
// failure — that's an operating-system-level fault and the manager
// must not silently mint colliding all-zero IDs.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("webhook: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
