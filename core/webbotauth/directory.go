package webbotauth

// directory.go: resolving a Signature-Agent URL to key material.
//
// The fetch target comes from an attacker-controlled request header, so
// this file is an SSRF boundary. The controls, each required by draft
// section 6.7 / Appendix C and each pinned by a test in
// directory_test.go:
//
//   - https only; http, file, and every other scheme are refused.
//   - netguard.IsInternal checked three times per fetch: on the literal
//     host (no DNS), on every DNS answer before dialing, and — the
//     authoritative check — in net.Dialer.Control on the actual
//     resolved address at connect time, which closes the TOCTOU window
//     where a name resolves public for the check and private for the
//     dial (DNS rebinding).
//   - redirects are not followed at all. draft-02 section 5.5 requires
//     non-200 to be a discovery failure and forbids automatic
//     redirects, which also removes the re-check-per-hop problem: there
//     are zero hops.
//   - response bodies are capped (256 KiB after content decoding, read
//     through io.LimitReader so an oversized body is detected without
//     buffering it).
//   - every fetch runs under a wall-clock context timeout.
//   - the key count is capped at maxDirectoryKeys.
//
// Cache policy (draft Appendix C.4/C.5, section 6.10):
//
//   - Successful fetches replace the cached entry wholesale, including
//     when the new set drops a key — that is the rotation mechanism
//     ("a directory that resolves and does not contain the key is
//     evidence; it is newer than whatever the verifier holds").
//   - Positive TTL honors Cache-Control max-age / Expires when present,
//     clamped to [1min, 24h], default 1h.
//   - Failed fetches never evict a positive entry: a directory outage
//     must not revoke the operator's keys at every verifier at once
//     (section 6.10). A stale entry is served until the directory
//     answers again. The trade-off is deliberate and follows the draft:
//     availability wins over revocation latency; rotation takes effect
//     on the next successful refetch.
//   - Failures are negative-cached for a short TTL (<= 5 min, draft
//     C.5) so a flood of unresolvable agent URLs cannot force a fetch
//     per request.
//   - Positive and negative entries live in separate bounded LRU maps:
//     an attacker spraying junk URLs can only churn the negative map,
//     never evict a real agent's keys.

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/core/netguard"
)

const (
	directoryWellKnown = "/.well-known/http-message-signatures-directory"

	defaultMaxDirectoryBytes = 256 << 10 // 256 KiB
	defaultFetchTimeout      = 5 * time.Second
	defaultPositiveTTL       = 1 * time.Hour
	minPositiveTTL           = 1 * time.Minute
	maxPositiveTTL           = 24 * time.Hour
	defaultNegativeTTL       = 5 * time.Minute // draft C.5 ceiling
	defaultCacheEntries      = 256             // per map
	dnsLookupTimeout         = 2 * time.Second // attacker-chosen hostname
	maxConcurrentFetches     = 8               // across all identifiers
	acceptDirectory          = "application/http-message-signatures-directory+json, application/json;q=0.9"
)

// discoveryType names the Signature-Agent member's type parameter.
type discoveryType string

const (
	discoveryDirectory discoveryType = "directory"
	discoveryJWKS      discoveryType = "jwks_uri"
)

// agentRef is one Signature-Agent member resolved to a fetch plan.
type agentRef struct {
	identifier string        // cache key: the URL the verifier resolves
	fetchURL   string        // what is actually fetched
	typ        discoveryType // how the URL was derived
}

