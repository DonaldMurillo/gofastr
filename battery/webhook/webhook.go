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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/tracing"
)

// MaxPayloadBytes caps webhook payload size. Larger payloads are
// rejected at Publish time to prevent unbounded queue/memory growth.
const MaxPayloadBytes = 1 << 20 // 1 MiB

// validateEventName rejects event names that would smuggle control
// characters into outbound headers / persisted rows: the full
// core/textsafe unsafe set — C0, DEL, the C1 block (U+0080–U+009F, whose
// 8-bit CSI/OSC forms drive terminal escapes exactly like ESC-[, and
// which Go's header writer happily carries as obs-text), and the
// zero-width/bidi invisible set.
func validateEventName(name string) error {
	if name == "" {
		return errors.New("webhook: event name required")
	}
	for _, r := range name {
		if textsafe.IsUnsafe(r) {
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
	// Auth is the credential each delivery sends to the receiver
	// (bearer, API key, basic); the zero value sends none. Sealed at
	// rest with Secret's codec. See Auth. Subscribe replaces the whole
	// subscriber, so re-subscribing an ID with a zero Auth clears the
	// stored credential: read the subscriber first to change one field.
	// A Store of your own must persist Auth, or deliveries go out
	// without it.
	Auth Auth
}

// DeliveryStatus is the lifecycle state of a single delivery attempt.
type DeliveryStatus string

const (
	StatusPending DeliveryStatus = "pending"
	StatusSuccess DeliveryStatus = "success"
	StatusFailed  DeliveryStatus = "failed"
	StatusDead    DeliveryStatus = "dead"
)

// isTerminalStatus reports whether s is a terminal delivery state. A
// settle write carrying a NON-terminal status must never overwrite a
// terminal row: a worker that overran its lease had the row re-claimed
// under it (the designed crash recovery), and its late failure settle
// must not re-queue the recovery claimant's delivered row (duplicate
// POST) nor a dead-letter someone triaged. The store-side sibling of
// battery/queue's fenced completions (claim_token / ownsClaim).
func isTerminalStatus(s DeliveryStatus) bool {
	return s == StatusSuccess || s == StatusDead
}

// settleFence lists the row statuses a settle carrying next must not
// overwrite. Beyond the isTerminalStatus fence, nothing but a success
// replaces a success: a claimant past the attempt budget dead-letters
// without sending while the claimant it outran may still deliver, and a
// dead row over that delivery invites a replay that POSTs it again.
func settleFence(next DeliveryStatus) []DeliveryStatus {
	switch {
	case !isTerminalStatus(next):
		return []DeliveryStatus{StatusSuccess, StatusDead}
	case next != StatusSuccess:
		return []DeliveryStatus{StatusSuccess}
	}
	return nil
}

// claimFenced reports whether a settle of d belongs to a claim the row has
// moved past: the row's next_attempt_at no longer holds the lease d's claim
// wrote. A re-claim needs next_attempt_at <= now and writes now+lease, so
// it always moves the value on, and so does an operator's ResetDelivery.
// Retry and dead settles are fenced. A stale retry replaced the live
// claimant's lease with a shorter backoff, so a third worker claimed the
// delivery while the second still sent it. A stale dead settle landed on
// a delivery an operator had replayed since, and dead-lettered it before
// it ran once. A success settle is never lease-fenced: a stale success
// really delivered, and on a replayed row it ends a replay whose payload
// the receiver already has. A delivery no shipped store claimed (the plain
// DueDeliveries path) carries no lease and is not fenced.
func claimFenced(d Delivery, rowNextAttemptAt time.Time) bool {
	return leaseFenced(d) && !rowNextAttemptAt.Equal(d.claimedUntil)
}

// leaseFenced reports whether a settle of d must match its claim's lease
// (see claimFenced); SQLStore turns it into a next_attempt_at predicate.
func leaseFenced(d Delivery) bool {
	return !d.claimedUntil.IsZero() && d.Status != StatusSuccess
}

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
	// Owner is the user id PublishFor queued the delivery for, or empty.
	// App.EraseUserData deletes an erased user's deliveries from a
	// SQLStore, sent or not.
	Owner string

	// ResponseStatus is the HTTP status of the last attempt that got a
	// response, or 0 when none did (a dial error, a timeout).
	ResponseStatus int
	// ResponseBody is the first Options.ResponseBodyLimit bytes of that
	// response's body, as text: invalid UTF-8 replaced, and control
	// bytes percent-encoded so the stored text cannot drive a terminal
	// or forge a log line when an operator prints it.
	ResponseBody string

	// claimedUntil is the lease the claim that produced this value wrote
	// (zero when no shipped store claimed it). The shipped stores fence a
	// retry settle on it; see claimFenced.
	claimedUntil time.Time

	// unclaimedAttempt marks a delivery tick's plain DueDeliveries
	// fallback dequeued: no claim consumed its attempt, so the shipped
	// stores' settle adds it to the row (attempts = attempts + 1).
	unclaimedAttempt bool
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

// RetentionSweeper is the optional Store/InboundStore capability the
// Manager's retention pass (Options.Retention) probes for: implementations
// delete their own terminal, fully-settled rows older than cutoff and
// report how many went. Terminal means unambiguous success only —
// StatusSuccess deliveries and processed envelopes. Dead deliveries and
// failed envelopes are deliberately out of scope: Replay and post-mortems
// need the body, so retention is not a dead-letter TTL (the same boundary
// framework/outbox documents for WithRetention).
type RetentionSweeper interface {
	ReapTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error)
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
	// no-op, never resurrecting an in-flight or already-delivered one.
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
// caller-supplied client is used as-is, it gets no dial-time guard.
//
// PollInterval controls how often the worker looks for due deliveries.
// Default 1 second.
//
// MaxResponseBodyBytes caps bytes read from each subscriber response.
// Default 64 KiB. Set < 0 to disable the cap (not recommended).
//
// AllowPrivateNetworks opts-out of the SSRF guard that rejects
// subscriber URLs targeting RFC1918, loopback, link-local, or cloud
// metadata endpoints. Default false, required production posture.
// Tests and explicit dev wiring may set true.
//
// SignatureTolerance bounds how far receivers may consider the
// signed timestamp drifting from "now" when verifying. The sender
// embeds the timestamp into the signature; this field is purely
// documentation here, receivers pass it to VerifyTimestamped.
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

	// AllowPlaintextAuth lets a subscriber with Auth use an http:// URL.
	// By default Subscribe refuses that pairing, and a delivery that
	// meets it fails without sending: the bearer token, API key or
	// Basic password would cross the network in clear text. Set it for
	// a receiver on loopback or a trusted LAN segment, or in tests.
	AllowPlaintextAuth bool
	SignatureTolerance time.Duration
	LeasePeriod        time.Duration

	// ResponseBodyLimit is how many bytes of each receiver response are
	// kept on the delivery (Delivery.ResponseBody) for diagnosing a
	// refusal. Zero means 1024. A negative value keeps none. It never
	// exceeds MaxResponseBodyBytes when that is positive. The body is
	// stored with the delivery, so a receiver that echoes the payload
	// or a secret back puts it in the store: size this to what an
	// error message needs.
	ResponseBodyLimit int64

	// Retention, when positive, turns on the manager's terminal-row
	// sweep: every tick reaps successful deliveries — and processed
	// inbound envelopes, when InboundStore below is also set — whose
	// updated_at is older than the window. Zero (the default) keeps
	// every row forever; this is the outbox-style opt-in, not the queue
	// battery's default-on. Dead deliveries and failed envelopes are
	// never reaped (Replay/forensics need the body): retention is not a
	// dead-letter TTL. A negative value is clamped to zero (off) at
	// construction.
	Retention time.Duration

	// InboundStore, when set, is swept by the same retention pass:
	// processed envelopes older than Options.Retention are reaped from
	// it on every tick. The Manager reads it for nothing else; wire it
	// only together with Retention.
	InboundStore InboundStore

	// Logger receives operational warnings the worker can't surface to a
	// caller, chiefly a delivery-state UPDATE failing after an attempt. A DB
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
	// credClient is opts.HTTPClient with redirects refused, for a
	// delivery that carries Subscriber.Auth. Built by New.
	credClient *http.Client

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
	if opts.Retention < 0 {
		// A negative window can only be a sign or unit error; folding
		// it on would arm a FUTURE cutoff and silently retain
		// everything while looking configured. Clamp to off.
		opts.Retention = 0
	}
	if opts.Logger == nil {
		opts.Logger = log.Printf
	}
	if opts.ResponseBodyLimit == 0 {
		opts.ResponseBodyLimit = defaultResponseBodyLimit
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: tracing.Transport(netguard.GuardedTransport(opts.AllowPrivateNetworks, "webhook")),
		}
	} else if !opts.AllowPrivateNetworks {
		// A caller-supplied client (proxy, tracing, custom timeout) must
		// not silently drop the SSRF guard, that reopens loopback /
		// RFC1918 / 169.254.169.254 via DNS rebinding for the most
		// common customizations. Wrap with a per-request target check
		// that leaves the caller's transport (proxy, tunnel, custom
		// dialer) untouched; only an explicit AllowPrivateNetworks
		// opts out.
		opts.HTTPClient = ssrfGuardClient(opts.HTTPClient)
	}
	// A delivery that carries a credential goes through a copy of the
	// client that never follows a redirect: Go drops Authorization on a
	// cross-host redirect but not X-API-Key or any custom header, and a
	// caller-supplied client may follow redirects.
	noRedirect := *opts.HTTPClient
	noRedirect.CheckRedirect = refuseRedirect
	return &Manager{
		credClient: &noRedirect,
		store:      s,
		opts:       opts,
		stopCh:     make(chan struct{}),
		nowFn:      time.Now,
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
	if err := s.Auth.validate(); err != nil {
		return Subscriber{}, err
	}
	if m.plaintextAuth(s) {
		return Subscriber{}, errPlaintextAuth
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

// Publish fans the event out to every active matching subscriber. The
// deliveries name no user; use PublishFor when the payload carries one
// user's data.
func (m *Manager) Publish(ctx context.Context, event string, payload []byte) (int, error) {
	return m.PublishFor(ctx, "", event, payload)
}

// PublishFor is Publish for a payload that belongs to one user: each
// delivery records owner, so erasing that user deletes the deliveries
// with their copy of the data.
func (m *Manager) PublishFor(ctx context.Context, owner, event string, payload []byte) (int, error) {
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
			Owner:         owner,
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
// return. If ctx fires first, returns ctx.Err, but the cancellation
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
// fall back to plain DueDeliveries, fine for single-instance setups.
func (m *Manager) tick(ctx context.Context) {
	// The store is host-supplied code on the worker's ticker loop,
	// which has no per-request recover net — a panicking store call
	// must degrade to an attributed log line (claimed rows stay
	// pending and retry next tick; at-least-once), not kill the
	// worker goroutine and the process with it.
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: store panic in tick (due deliveries stay pending): %v", v)
		}
	}()
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
		// The claim path consumes one attempt per claim (the stores bump
		// the row); this plain-dequeue fallback has no claim, so the
		// attempt is consumed here instead — the same one-bump-per-
		// dequeue rule, keeping MaxAttempts reachable for non-leased
		// stores. The settle adds it to the row relative to the stored
		// count (see SQLStore.UpdateDelivery); it never writes the value.
		for i := range due {
			due[i].Attempts++
			due[i].unclaimedAttempt = true
		}
	}
	if err != nil {
		return
	}
	for i, d := range due {
		if ctx.Err() != nil {
			// Shutdown mid-batch: the rest were claimed, their attempt
			// spent, and never sent. Hand them back refunded, as a lease
			// too short to start them does; with MaxAttempts 1 the next
			// claim dead-lettered them unsent.
			if r, ok := m.store.(claimReleaser); ok {
				m.releaseClaims(ctx, r, due[i:])
			}
			return
		}
		// The whole batch shares one lease, and the attempts run one
		// after another. Starting a delivery whose lease runs out mid-POST
		// lets another worker re-claim and send it too, or dead-letter it
		// unsent once the re-claim passes the attempt budget. Such claims
		// go back to pending with their attempt refunded, and the next
		// tick claims them under a fresh lease. The bar is at most half
		// the lease, so a fresh claim always starts.
		if m.leaseTooShort(d) {
			if r, ok := m.store.(claimReleaser); ok {
				m.releaseClaims(ctx, r, due[i:])
				break
			}
		}
		m.attempt(ctx, d)
	}
	m.sweepTerminal(ctx)
}

// claimReleaser is the shipped stores' hand-back of a claim the worker
// never started: the row returns to pending, due now, with the attempt the
// claim consumed refunded, fenced on the claim's own lease so a re-claimed
// row is left alone.
type claimReleaser interface {
	releaseClaim(ctx context.Context, d Delivery, now time.Time) error
}

// leaseTooShort reports whether d's claim has less lease left than one
// attempt may take: the HTTP client's timeout, capped at half the lease
// (also the bar for a client with no timeout). A claim with no lease
// recorded is never too short.
func (m *Manager) leaseTooShort(d Delivery) bool {
	if d.claimedUntil.IsZero() {
		// No lease known: a host store that claims rows itself never set
		// one, and refusing every such claim would deliver nothing.
		return false
	}
	need := m.opts.LeasePeriod / 2
	if t := m.opts.HTTPClient.Timeout; t > 0 {
		need = min(t, need)
	}
	return m.nowFn().Add(need).After(d.claimedUntil)
}

// releaseClaims hands back the claims a tick did not start (see tick).
func (m *Manager) releaseClaims(ctx context.Context, r claimReleaser, ds []Delivery) {
	// Store code on the worker loop, the saveDelivery recover net: a
	// panic leaves the rest leased until the lease expires.
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: release of unstarted claims panicked (they are re-claimed when the lease expires): %v", v)
		}
	}()
	now := m.nowFn()
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	for _, d := range ds {
		if err := r.releaseClaim(settleCtx, d, now); err != nil {
			m.logf("webhook: delivery %s: release unstarted claim failed (it is re-claimed when the lease expires): %v", d.ID, err)
		}
	}
}

