package webbotauth

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

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
	dialTimeout              = 3 * time.Second
	maxConcurrentFetches     = 8               // across all identifiers
	maxConcurrentLookups     = 8               // across all hostnames
	maxFetchesPerSite        = 2               // per registrable domain
	fetchSlotWait            = 2 * time.Second // wait for a free fetch slot
	acceptDirectory          = "application/http-message-signatures-directory+json, application/json;q=0.9"
)

type discoveryType string

const (
	discoveryDirectory discoveryType = "directory"
	discoveryJWKS      discoveryType = "jwks_uri"
)

type agentRef struct {
	identifier string        // cache key: the URL the verifier resolves
	fetchURL   string        // what is actually fetched
	typ        discoveryType // how the URL was derived
}

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
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("directory-type signature-agent must be an origin, got path %q", u.Path)
		}
		if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return nil, fmt.Errorf("directory-type signature-agent must not carry a query or fragment")
		}
		wk := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: directoryWellKnown}
		return &agentRef{identifier: wk.String(), fetchURL: wk.String(), typ: typ}, nil
	case discoveryJWKS:
		id := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
		fetch := &url.URL{Scheme: "https", Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
		return &agentRef{identifier: id.String(), fetchURL: fetch.String(), typ: typ}, nil
	default:
		return nil, fmt.Errorf("unsupported signature-agent discovery type %q", typ)
	}
}

var dnsSem = make(chan struct{}, maxConcurrentLookups)

// ErrResolverBusy reports that discovery was not attempted for lack of a
// lookup or fetch slot; it says nothing about the signature.
var ErrResolverBusy = errors.New("webbotauth: resolver busy")

func checkHostPublic(ctx context.Context, host string) error {
	if netguard.IsInternalName(host) {
		return fmt.Errorf("webbotauth: host %q targets internal infrastructure", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := netguard.Reason(ip); reason != "" {
			return fmt.Errorf("webbotauth: %s %s not allowed", reason, ip)
		}
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	defer cancel()
	select {
	case dnsSem <- struct{}{}:
		defer func() { <-dnsSem }()
	case <-lookupCtx.Done():
		return fmt.Errorf("host %q lookup not attempted: %w", host, ErrResolverBusy)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
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

func guardedTransport(allowPrivate bool) *http.Transport {
	tr := netguard.GuardedTransport(allowPrivate, "webbotauth")
	dial := tr.DialContext
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		return dial(ctx, network, addr)
	}
	tr.TLSHandshakeTimeout = 3 * time.Second
	tr.ResponseHeaderTimeout = 4 * time.Second
	return tr
}

type cacheEntry struct {
	keys      []jwk
	expiresAt time.Time
	retryAt   time.Time
}

type negativeEntry struct {
	reason    string
	expiresAt time.Time
}

type lruShard struct {
	max   int
	mu    sync.Mutex
	keys  map[string]*list.Element
	order *list.List
}

func newLRUShard(max int) *lruShard {
	return &lruShard{max: max, keys: make(map[string]*list.Element, max), order: list.New()}
}

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

	sem         chan struct{}
	slotWait    time.Duration
	siteFetches map[string]int
	perSite     int
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
		slotWait:    fetchSlotWait,
		siteFetches: make(map[string]int),
		perSite:     maxFetchesPerSite,
	}
}

func (d *directoryResolver) resolve(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	now := d.now()
	if v, ok := d.pos.get(ref.identifier); ok {
		e := v.(*cacheEntry)
		if now.Before(e.expiresAt) || now.Before(e.retryAt) {
			return &jwkSet{keys: e.keys}, nil
		}
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

func fetchSite(fetchURL string) string {
	u, err := url.Parse(fetchURL)
	if err != nil {
		return fetchURL
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil {
		return host
	}
	if site, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return site
	}
	return host
}

func (d *directoryResolver) fetchBounded(ctx context.Context, ref *agentRef) (*jwkSet, error) {
	site := fetchSite(ref.fetchURL)
	d.mu.Lock()
	if d.siteFetches[site] >= d.perSite {
		d.mu.Unlock()
		return nil, fmt.Errorf("directory fetch for %q not attempted, %d already in flight: %w", site, d.perSite, ErrResolverBusy)
	}
	d.siteFetches[site]++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		if d.siteFetches[site]--; d.siteFetches[site] <= 0 {
			delete(d.siteFetches, site)
		}
		d.mu.Unlock()
	}()
	wait := time.NewTimer(d.slotWait)
	defer wait.Stop()
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-wait.C:
		return nil, fmt.Errorf("directory fetch for %q not attempted: %w", site, ErrResolverBusy)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
		if v, ok := d.pos.get(ref.identifier); ok {
			e := v.(*cacheEntry)
			if ctx.Err() == nil {
				d.pos.put(ref.identifier, &cacheEntry{keys: e.keys, expiresAt: e.expiresAt, retryAt: d.now().Add(d.negativeTTL)})
			}
			return nil, err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrResolverBusy) {
			return nil, err
		}
		d.neg.put(ref.identifier, &negativeEntry{reason: err.Error(), expiresAt: d.now().Add(d.negativeTTL)})
		return nil, err
	}
	d.neg.delete(ref.identifier)
	d.pos.put(ref.identifier, &cacheEntry{keys: set.keys, expiresAt: d.now().Add(set.ttl)})
	return set, nil
}

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

func responseTTL(h http.Header, now time.Time) (time.Duration, bool) {
	if cc := strings.Join(h.Values("Cache-Control"), ","); cc != "" {
		for _, part := range strings.Split(cc, ",") {
			part = strings.TrimSpace(part)
			if v, ok := strings.CutPrefix(part, "max-age="); ok {
				if secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && secs >= 0 {
					if secs > int64(maxPositiveTTL/time.Second) {
						return maxPositiveTTL, true
					}
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