// parseAgentRef validates a Signature-Agent member value and derives
// the fetch URL plus identifier (draft section 5.5).
func parseAgentRef(ctx context.Context, raw string, typ discoveryType) (*agentRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("signature-agent URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, fmt.Errorf("signature-agent URL scheme must be https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("signature-agent URL missing host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("signature-agent URL with embedded userinfo not allowed")
	}
	if err := checkHostPublic(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	if typ == "" {
		typ = discoveryDirectory
	}
	switch typ {
	case discoveryDirectory:
		// The member value must be an origin: empty path ("/ MAY be
		// accepted"), no query, no fragment. The identifier is the
		// well-known URI at that origin.
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("directory-type signature-agent must be an origin, got path %q", u.Path)
		}
		if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return nil, fmt.Errorf("directory-type signature-agent must not carry a query or fragment")
		}
		wk := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: directoryWellKnown}
		return &agentRef{identifier: wk.String(), fetchURL: wk.String(), typ: typ}, nil
	case discoveryJWKS:
		// Fetched as sent; the identifier drops the fragment so one key
		// set cannot mint an identifier per spelling.
		id := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
		fetch := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
		return &agentRef{identifier: id.String(), fetchURL: fetch.String(), typ: typ}, nil
	default:
		return nil, fmt.Errorf("unsupported signature-agent discovery type %q", typ)
	}
}

// checkHostPublic rejects hostnames that target internal
// infrastructure before any network activity: the obvious internal
// names, literal internal IPs, and hostnames whose DNS answers include
// an internal address. The dial-time Control hook re-checks the
// address actually dialed.
func checkHostPublic(ctx context.Context, host string) error {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal" {
		return fmt.Errorf("webbotauth: host %q targets internal infrastructure", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := netguard.Reason(ip); reason != "" {
			return fmt.Errorf("webbotauth: %s %s not allowed", reason, ip)
		}
		return nil
	}
	// Bounded, and tied to the request: an attacker picks this hostname,
	// so an un-deadlined lookup lets a slow authoritative server hold the
	// goroutine for the resolver's full retry budget. This runs ahead of
	// the fetch's own timeout and repeats even when the identifier is
	// negative-cached, so the bound has to live here.
	lookupCtx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		// Unresolvable now: not a policy refusal. The fetch (and its
		// dial-time guard) will fail and negative-cache the outcome.
		return nil
	}
	for _, a := range addrs {
		ip := a.IP
		if reason := netguard.Reason(ip); reason != "" {
			return fmt.Errorf("webbotauth: %s %s not allowed (via %q)", reason, ip, host)
		}
	}
	return nil
}

// guardedTransport is the outbound transport: redirects disabled,
// timeouts, and the dial-time netguard check on the resolved address.
// Built once per resolver; the dialer Control sees every connect.
// allowPrivate skips the dial-time check for the in-package tests that
// dial loopback test servers deliberately; the production constructor
// always passes false and no exported surface exposes the knob.
func guardedTransport(allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
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
				// Control receives the already-resolved numeric address.
				return fmt.Errorf("webbotauth: dial address %q is not a resolved IP", address)
			}
			if reason := netguard.Reason(ip); reason != "" {
				return fmt.Errorf("webbotauth: %s %s not allowed", reason, ip)
			}
			return nil
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	tr.TLSHandshakeTimeout = 3 * time.Second
	tr.ResponseHeaderTimeout = 4 * time.Second
	return tr
}

// cacheEntry is one cached directory resolution.
type cacheEntry struct {
	keys      []jwk
	expiresAt time.Time
	// stale is set on the positive entry that a failed refresh left
	// behind; the entry keeps serving past expiresAt (draft 6.10).
	stale bool
}

// negativeEntry is a cached failure.
type negativeEntry struct {
	reason    string
	expiresAt time.Time
}

// lruShard is a tiny LRU: map + move-to-front list, bounded.
type lruShard struct {
	max   int
	mu    sync.Mutex
	keys  map[string]*list.Element
	order *list.List
}

func newLRUShard(max int) *lruShard {
	return &lruShard{max: max, keys: make(map[string]*list.Element, max), order: list.New()}
}

// get returns the value and bumps it to most-recently-used.
func (l *lruShard) get(key string) (any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.keys[key]
	if !ok {
		return nil, false
	}
	l.order.MoveToFront(el)
	return el.Value.(*lruElement).val, true
}

