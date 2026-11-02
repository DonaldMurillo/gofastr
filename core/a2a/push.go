package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/netguard"
)

// PushOptions tunes push-notification delivery. The zero value delivers
// over a guarded default client with private hosts refused.
type PushOptions struct {
	// Client delivers notifications. Nil means a 10s-timeout client on
	// the SSRF-guarded transport. A supplied client is used as a
	// template: redirect following is forced off (a 3xx must never carry
	// the notification token to a second host) and, unless AllowPrivate
	// is set, it is wrapped with the dial-time guard, so a caller cannot
	// accidentally opt registered URLs out of the SSRF posture by
	// bringing their own client.
	Client *http.Client
	// AllowPrivate permits internal hosts (loopback, RFC1918, CGNAT,
	// link-local). Tests and internal deployments only: a
	// caller-registered URL that the server POSTs to is an SSRF vector
	// otherwise.
	AllowPrivate bool
	// Disable turns the capability off: Capabilities().PushNotifications
	// reports false, CreateTaskPushNotificationConfig answers
	// CodePushNotificationNotSupported, and a config inside
	// SendMessage's configuration is refused the same way.
	Disable bool
}

// pushConcurrency caps in-flight push deliveries process-wide. Each
// delivery is one short HTTP POST; the cap keeps a task that mints
// events quickly from opening an unbounded number of connections.
const pushConcurrency = 16

// pushBacklog caps the events queued process-wide behind a config's
// in-flight delivery. Deliveries to one config go one at a time, in
// event order; past this bound an event is dropped like one past
// pushConcurrency.
const pushBacklog = pushConcurrency

// pushOwnerSlots and pushOwnerBacklog are one owner's share of the
// delivery slots and the backlog. Both pools are process-wide, so
// without a share one caller whose receiver hangs held every slot and
// queue entry, and every other owner's notifications were dropped.
const (
	pushOwnerSlots   = pushConcurrency / 4
	pushOwnerBacklog = pushBacklog / 4
)

// pushDeliveryTimeout bounds one push POST. A hung receiver must not
// accumulate goroutines for the TaskTimeout duration.
const pushDeliveryTimeout = 10 * time.Second

// pushLookupTimeout bounds the registration-time DNS check of a push URL.
const pushLookupTimeout = 3 * time.Second

type pusher struct {
	client       *http.Client
	disable      bool
	allowPrivate bool
	sem          chan struct{}
	log          *slog.Logger

	mu sync.Mutex
	// queues holds, per config with a delivery in flight, the events
	// waiting behind it. A key present with an empty queue is a config
	// whose drain goroutine is still posting.
	queues map[pushQueueKey][]pushItem
	queued int // events across all queues, bounded by pushBacklog
	// ownerSlots and ownerQueued count each owner's in-flight configs
	// and queued events, bounded by pushOwnerSlots and pushOwnerBacklog.
	// An owner at zero has no key.
	ownerSlots  map[string]int
	ownerQueued map[string]int
	// marks holds, per config, the newest task version enqueued for it.
	// Two publications of one task race between their persist and their
	// enqueue (a run's WORKING against a CancelTask's CANCELED), so the
	// older one can arrive second; serializing the POSTs alone kept that
	// reversed order and left a receiver's last word on a canceled task
	// at WORKING. An event older than the mark is dropped. A mark
	// outlives its config's queue by pushMarkRetention, past the longest
	// a publication can sit between persist and enqueue.
	marks     map[pushQueueKey]pushMark
	lastSweep time.Time
}

// pushMark is the newest version enqueued for one config, and when its
// queue last drained (zero while a delivery is in flight).
type pushMark struct {
	version int64
	idle    time.Time
}

// pushMarkRetention is how long a config's mark is kept once its queue
// drains. A publication lists its push configs under storeOpTimeout
// after it persists, so a stale event reaches enqueue within that bound
// of the newer one; twice the bound leaves margin.
func pushMarkRetention() time.Duration { return 2 * storeOpTimeout }

