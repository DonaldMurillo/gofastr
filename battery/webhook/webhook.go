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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/tracing"
)

// MaxPayloadBytes caps a published payload; Publish refuses a larger one.
const MaxPayloadBytes = 1 << 20

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

// Subscriber is an HTTP endpoint that receives signed POSTs for the events its
// patterns match ("*" one segment, "**" one or more).
type Subscriber struct {
	ID      string
	URL     string
	Secret  string
	Events  []string
	Active  bool
	Paused  bool // Paused is an explicit opt-out; Subscribe honors it.
	Created time.Time
	// Auth is the credential each delivery sends, sealed at rest; Subscribe
	// replaces it with the rest of the subscriber.
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

func isTerminalStatus(s DeliveryStatus) bool {
	return s == StatusSuccess || s == StatusDead
}

func settleFence(next DeliveryStatus) []DeliveryStatus {
	switch {
	case !isTerminalStatus(next):
		return []DeliveryStatus{StatusSuccess, StatusDead}
	case next != StatusSuccess:
		return []DeliveryStatus{StatusSuccess}
	}
	return nil
}

func claimFenced(d Delivery, rowNextAttemptAt time.Time) bool {
	return leaseFenced(d) && !rowNextAttemptAt.Equal(d.claimedUntil)
}

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
	Owner string

	// ResponseStatus is the HTTP status of the last attempt that got a
	// response, or 0.
	ResponseStatus int
	// ResponseBody is the first Options.ResponseBodyLimit bytes of that
	// response, as safe text with the credential redacted.
	ResponseBody string

	claimedUntil time.Time

	unclaimedAttempt bool
}

// Store is the persistence interface. A pending delivery with a zero
// NextAttemptAt is due now.
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

// LeasedStore is the optional Store upgrade for several workers on one store:
// it claims due rows under a lease and consumes one attempt per claim.
type LeasedStore interface {
	Store
	ClaimDueDeliveries(ctx context.Context, now time.Time, limit int, leasePeriod time.Duration) ([]Delivery, error)
}