func (l *lruShard) put(key string, val any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.keys[key]; ok {
		el.Value.(*lruElement).val = val
		l.order.MoveToFront(el)
		return
	}
	el := l.order.PushFront(&lruElement{key: key, val: val})
	l.keys[key] = el
	if len(l.keys) > l.max {
		oldest := l.order.Back()
		if oldest != nil {
			l.order.Remove(oldest)
			delete(l.keys, oldest.Value.(*lruElement).key)
		}
	}
}

func (l *lruShard) delete(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.keys[key]; ok {
		l.order.Remove(el)
		delete(l.keys, el.Value.(*lruElement).key)
	}
}

type lruElement struct {
	key string
	val any
}

// directoryResolver caches and fetches agent directories.
type directoryResolver struct {
	client  *http.Client
	maxBody int64

	positiveTTL time.Duration
	negativeTTL time.Duration
	maxEntries  int
	log         *slog.Logger

	pos *lruShard // identifier -> cacheEntry
	neg *lruShard // identifier -> negativeEntry

	mu       sync.Mutex
	inflight map[string]*fetchCall
	now      func() time.Time

	// sem caps outbound fetches across every identifier. The inflight
	// map already collapses duplicate fetches for one identifier, but
	// an attacker naming many distinct resolvable hosts is not
	// duplicating anything, so that map bounds nothing for them.
	sem chan struct{}
}

type fetchCall struct {
	done chan struct{}
	set  *jwkSet
	err  error
}

func newDirectoryResolver(log *slog.Logger) *directoryResolver {
	return &directoryResolver{
		client: &http.Client{
			Transport: guardedTransport(false),
			// draft section 5.5: never follow redirects; the
			// CheckRedirect hook fires before any second connection is
			// opened, so a 3xx re-opens no SSRF question at all.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxBody:     defaultMaxDirectoryBytes,
		positiveTTL: defaultPositiveTTL,
		negativeTTL: defaultNegativeTTL,
		maxEntries:  defaultCacheEntries,
		log:         log,
		pos:         newLRUShard(defaultCacheEntries),
		neg:         newLRUShard(defaultCacheEntries),
		inflight:    make(map[string]*fetchCall),
		now:         time.Now,
		sem:         make(chan struct{}, maxConcurrentFetches),
	}
}

// resolve returns the key set for an identifier, fetching and caching
// as needed. Concurrent calls for one identifier coalesce into a single
// fetch (draft C.3).
func (d *directoryResolver) resolve(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	now := d.now()
	if v, ok := d.pos.get(ref.identifier); ok {
		e := v.(*cacheEntry)
		if now.Before(e.expiresAt) {
			return &jwkSet{keys: e.keys}, nil
		}
		// TTL lapsed: refresh (blocking, bounded by the fetch timeout).
		// A successful fetch replaces the entry wholesale - the
		// rotation path. A failed fetch is not evidence (draft 6.10)
		// and the cached keys keep serving, however old: an operator's
		// directory outage must not revoke its keys at every verifier
		// at once.
		set, err := d.fetchCoalesced(ctx, ref)
		if err != nil {
			return &jwkSet{keys: e.keys}, nil
		}
		return set, nil
	}
	if v, ok := d.neg.get(ref.identifier); ok {
		e := v.(*negativeEntry)
		if now.Before(e.expiresAt) {
			return nil, fmt.Errorf("discovery failed (cached): %s", e.reason)
		}
	}
	set, err := d.fetchCoalesced(ctx, ref)
	if err != nil {
		return nil, err
	}
	return set, nil
}

// fetchBounded acquires the global fetch slot, then runs the fetch
// detached from the calling request's cancellation under its own
// deadline. Detaching matters because other verifiers join this call:
// if it inherited the leader's context, one client disconnecting would
// fail every request waiting on the same directory.
func (d *directoryResolver) fetchBounded(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Detach from cancellation, but never from a caller deadline that is
	// tighter than our own: dropping it would silently lengthen a bound
	// the caller asked for. WithoutCancel removes the disconnect
	// propagation; the deadline below keeps the time bound.
	timeout := defaultFetchTimeout
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining < timeout {
			timeout = remaining
		}
	}
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return d.fetch(fetchCtx, ref)
}