// sweepTerminal reaps terminal, fully-settled rows older than the
// retention window when one is configured. It probes the optional
// [RetentionSweeper] capability on both the delivery store and the
// configured inbound store, so stores without reaping (a custom Store
// from a host) simply keep every row, exactly as before the option
// existed. Only unambiguous terminal-success rows are ever deleted; see
// RetentionSweeper for why dead rows stay.
func (m *Manager) sweepTerminal(ctx context.Context) {
	if m.opts.Retention <= 0 {
		return
	}
	cutoff := m.nowFn().Add(-m.opts.Retention)
	if s, ok := m.store.(RetentionSweeper); ok {
		if _, err := s.ReapTerminalBefore(ctx, cutoff); err != nil {
			m.logf("webhook: retention sweep of terminal deliveries failed (rows retained): %v", err)
		}
	}
	if s, ok := m.opts.InboundStore.(RetentionSweeper); ok {
		if _, err := s.ReapTerminalBefore(ctx, cutoff); err != nil {
			m.logf("webhook: retention sweep of processed envelopes failed (rows retained): %v", err)
		}
	}
}

func (m *Manager) attempt(ctx context.Context, d Delivery) {
	// Same recover net as tick: the store (and the HTTP client's
	// rounds) run on the worker loop with no per-request net. A panic
	// mid-attempt leaves the row at its pre-attempt status, so the
	// next tick re-delivers (at-least-once) — the recovery posture the
	// error branches below already document.
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: delivery %s: attempt panicked (row stays pending → duplicate delivery): %v", d.ID, v)
		}
	}()
	// d.Attempts counts this claim. Past the budget means earlier claims
	// spent it without a settle (a worker died between claim and settle,
	// its lease expired, and this tick re-claimed the row): dead-letter
	// it without sending. schedule only checks the budget after a failed
	// send, so without this a MaxAttempts 1 delivery was sent once per
	// crash. No request is made, so no attempt is counted.
	if d.Attempts > m.opts.MaxAttempts {
		d.Status = StatusDead
		d.LastError = fmt.Sprintf("attempt budget exhausted: claimed %d times, max %d (a worker died between claim and settle)",
			d.Attempts, m.opts.MaxAttempts)
		d.NextAttemptAt = time.Time{}
		d.UpdatedAt = m.nowFn()
		m.logf("webhook: delivery %s: dead-lettered without sending, %s", d.ID, d.LastError)
		m.saveDelivery(d)
		return
	}
	// One delivery attempt is being processed: count it now. Every
	// non-success branch below bumps failuresTotal exactly once, so
	// failuresTotal stays <= deliveriesTotal. Success bumps nothing else.
	m.deliveriesTotal.Add(1)
	sub, err := m.store.GetSubscriber(ctx, d.SubscriberID)
	if err != nil && ctx.Err() != nil {
		m.refundShutdown(ctx, d)
		return
	}
	if err != nil {
		// Transient lookup failure (DB blip, canceled worker context
		// on a ctx-aware store): says nothing about the subscriber.
		// Retryable — StatusDead is reserved for an exhausted attempt
		// budget (Options.MaxAttempts) or a definitively gone/inactive
		// subscriber; conflating the two burned the whole retry budget
		// on one store blip.
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = fmt.Sprintf("subscriber lookup failed: %v", err)
		m.schedule(&d)
		m.saveDelivery(d)
		return
	}
	if sub == nil || !sub.Active {
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
	// d.Attempts already counts this attempt: the claim consumed it
	// (ClaimDueDeliveries bumps the row and the snapshot; the
	// non-leased fallback bumps it in tick). Bumping here again and
	// persisting it absolutely is exactly the write that let a stale
	// claimant's settle rewind the counter a re-claimant's claim had
	// consumed, so the settle never writes attempts (the fallback's bump
	// reaches the row as a relative +1, see unclaimedAttempt).
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
	client := m.opts.HTTPClient
	if m.plaintextAuth(*sub) {
		// A subscriber stored past Subscribe (or before this check
		// existed): fail the attempt rather than send the credential in
		// clear text. It stays failed until the URL or the option changes.
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = errPlaintextAuth.Error()
		d.ResponseStatus, d.ResponseBody = 0, ""
		m.schedule(&d)
		m.saveDelivery(d)
		return
	}
	if sub.Auth.Kind != AuthNone {
		sub.Auth.apply(req)
		client = m.credentialClient()
	}

	resp, err := client.Do(req)
	if err != nil && ctx.Err() != nil {
		// Stop cancelled the POST: a shutdown, not a failed attempt.
		m.refundShutdown(ctx, d)
		return
	}
	if err != nil {
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = err.Error()
		d.ResponseStatus, d.ResponseBody = 0, ""
		m.schedule(&d)
		m.saveDelivery(d)
		return
	}
	d.ResponseStatus = resp.StatusCode
	d.ResponseBody = sub.Auth.redact(m.readResponse(resp.Body))
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

// errPlaintextAuth refuses a credential on an http:// URL (see
// Options.AllowPlaintextAuth). It names no credential value.
var errPlaintextAuth = errors.New("webhook: Subscriber.Auth needs an https:// URL; set Options.AllowPlaintextAuth for a trusted plain-HTTP receiver")

// plaintextAuth reports whether sub would send its credential over
// plain HTTP without Options.AllowPlaintextAuth.
func (m *Manager) plaintextAuth(sub Subscriber) bool {
	if sub.Auth.Kind == AuthNone || m.opts.AllowPlaintextAuth {
		return false
	}
	u, err := url.Parse(sub.URL)
	return err != nil || !strings.EqualFold(u.Scheme, "https")
}

// defaultResponseBodyLimit is Options.ResponseBodyLimit's zero value.
const defaultResponseBodyLimit = 1024

// refuseRedirect is the CheckRedirect of a credential-bearing delivery:
// the 3xx itself is the answer, recorded as a failed attempt.
func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// credentialClient is the client a delivery carrying Subscriber.Auth
// uses: never one that follows a redirect.
//
// New builds it. A Manager made some other way gets one derived from
// its HTTPClient, and a bounded default client when that is nil too,
// never a nil dereference on the worker.
func (m *Manager) credentialClient() *http.Client {
	if m.credClient != nil {
		return m.credClient
	}
	if m.opts.HTTPClient == nil {
		return &http.Client{Timeout: 10 * time.Second, CheckRedirect: refuseRedirect}
	}
	c := *m.opts.HTTPClient
	c.CheckRedirect = refuseRedirect
	return &c
}

// readResponse keeps the first ResponseBodyLimit bytes of a receiver's
// body as safe text and drains the rest within MaxResponseBodyBytes,
// so the connection can be reused.
func (m *Manager) readResponse(body io.Reader) string {
	total := m.opts.MaxResponseBodyBytes
	if total > 0 {
		body = io.LimitReader(body, total)
	}
	keep := m.opts.ResponseBodyLimit
	if total > 0 && keep > total {
		keep = total
	}
	var kept string
	if keep > 0 {
		buf, _ := io.ReadAll(io.LimitReader(body, keep))
		kept = textsafe.ScrubControlBytes(strings.ToValidUTF8(string(buf), "\uFFFD"))
	}
	if total != 0 {
		_, _ = io.Copy(io.Discard, body)
	}
	return kept
}

// refundShutdown hands back an attempt the worker's shutdown cut short:
// the row goes back to pending, due now, with the attempt refunded, the
// way tick hands back claims it never started. Scheduled as a failure
// instead, a delivery on its last attempt was dead-lettered by a
// restart. The receiver may have seen the POST; the next send is the
// at-least-once retry it already has to dedupe (X-GoFastr-Delivery-ID).
//
// The plain-dequeue fallback bumped the attempt in this snapshot only,
// so leaving the row alone is its refund. A host LeasedStore with no
// release keeps the claim until its lease expires, as for unstarted
// claims in tick.
func (m *Manager) refundShutdown(ctx context.Context, d Delivery) {
	if d.unclaimedAttempt {
		return
	}
	if r, ok := m.store.(claimReleaser); ok {
		m.releaseClaims(ctx, r, []Delivery{d})
	}
}

// saveDelivery persists a delivery's post-attempt state, logging (never
// swallowing) a store error. A lost state write leaves the row at its
// pre-attempt status, so the next tick re-delivers (at-least-once) with no
// signal that the prior attempt ever happened, the only correct response is
// to make the loss observable via the configured Logger. The attempt's HTTP
// side-effect already occurred, so there is nothing else to do here.
//
// A background context is used deliberately: a worker cancellation is the
// reason the attempt is winding down, not a reason to lose the terminal
// state write. The write is bounded by settleTimeout, so a store blocked
// on a dead database cannot hold the worker past Stop.
func (m *Manager) saveDelivery(d Delivery) {
	// The recover net mirrors tick/attempt: UpdateDelivery is
	// host-supplied store code on the worker loop. A panic here loses
	// the state write exactly like the logged error path (row stays
	// stale → duplicate delivery), so it is attributed via the
	// configured Logger and the worker keeps running.
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: delivery %s: persist %s state panicked (row may be stale → duplicate delivery): %v", d.ID, d.Status, v)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	if err := m.store.UpdateDelivery(ctx, d); err != nil {
		m.logf("webhook: delivery %s: persist %s state failed (row may be stale → duplicate delivery): %v", d.ID, d.Status, err)
	}
}

// settleTimeout bounds a delivery's state write and a claim release
// after the worker detaches them from its own context.
const settleTimeout = 10 * time.Second

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
// failure, that's an operating-system-level fault and the manager
// must not silently mint colliding all-zero IDs.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("webhook: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
