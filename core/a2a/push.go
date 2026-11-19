package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// PushOptions tunes push-notification delivery. The zero value delivers over
// a guarded default client with private hosts refused.
type PushOptions struct {
	// Client delivers notifications; nil means a 10s-timeout guarded client.
	// A supplied client never follows redirects and is wrapped with the SSRF
	// guard unless AllowPrivate is set.
	Client *http.Client
	// AllowPrivate permits internal hosts (loopback, RFC1918, CGNAT, link-
	// local).
	AllowPrivate bool
	// Disable turns the capability off: Capabilities().PushNotifications
	// reports false, CreateTaskPushNotificationConfig answers
	// CodePushNotificationNotSupported, and a config inside
	// SendMessage's configuration is refused the same way.
	Disable bool
}

const pushConcurrency = 16

const pushBacklog = pushConcurrency

const (
	pushOwnerSlots   = pushConcurrency / 4
	pushOwnerBacklog = pushBacklog / 4
)

const pushDeliveryTimeout = 10 * time.Second

const pushLookupTimeout = 3 * time.Second

type pusher struct {
	client       *http.Client
	disable      bool
	allowPrivate bool
	sem          chan struct{}
	log          *slog.Logger

	mu          sync.Mutex
	queues      map[pushQueueKey][]pushItem
	queued      int // events across all queues, bounded by pushBacklog
	ownerSlots  map[string]int
	ownerQueued map[string]int
	marks       map[pushQueueKey]pushMark
	lastSweep   time.Time
}

type pushMark struct {
	version int64
	idle    time.Time
}

func pushMarkRetention() time.Duration { return 2 * storeOpTimeout }

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

func (p *pusher) drain(key pushQueueKey, it pushItem) {
	defer func() { <-p.sem }()
	for {
		if err := p.postGuarded(it.cfg, it.payload); err != nil {
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

var errPushPanicked = errors.New("a2a: push delivery panicked")

func (p *pusher) postGuarded(cfg PushNotificationConfig, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("a2a: push delivery panicked",
				"taskId", cfg.TaskID, "panic", textsafe.Recovered(r))
			err = errPushPanicked
		}
	}()
	return p.post(cfg, payload)
}

func decOwner(m map[string]int, owner string) {
	if m[owner] <= 1 {
		delete(m, owner)
		return
	}
	m[owner]--
}

func (p *pusher) post(cfg PushNotificationConfig, payload []byte) error {
	if hasCredentials(cfg) && !p.allowPrivate && !strings.EqualFold(schemeOf(cfg.URL), "https") {
		return fmt.Errorf("a2a: refusing to send push credentials over %q", cfg.URL)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), pushLookupTimeout)
	defer cancel()
	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	for _, ip := range ipAddrs {
		if err := rejectInternal(ip.IP); err != nil {
			return err
		}
	}
	return nil
}

func rejectInternal(ip net.IP) error {
	if netguard.IsInternal(ip) {
		return fmt.Errorf("a2a: %s not allowed as push target", netguard.Reason(ip))
	}
	return nil
}

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

func plainGuarded(rt http.RoundTripper) (*http.Transport, bool) {
	tr, ok := rt.(*http.Transport)
	if !ok {
		return nil, false
	}
	return netguard.GuardPlainTransport(tr, "a2a")
}

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