// RetentionSweeper is the optional capability the retention pass uses to
// delete terminal rows older than cutoff.
type RetentionSweeper interface {
	ReapTerminalBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// ReplayableStore is the optional Store capability for dead-letter inspection
// and replay.
type ReplayableStore interface {
	Store
	// ListDeadDeliveries returns up to limit dead deliveries, newest first.
	ListDeadDeliveries(ctx context.Context, limit int) ([]Delivery, error)
	// ResetDelivery returns a dead delivery to pending and due now; any other
	// id is a no-op.
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

// Replay re-queues a dead delivery when the store supports it.
func (m *Manager) Replay(ctx context.Context, id string) error {
	rs, ok := m.store.(ReplayableStore)
	if !ok {
		return fmt.Errorf("webhook: store %T does not support replay", m.store)
	}
	return rs.ResetDelivery(ctx, id)
}

// DefaultMaxResponseBodyBytes is the response read cap when
// Options.MaxResponseBodyBytes is unset.
const DefaultMaxResponseBodyBytes int64 = 64 << 10

// Options configures the Manager.
type Options struct {
	MaxAttempts          int             // attempts before a delivery is dead; default 6
	Backoff              []time.Duration // wait after attempt i at [i-1], the last reused; default 30s to 3h
	HTTPClient           *http.Client    // default: 10s timeout, no redirects, SSRF-guarded
	PollInterval         time.Duration   // default 1s
	MaxResponseBodyBytes int64           // response read cap; default 64 KiB, negative for none
	AllowPrivateNetworks bool            // turns the SSRF guard off

	// AllowPlaintextAuth lets a subscriber with Auth use an http:// URL.
	AllowPlaintextAuth bool
	SignatureTolerance time.Duration // documentation only: receivers pass it to VerifyTimestamped
	LeasePeriod        time.Duration // claim lease with a LeasedStore; default 30s, raised past the client timeout

	// Signature signs deliveries in a receiver's existing scheme; nil keeps
	// the GoFastr format.
	Signature *SignatureFormat

	// ResponseBodyLimit is how many bytes of each response are kept on the
	// delivery. Zero means 1024; negative keeps none.
	ResponseBodyLimit int64

	// Retention, when positive, reaps successful deliveries and processed
	// envelopes older than it. Zero keeps every row.
	Retention time.Duration

	// InboundStore, when set, is swept by the same retention pass.
	InboundStore InboundStore

	// Logger receives warnings the worker cannot return to a caller. Default
	// log.Printf.
	Logger func(format string, args ...any)
}

// Manager owns the worker goroutine and is the entry point for Publish and
// subscriber management.
type Manager struct {
	store      Store
	opts       Options
	sig        SignatureFormat
	credClient *http.Client

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	runCtx    context.Context
	runCancel context.CancelFunc

	nowFn func() time.Time

	deliveriesTotal atomic.Uint64
	failuresTotal   atomic.Uint64
}

// New constructs a Manager with defaults applied; it panics on an invalid
// Options.Signature.
func New(s Store, opts Options) *Manager {
	sig := defaultSignatureFormat
	if opts.Signature != nil {
		if err := opts.Signature.Validate(); err != nil {
			panic(err.Error())
		}
		sig = *opts.Signature
	}
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
		opts.HTTPClient = ssrfGuardClient(opts.HTTPClient)
	}
	if _, leased := s.(LeasedStore); leased {
		if t := opts.HTTPClient.Timeout; t > 0 && opts.LeasePeriod <= t {
			raised := leaseForClientTimeout(t)
			opts.Logger("webhook: LeasePeriod %s is not longer than the HTTP client timeout %s; raised to %s "+
				"so a slow receiver cannot outlive its claim and be sent the delivery twice", opts.LeasePeriod, t, raised)
			opts.LeasePeriod = raised
		}
	}
	noRedirect := *opts.HTTPClient
	noRedirect.CheckRedirect = refuseRedirect
	return &Manager{
		credClient: &noRedirect,
		sig:        sig,
		store:      s,
		opts:       opts,
		stopCh:     make(chan struct{}),
		nowFn:      time.Now,
	}
}

func leaseForClientTimeout(timeout time.Duration) time.Duration {
	const maxLease = time.Duration(1<<63 - 1)
	if timeout > maxLease/2 {
		return maxLease
	}
	return 2 * timeout
}

func (m *Manager) logf(format string, args ...any) {
	if m.opts.Logger != nil {
		m.opts.Logger(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Subscribe registers or replaces a Subscriber, assigning an ID when empty. It
// refuses an internal URL unless AllowPrivateNetworks is set.
func (m *Manager) Subscribe(ctx context.Context, s Subscriber) (Subscriber, error) {
	if !textsafe.Storable(s.Secret) {
		return Subscriber{}, errors.New("webhook: subscriber Secret must be valid UTF-8 without NUL")
	}
	if !textsafe.Storable(s.ID) {
		return Subscriber{}, errors.New("webhook: subscriber ID must be valid UTF-8 without NUL")
	}
	if s.Secret == "" {
		return Subscriber{}, errors.New("webhook: subscriber Secret required")
	}
	if err := s.Auth.validate(); err != nil {
		return Subscriber{}, err
	}
	if m.plaintextAuth(s) {
		return Subscriber{}, errPlaintextAuth
	}
	if m.authOnSignatureHeader(s) {
		return Subscriber{}, errAuthOnSignatureHeader
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
	if !textsafe.Storable(id) {
		return errors.New("webhook: subscriber ID must be valid UTF-8 without NUL")
	}
	return m.store.DeleteSubscriber(ctx, id)
}

// Subscribers returns every registered Subscriber.
func (m *Manager) Subscribers(ctx context.Context) ([]Subscriber, error) {
	return m.store.ListSubscribers(ctx)
}

// Publish fans the event out to every active matching subscriber, owned by no
// user.
func (m *Manager) Publish(ctx context.Context, event string, payload []byte) (int, error) {
	return m.PublishFor(ctx, "", event, payload)
}

// PublishFor is Publish for a payload that belongs to owner; erasing owner
// deletes the deliveries.
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

// Start launches the worker goroutine; later calls are no-ops.
func (m *Manager) Start() {
	m.startOnce.Do(func() {
		m.runCtx, m.runCancel = context.WithCancel(context.Background())
		m.wg.Add(1)
		go m.runWorker()
	})
}

// Stop stops the worker, cancels in-flight attempts and waits, or returns
// ctx.Err if ctx ends first.
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

func (m *Manager) tick(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: store panic in tick (due deliveries stay pending): %s", textsafe.Recovered(v))
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
		for i := range due {
			due[i].Attempts++
			due[i].unclaimedAttempt = true
		}
	}
	if err != nil {
		m.logf("webhook: read due deliveries failed: %v", err)
		return
	}
	for i, d := range due {
		if ctx.Err() != nil {
			if r, ok := m.store.(claimReleaser); ok {
				m.releaseClaims(ctx, r, due[i:])
			}
			return
		}
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

type claimReleaser interface {
	releaseClaim(ctx context.Context, d Delivery, now time.Time) error
}

func (m *Manager) leaseTooShort(d Delivery) bool {
	if d.claimedUntil.IsZero() {
		return false
	}
	need := m.opts.LeasePeriod / 2
	if t := m.opts.HTTPClient.Timeout; t > 0 {
		need = min(t, need)
	}
	return m.nowFn().Add(need).After(d.claimedUntil)
}

func (m *Manager) releaseClaims(ctx context.Context, r claimReleaser, ds []Delivery) {
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: release of unstarted claims panicked (they are re-claimed when the lease expires): %s", textsafe.Recovered(v))
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
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: delivery %s: attempt panicked (row stays pending → duplicate delivery): %s", d.ID, textsafe.Recovered(v))
		}
	}()
	if d.Attempts > m.opts.MaxAttempts {
		m.deliveriesTotal.Add(1)
		m.failuresTotal.Add(1)
		d.Status = StatusDead
		d.LastError = fmt.Sprintf("attempt budget exhausted: claimed %d times, max %d (a worker died between claim and settle)",
			d.Attempts, m.opts.MaxAttempts)
		d.NextAttemptAt = time.Time{}
		d.UpdatedAt = m.nowFn()
		m.logf("webhook: delivery %s: dead-lettered without sending, %s", d.ID, d.LastError)
		m.saveDelivery(d)
		return
	}
	m.deliveriesTotal.Add(1)
	sub, err := m.store.GetSubscriber(ctx, d.SubscriberID)
	if err != nil && ctx.Err() != nil {
		m.refundShutdown(ctx, d)
		return
	}
	if err != nil {
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
		m.saveDelivery(d)
		return
	}
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
	m.sig.setHeaders(req.Header, sub.Secret, ts, d.Event, d.ID, d.Payload)
	client := m.opts.HTTPClient
	refusal := error(nil)
	switch {
	case m.plaintextAuth(*sub):
		refusal = errPlaintextAuth
	case m.authOnSignatureHeader(*sub):
		refusal = errAuthOnSignatureHeader
	}
	if refusal != nil {
		m.failuresTotal.Add(1)
		d.Status = StatusFailed
		d.LastError = refusal.Error()
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
	d.ResponseBody = m.readResponse(resp.Body, sub.Auth)
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

var errPlaintextAuth = errors.New("webhook: Subscriber.Auth needs an https:// URL; set Options.AllowPlaintextAuth for a trusted plain-HTTP receiver")

func (m *Manager) plaintextAuth(sub Subscriber) bool {
	if sub.Auth.Kind == AuthNone || m.opts.AllowPlaintextAuth {
		return false
	}
	u, err := url.Parse(sub.URL)
	return err != nil || !strings.EqualFold(u.Scheme, "https")
}

var errAuthOnSignatureHeader = errors.New("webhook: Subscriber.Auth header is one the signature format writes")

func (m *Manager) authOnSignatureHeader(sub Subscriber) bool {
	if sub.Auth.Kind == AuthNone {
		return false
	}
	return slices.Contains(m.sig.headers(), http.CanonicalHeaderKey(sub.Auth.headerName()))
}

const defaultResponseBodyLimit = 1024

func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

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

func (m *Manager) readResponse(body io.Reader, auth Auth) string {
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
		buf, _ := io.ReadAll(io.LimitReader(body, keep+int64(auth.longestSecret())))
		kept = auth.redactHead(string(buf), int(keep))
		if int64(len(buf)) > keep && int64(len(kept)) > keep {
			kept = kept[:keep]
		}
		kept = textsafe.ScrubControlBytes(strings.ToValidUTF8(kept, "\uFFFD"))
	}
	if total != 0 {
		_, _ = io.Copy(io.Discard, body)
	}
	return kept
}

func (m *Manager) refundShutdown(ctx context.Context, d Delivery) {
	if d.unclaimedAttempt {
		return
	}
	if r, ok := m.store.(claimReleaser); ok {
		m.releaseClaims(ctx, r, []Delivery{d})
	}
}

func (m *Manager) saveDelivery(d Delivery) {
	defer func() {
		if v := recover(); v != nil {
			m.logf("webhook: delivery %s: persist %s state panicked (row may be stale → duplicate delivery): %s", d.ID, d.Status, textsafe.Recovered(v))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	if err := m.store.UpdateDelivery(ctx, d); err != nil {
		m.logf("webhook: delivery %s: persist %s state failed (row may be stale → duplicate delivery): %v", d.ID, d.Status, err)
	}
}

const settleTimeout = 10 * time.Second

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

// DeliveriesTotal returns the number of delivery attempts the worker has
// processed.
func (m *Manager) DeliveriesTotal() uint64 {
	return m.deliveriesTotal.Load()
}

// FailuresTotal returns the number of attempts that did not succeed, never
// more than DeliveriesTotal.
func (m *Manager) FailuresTotal() uint64 {
	return m.failuresTotal.Load()
}

// MetricsCollector returns a Prometheus text collector for the delivery
// counters.
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

func matchesAny(event string, patterns []string) bool {
	for _, p := range patterns {
		if matchOne(event, p) {
			return true
		}
	}
	return false
}

func matchOne(event, pattern string) bool {
	if pattern == "*" || pattern == "**" || pattern == event {
		return true
	}
	return matchSegments(strings.Split(event, "."), strings.Split(pattern, "."))
}

func matchSegments(event, pattern []string) bool {
	if len(pattern) == 0 {
		return len(event) == 0
	}
	if pattern[0] == "**" {
		if len(event) == 0 {
			return false
		}
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

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("webhook: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
