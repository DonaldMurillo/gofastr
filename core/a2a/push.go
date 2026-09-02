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
	"syscall"
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
}

func newPusher(opts PushOptions, log *slog.Logger) *pusher {
	var client *http.Client
	if opts.Client != nil {
		client = guardedClient(opts.Client, opts.AllowPrivate)
	} else {
		client = &http.Client{
			Timeout:   pushDeliveryTimeout,
			Transport: guardedTransport(opts.AllowPrivate),
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
	}
}

// deliver POSTs ev to every config, each in its own goroutine so the
// run never waits on a webhook. Delivery is best effort by design: one
// attempt, no retry, a non-2xx or transport error is logged once at
// Warn. Reliable delivery with backoff is battery/queue's job; task
// progress must never stall on a receiver that is down.
func (p *pusher) deliver(recs []*PushConfigRecord, ev StreamResponse) {
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
		go func() {
			p.sem <- struct{}{}
			defer func() { <-p.sem }()
			if err := p.post(cfg, payload); err != nil {
				p.log.Warn("a2a: push notification not delivered", "taskId", cfg.TaskID, "url", cfg.URL, "err", err)
			}
		}()
	}
}

// post sends one notification. The token and Authorization header ride
// only on this request; the client cannot follow a redirect (see
// PushOptions.Client), so they cannot leak to a second host.
func (p *pusher) post(cfg PushNotificationConfig, payload []byte) error {
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
func validatePushURL(raw string, allowPrivate bool) error {
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

// guardedTransport builds the outbound transport for push delivery.
// The net.Dialer.Control hook re-runs the internal-address check on the
// ACTUAL resolved address at connect time, which is what closes the
// DNS-rebinding window validatePushURL cannot: a host that passed
// validation while resolving public and is later re-pointed at
// 127.0.0.1 or 169.254.169.254 never gets dialed. When allowPrivate is
// true the dial-time check is skipped, matching the registration-time
// opt-out. Mirrors battery/webhook/ssrf.go, on core/netguard.
func guardedTransport(allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// Control sees the already-resolved numeric address; a
				// non-IP here is unexpected, refuse rather than dial
				// blind.
				return fmt.Errorf("a2a: dial address %q is not a resolved IP", address)
			}
			return rejectInternal(ip)
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	return tr
}

// guardedClient returns a shallow copy of c with redirects disabled and
// the SSRF guard applied without disturbing the caller's routing: a nil
// transport gets the guarded dial-time hook (strongest); a custom
// transport is left untouched and wrapped with a per-request
// resolved-IP check, because swapping its dialer would silently break
// an egress proxy or unix-socket transport that dials internal
// addresses by design.
func guardedClient(c *http.Client, allowPrivate bool) *http.Client {
	cc := *c
	cc.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if c.Transport == nil && !allowPrivate {
		cc.Transport = guardedTransport(false)
	} else if !allowPrivate {
		cc.Transport = &guardedRoundTripper{inner: c.Transport}
	}
	return &cc
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
	return g.inner.RoundTrip(r)
}