// pushQueueKey names one config of one owner's task.
type pushQueueKey struct{ owner, taskID, configID string }

type pushItem struct {
	cfg     PushNotificationConfig
	payload []byte
	version int64
}

func newPusher(opts PushOptions, log *slog.Logger) *pusher {
	var client *http.Client
	if opts.Client != nil {
		client = guardedClient(opts.Client, opts.AllowPrivate)
	} else {
		client = &http.Client{
			Timeout:   pushDeliveryTimeout,
			Transport: netguard.GuardedTransport(opts.AllowPrivate, "a2a"),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &pusher{
		client:       client,
		disable:      opts.Disable,
		allowPrivate: opts.AllowPrivate,
		sem:          make(chan struct{}, pushConcurrency),
		log:          log,
		queues:       map[pushQueueKey][]pushItem{},
		ownerSlots:   map[string]int{},
		ownerQueued:  map[string]int{},
		marks:        map[pushQueueKey]pushMark{},
	}
}

// deliver POSTs ev to every config so the run never waits on a
// webhook. Delivery is best effort by design: one attempt, no retry, a
// non-2xx or transport error is logged once at Warn. Reliable delivery
// with backoff is battery/queue's job; task progress must never stall
// on a receiver that is down.
//
// Deliveries to one config go one at a time, in event order: appended
// artifact chunks carry no sequence number, so a receiver handed C
// before B rebuilds the wrong artifact. Each config with a delivery in
// flight holds one of pushConcurrency slots and drains its own queue,
// so a slow receiver delays only its own config's events.
//
// The slot is taken BEFORE the drain goroutine starts and without
// blocking, and the queues are bounded by pushBacklog: past either
// bound the delivery is dropped with a Warn line. Waiting for a slot
// let goroutines and payloads pile up without limit behind one hung
// receiver.
//
// version is the task version the event's write produced. An event
// older than one already enqueued for a config is dropped for that
// config: delivery never goes backwards, so a terminal state stays the
// last thing a receiver sees.
func (p *pusher) deliver(recs []*PushConfigRecord, ev StreamResponse, version int64) {
	if p.disable || len(recs) == 0 {
		return
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		p.log.Error("a2a: marshal push payload", "err", err)
		return
	}
	for _, rec := range recs {
		cfg := rec.Config
		switch p.enqueue(pushQueueKey{rec.Owner, cfg.TaskID, cfg.ID}, pushItem{cfg, payload, version}) {
		case pushFull:
			p.log.Warn("a2a: push notification dropped: delivery backlog full", "taskId", cfg.TaskID, "url", cfg.URL, "inFlight", cap(p.sem))
		case pushStale:
			p.log.Debug("a2a: push notification dropped: a newer event was already enqueued", "taskId", cfg.TaskID, "version", version)
		}
	}
}

// enqueue queues it behind the config's in-flight delivery, or starts a
// drain for the config when none is in flight. It reports pushFull or
// pushStale when the event is dropped.
func (p *pusher) enqueue(key pushQueueKey, it pushItem) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.sweepMarks(now)
	mark, seen := p.marks[key]
	if seen && it.version < mark.version {
		return pushStale
	}
	mark.version = max(mark.version, it.version)
	if pending, busy := p.queues[key]; busy {
		p.marks[key] = pushMark{version: mark.version}
		if p.queued >= pushBacklog || p.ownerQueued[key.owner] >= pushOwnerBacklog {
			return pushFull
		}
		p.queues[key] = append(pending, it)
		p.queued++
		p.ownerQueued[key.owner]++
		return pushQueued
	}
	if p.ownerSlots[key.owner] >= pushOwnerSlots {
		p.marks[key] = pushMark{version: mark.version, idle: now}
		return pushFull
	}
	select {
	case p.sem <- struct{}{}:
	default:
		p.marks[key] = pushMark{version: mark.version, idle: now}
		return pushFull
	}
	p.marks[key] = pushMark{version: mark.version}
	p.queues[key] = nil
	p.ownerSlots[key.owner]++
	go p.drain(key, it)
	return pushQueued
}

// enqueue outcomes.
const (
	pushQueued = iota
	pushFull
	pushStale
)

// sweepMarks forgets the marks of configs idle past pushMarkRetention,
// at most once per retention period. Called with p.mu held.
func (p *pusher) sweepMarks(now time.Time) {
	keep := pushMarkRetention()
	if now.Sub(p.lastSweep) < keep {
		return
	}
	p.lastSweep = now
	for key, m := range p.marks {
		if !m.idle.IsZero() && now.Sub(m.idle) >= keep {
			delete(p.marks, key)
		}
	}
}

// drain posts it, then each event queued behind it for the same config,
// and gives the slot back once the config's queue is empty.
func (p *pusher) drain(key pushQueueKey, it pushItem) {
	defer func() { <-p.sem }()
	for {
		if err := p.post(it.cfg, it.payload); err != nil {
			p.log.Warn("a2a: push notification not delivered", "taskId", it.cfg.TaskID, "url", it.cfg.URL, "err", err)
		}
		p.mu.Lock()
		pending := p.queues[key]
		if len(pending) == 0 {
			delete(p.queues, key)
			decOwner(p.ownerSlots, key.owner)
			p.marks[key] = pushMark{version: p.marks[key].version, idle: time.Now()}
			p.mu.Unlock()
			return
		}
		it = pending[0]
		pending[0] = pushItem{}
		p.queues[key] = pending[1:]
		p.queued--
		decOwner(p.ownerQueued, key.owner)
		p.mu.Unlock()
	}
}

// decOwner takes one from owner's count, dropping the key at zero.
func decOwner(m map[string]int, owner string) {
	if m[owner] <= 1 {
		delete(m, owner)
		return
	}
	m[owner]--
}

// post sends one notification. The token and Authorization header ride
// only on this request; the client cannot follow a redirect (see
// PushOptions.Client), so they cannot leak to a second host.
func (p *pusher) post(cfg PushNotificationConfig, payload []byte) error {
	// Defence for a config stored before the registration-time check
	// existed: credentials never leave over plain http unless the
	// deployment opted into internal receivers.
	if hasCredentials(cfg) && !p.allowPrivate && !strings.EqualFold(schemeOf(cfg.URL), "https") {
		return fmt.Errorf("a2a: refusing to send push credentials over %q", cfg.URL)
	}
	// The deadline rides on the request as well as on the default
	// client's Timeout, so a caller-supplied client without one cannot
	// turn a hung receiver into a goroutine that never returns.
	ctx, cancel := context.WithTimeout(context.Background(), pushDeliveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Token != "" {
		req.Header.Set(PushNotificationTokenHeader, cfg.Token)
	}
	if cfg.Authentication != nil {
		scheme := cfg.Authentication.Scheme
		if (strings.EqualFold(scheme, "Bearer") || strings.EqualFold(scheme, "Basic")) && cfg.Authentication.Credentials != "" {
			req.Header.Set("Authorization", scheme+" "+cfg.Authentication.Credentials)
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("receiver answered %s", resp.Status)
	}
	return nil
}

// validatePushURL refuses URLs that target internal infrastructure. It
// runs at registration time so an obvious SSRF attempt never gets
// stored; the dial-time guard below is the real barrier, because a host
// can resolve public here and internal later (DNS rebinding).
//
// When allowPrivate is true the host checks are skipped (test/dev
// posture); the scheme and userinfo guards run in both modes.
func validatePushURL(raw string, credentialed, allowPrivate bool) error {
	if raw == "" {
		return fmt.Errorf("a2a: push URL required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("a2a: parse push URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("a2a: scheme %q not allowed (need http or https)", u.Scheme)
	}
	if scheme == "http" && credentialed && !allowPrivate {
		// The token and Authorization header ride on every delivery; over
		// plain http they ride in the clear. Loopback receivers in tests
		// and internal deployments opt in with AllowPrivate, the same
		// switch that admits their addresses.
		return fmt.Errorf("a2a: push URL %q carries credentials over plain http; use https (or set AllowPrivate for an internal receiver)", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("a2a: push URL missing host")
	}
	if u.User != nil {
		return fmt.Errorf("a2a: push URL with embedded userinfo not allowed (credential leakage)")
	}
	if allowPrivate {
		return nil
	}
	host := u.Hostname()
	// Hostname checks first: cheaper than DNS and catch the obvious
	// internal names however they resolve.
	lower := strings.ToLower(host)
	if lower == "localhost" ||
		strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") ||
		lower == "metadata.google.internal" {
		return fmt.Errorf("a2a: host %q targets internal infrastructure", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return rejectInternal(ip)
	}
	// Bounded: the hostname is client-supplied, and a name whose
	// resolver stalls must not hold the registration request for the
	// resolver's own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), pushLookupTimeout)
	defer cancel()
	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// DNS failure at registration is not a refusal: the receiver
		// need not be live yet, and the dial-time guard is what stops a
		// later resolution onto an internal address.
		return nil
	}
	for _, ip := range ipAddrs {
		if err := rejectInternal(ip.IP); err != nil {
			return err
		}
	}
	return nil
}

// rejectInternal applies the repo's one internal-address predicate.
func rejectInternal(ip net.IP) error {
	if netguard.IsInternal(ip) {
		return fmt.Errorf("a2a: %s not allowed as push target", netguard.Reason(ip))
	}
	return nil
}

// guardedClient returns a shallow copy of c with redirects disabled and
// the SSRF guard applied without disturbing the caller's routing: a nil
// transport gets the guarded dial-time hook (strongest); a plain
// *http.Transport with no proxy and no custom dialer is cloned and given
// the same dial-time hook, so the address the guard checks is the
// address dialed (its TLS and pool settings survive the clone). Any
// other transport (a proxy, a custom dialer, a non-*http.Transport
// RoundTripper) is left untouched and wrapped with a per-request
// resolved-IP check, because swapping its dialer would silently break
// an egress proxy or unix-socket transport that dials internal
// addresses by design. That wrapper resolves once to check and the
// inner transport resolves again to dial, so a name that rebinds
// between the two passes it: it is the weaker posture, kept only where
// the dialer cannot be replaced.
func guardedClient(c *http.Client, allowPrivate bool) *http.Client {
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if allowPrivate {
		return &cc
	}
	if c.Transport == nil {
		cc.Transport = netguard.GuardedTransport(false, "a2a")
	} else if guarded, ok := plainGuarded(c.Transport); ok {
		cc.Transport = guarded
	} else {
		cc.Transport = &guardedRoundTripper{inner: c.Transport}
	}
	return &cc
}

// plainGuarded returns the dial-guarded clone of rt when it is a plain
// *http.Transport (netguard.GuardPlainTransport).
func plainGuarded(rt http.RoundTripper) (*http.Transport, bool) {
	tr, ok := rt.(*http.Transport)
	if !ok {
		return nil, false
	}
	return netguard.GuardPlainTransport(tr, "a2a")
}

// guardedRoundTripper is the fallback guard for custom RoundTrippers
// the dialer hook cannot reach: it resolves the request host and
// refuses when any resolved address is internal.
type guardedRoundTripper struct{ inner http.RoundTripper }

func (g *guardedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if err := rejectInternal(ip); err != nil {
			return nil, err
		}
		return g.inner.RoundTrip(r)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), host)
	if err != nil {
		return nil, fmt.Errorf("a2a: resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if err := rejectInternal(ip.IP); err != nil {
			return nil, err
		}
	}
	//gofastr:allow(resolvedial) fallback for a transport with its own proxy or dialer, which cannot be swapped; a plain one gets netguard.GuardPlainTransport
	return g.inner.RoundTrip(r)
}

// hasCredentials reports whether a delivery to cfg would carry a secret:
// the notification token, or Authorization credentials.
func hasCredentials(cfg PushNotificationConfig) bool {
	if cfg.Token != "" {
		return true
	}
	return cfg.Authentication != nil && cfg.Authentication.Credentials != ""
}

func schemeOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme
}