// fetchCoalesced runs (or joins) one in-flight fetch for the
// identifier, then updates the cache per the evidence rules.
func (d *directoryResolver) fetchCoalesced(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	d.mu.Lock()
	if call, ok := d.inflight[ref.identifier]; ok {
		d.mu.Unlock()
		select {
		case <-call.done:
			return call.set, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &fetchCall{done: make(chan struct{})}
	d.inflight[ref.identifier] = call
	d.mu.Unlock()

	set, err := d.fetchBounded(ctx, ref)
	call.set, call.err = set, err
	close(call.done)

	d.mu.Lock()
	delete(d.inflight, ref.identifier)
	d.mu.Unlock()

	if err != nil {
		// A failed resolution is not evidence (draft 6.10): keep any
		// positive entry (marking it stale-serving) and only write the
		// negative cache when nothing positive is held.
		if v, ok := d.pos.get(ref.identifier); ok {
			e := v.(*cacheEntry)
			e.stale = true
			d.pos.put(ref.identifier, e)
			return nil, err
		}
		d.neg.put(ref.identifier, &negativeEntry{reason: err.Error(), expiresAt: d.now().Add(d.negativeTTL)})
		return nil, err
	}
	// Success is newer evidence whatever it says: replace wholesale.
	d.neg.delete(ref.identifier)
	d.pos.put(ref.identifier, &cacheEntry{keys: set.keys, expiresAt: d.now().Add(set.ttl)})
	return set, nil
}

// fetch performs one guarded directory fetch.
func (d *directoryResolver) fetch(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	fctx, cancel := context.WithTimeout(ctx, defaultFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fctx, http.MethodGet, ref.fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", acceptDirectory)
	req.Header.Set("User-Agent", "gofastr-webbotauth")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("directory fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// draft 5.5: only 200 OK counts; every other status (redirects
		// included) is a discovery failure.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("directory fetch: status %d", resp.StatusCode)
	}
	if mt := mediaType(resp.Header.Get("Content-Type")); mt != "application/http-message-signatures-directory+json" && mt != "application/json" {
		return nil, fmt.Errorf("directory fetch: content type %q not acceptable", mt)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, d.maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("directory fetch: read body: %w", err)
	}
	if int64(len(body)) > d.maxBody {
		return nil, fmt.Errorf("directory fetch: body exceeds %d bytes", d.maxBody)
	}
	set, err := parseJWKS(body)
	if err != nil {
		return nil, fmt.Errorf("directory fetch: %w", err)
	}
	ttl, ok := responseTTL(resp.Header, d.now())
	if !ok {
		ttl = d.positiveTTL
	}
	return &jwkSet{keys: set.keys, ttl: ttl}, nil
}

func mediaType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// responseTTL derives the positive cache TTL from Cache-Control
// max-age or Expires-Date, clamped to [minPositiveTTL, maxPositiveTTL].
// ok is false when neither header is present.
func responseTTL(h http.Header, now time.Time) (time.Duration, bool) {
	if cc := h.Get("Cache-Control"); cc != "" {
		for _, part := range strings.Split(cc, ",") {
			part = strings.TrimSpace(part)
			if v, ok := strings.CutPrefix(part, "max-age="); ok {
				if secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && secs >= 0 {
					return clampTTL(time.Duration(secs) * time.Second), true
				}
			}
		}
	}
	if ex := h.Get("Expires"); ex != "" {
		if t, err := http.ParseTime(ex); err == nil {
			if d := t.Sub(now); d > 0 {
				return clampTTL(d), true
			}
			return minPositiveTTL, true
		}
	}
	return 0, false
}

func clampTTL(d time.Duration) time.Duration {
	if d < minPositiveTTL {
		return minPositiveTTL
	}
	if d > maxPositiveTTL {
		return maxPositiveTTL
	}
	return d
}
