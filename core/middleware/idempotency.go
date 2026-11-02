package middleware

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// IdempotencyKeyHeader is the request header clients use to assign a
// stable identity to a write. Two requests carrying the same value are
// the "same" request from the client's point of view; the middleware
// guarantees at-most-once side-effects within the store's retention
// window.
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyStore is the pluggable backend for cached responses.
// Implementations must be safe for concurrent use.
//
// Begin claims a key. The semantics are:
//
//   - replay non-nil, ok=true: a cached response already exists for this
//     key and fingerprint; the middleware should write replay back and
//     skip the downstream handler.
//   - replay nil, ok=false, err=nil: the caller is the first writer for
//     this key. It must call Finish exactly once with the captured
//     response. (Both shipped stores return ok=false here, and the
//     middleware's replay branch is `ok && replay != nil`, so ok=false is
//     the contract. This line used to say ok=true, which no implementation
//     and no caller has ever agreed with.)
//   - ok=false, err=ErrFingerprintMismatch: same key was used previously
//     with a different request fingerprint. The middleware responds 422.
//   - ok=false, err=ErrInFlight: another request with the same key is
//     currently executing. The middleware responds 409.
//   - any other err: storage failure; middleware fails closed (503)
//     unless IdempotencyConfig.FailOpen is true.
type IdempotencyStore interface {
	Begin(ctx context.Context, key, fingerprint string) (replay *IdempotentResponse, ok bool, err error)
	Finish(ctx context.Context, key, fingerprint string, resp *IdempotentResponse) error
}

// IdempotentResponse is the cached snapshot of a completed write.
type IdempotentResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

// Sentinel errors returned by IdempotencyStore.Begin.
var (
	ErrFingerprintMismatch = errors.New("idempotency: key reused with different request")
	ErrInFlight            = errors.New("idempotency: concurrent request in flight")
)

// IdempotencyConfig configures the idempotency middleware.
//
// Store defaults to an in-memory store with TTL. Set this to a redis-
// or db-backed implementation for multi-instance deployments.
//
// TTL controls how long completed responses are remembered. Default 24h
// (matches the Stripe/Square convention).
//
// MaxBodyBytes caps how much of the request body is read for fingerprint
// + replay capture. Defaults to 1 MiB. Larger requests bypass
// idempotency to keep memory bounded; they receive a Vary header
// indicating the bypass but otherwise proceed normally.
//
// MaxResponseBytes caps the size of the captured response body. When a
// successful handler writes more than this, the claim is released and
// the response goes through unchanged. Default 1 MiB.
//
// Methods restricts which HTTP methods participate. Defaults to POST,
// PUT, PATCH, DELETE. GET/HEAD/OPTIONS always bypass.
//
// Required, if true, rejects unsafe writes that don't carry the header
// (400). Default false; header is opt-in per request.
//
// Principal extracts the authenticated subject (user/tenant id) from
// each request. When set, the fingerprint is namespaced by the result
// so two principals using the SAME Idempotency-Key value never see
// each other's cached responses, closing a cross-tenant replay leak.
// Without one, or for a request it returns "" for, nothing is cached;
// return a constant such as "anon" to share one namespace on purpose.
//
// Tenant, when set, returns the tenant the request acts in ("" for
// none). A non-empty tenant joins the principal in the storage key, so
// one credential that switches its active tenant and resends the same
// key, path and body runs the write in the second tenant instead of
// being served the first tenant's response. The principal alone stays
// the row's owner for export and erasure.
//
// Scope, when set, returns what narrows the request below its
// principal's full authority ("" for nothing): a scope-restricted API
// token or embed grant. It joins the storage key the way Tenant does, so
// a narrow token of one user resending the key a session or a broader
// token used runs its own request instead of being served a response its
// scopes would not allow. framework.WithIdempotency sets it from the
// request's held scopes.
//
// FailOpen flips behaviour on store error: true falls through to the
// handler (availability-first), false returns 503 to the client
// (correctness-first). Default false; a broken store no longer
// silently allows duplicate writes.
//
// Logger records a Finish failure: a DB blip that strands the in-flight
// claim so later retries of the SAME request get 409 until the entry TTLs.
// The client already received its response (Finish runs after the handler),
// so the response is unaffected; this is an observability seam, not a
// control-flow change. Default slog.Default().
type IdempotencyConfig struct {
	Store            IdempotencyStore
	TTL              time.Duration
	MaxBodyBytes     int64
	MaxResponseBytes int64
	Methods          []string
	Required         bool
	Principal        func(r *http.Request) string
	Tenant           func(r *http.Request) string
	Scope            func(r *http.Request) string
	FailOpen         bool
	Logger           *slog.Logger
	// MaxStoreEntries caps how many entries the DEFAULT in-memory store
	// (Store == nil) retains; at the cap the store sheds idle entries
	// first (least recently used), in-flight claims last. 0 keeps the
	// default, 100_000 (the framework/ratelimit maxKeys precedent).
	// Ignored when Store is set — a custom store owns its own bounds.
	MaxStoreEntries int
	// MaxStoreEntryBytes caps the retained footprint of one cached
	// response (body + headers) in the default store; a larger response
	// is dropped at Finish and the key re-executes instead of replaying.
	// 0 keeps the default, 1 MiB. Ignored when Store is set.
	MaxStoreEntryBytes int64
}

// headersStrippedFromReplay are response headers the middleware never
// caches: they're per-request and/or per-identity and replaying them
// across requests would leak session/credential material.
var headersStrippedFromReplay = map[string]struct{}{
	"Set-Cookie":          {},
	"Cookie":              {},
	"Authorization":       {},
	"Proxy-Authorization": {},
	"Www-Authenticate":    {},
}

// Idempotency returns Middleware that honours the Idempotency-Key header
// on configured methods. See IdempotencyConfig for tuning.
//
// On a replay the middleware writes the cached status, headers, and
// body verbatim and adds Idempotent-Replay: true so the client can
// distinguish a replay from a fresh result.
//
// A negative TTL panics at construction (this constructor returns no
// error): a negative lifetime can only be a sign or unit error, and
// substituting the default would turn the caller's most restrictive
// input into the longest retention the middleware offers.
func Idempotency(cfg IdempotencyConfig) Middleware {
	if cfg.TTL < 0 {
		panic(fmt.Sprintf("idempotency: Idempotency: TTL must be >= 0 (got %v)", cfg.TTL))
	}
	if cfg.TTL == 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 1 << 20
	}
	if len(cfg.Methods) == 0 {
		cfg.Methods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	}
	if cfg.Store == nil {
		opts := []MemoryIdempotencyOption{
			WithMemoryStoreMaxEntries(cfg.MaxStoreEntries),
			WithMemoryStoreMaxEntryBytes(cfg.MaxStoreEntryBytes),
		}
		cfg.Store = NewMemoryIdempotencyStore(cfg.TTL, opts...)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	methods := map[string]bool{}
	for _, m := range cfg.Methods {
		methods[m] = true
	}
	// No Principal means one shared key namespace for every caller: two
	// users who pick the same Idempotency-Key value land on the same
	// entry, and the second is served the first's cached response BODY.
	// A UUID collision is unlikely, but a client library using a
	// request-scoped counter, a retry helper hashing the payload, or a
	// hand-written "order-1" are not.
	//
	// Degrade to a no-op rather than cache into a shared namespace: a
	// host that never wired a Principal then behaves exactly as if the
	// middleware were absent, which is the safe reading of "not
	// configured". Everything else in this file already fails closed:
	// FailOpen defaults false, credential headers are stripped from
	// replays. This default was the odd one out.
	if cfg.Principal == nil {
		logSlogWarnDefault("middleware: Idempotency has no Principal function — replay caching is DISABLED. " +
			"Set IdempotencyConfig.Principal (e.g. the authenticated user or tenant id) to enable it; " +
			"without one, two callers sharing an Idempotency-Key would receive each other's responses.")
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !methods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get(IdempotencyKeyHeader)
			if key == "" {
				if cfg.Required {
					i18n.HTTPError(w, r, http.StatusBadRequest, reasonIdemKeyMissing)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > 255 {
				i18n.HTTPError(w, r, http.StatusBadRequest, reasonIdemKeyTooLong)
				return
			}
			// A key is printable ASCII (RFC 9110 field-value bytes a
			// client mints; the IETF draft recommends a UUID). A byte
			// past 0x7e is not UTF-8 a Postgres text column accepts, so
			// it reached the store and answered 503 "store down" for a
			// client's malformed header.
			if !printableASCII(key) {
				i18n.HTTPError(w, r, http.StatusBadRequest, reasonIdemKeyInvalid)
				return
			}

			body, tooLarge, err := readBodyLimit(r, cfg.MaxBodyBytes)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonBodyUnreadable)
				return
			}
			if tooLarge {
				w.Header().Add("Vary", "Idempotency-Key") // Add, not Set: an upstream middleware (CORS) may already have written Vary
				w.Header().Set("Idempotent-Bypass", "body-too-large")
				original := r.Body
				r.Body = struct {
					io.Reader
					io.Closer
				}{
					Reader: io.MultiReader(bytes.NewReader(body), original),
					Closer: original,
				}
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			principal := ""
			if cfg.Principal != nil {
				principal = cfg.Principal(r)
			}
			// No subject means no namespace to keep this caller's entry
			// apart from another's: every such caller would share one,
			// and a colliding key replays one user's response to the
			// next. Serve it uncached, the same as no Principal at all.
			if principal == "" {
				next.ServeHTTP(w, r)
				return
			}
			fp := requestFingerprint(r, body, principal)
			// Namespace the storage key by principal too, binding the
			// key shard to "principal:key". The
			// byte-length prefix keeps the shard injective over
			// (principal, key) pairs: with a bare "\x00" separator,
			// ("a\x00b", "c") and ("a", "b\x00c") both collapse to
			// "a\x00b\x00c", and the second caller is wrongly refused
			// (422 fingerprint mismatch) on a key it is the first and
			// only user of — a cross-principal cache-poisoning DoS for
			// any Principal function that can return NUL-bearing
			// subjects.
			tenant := ""
			if cfg.Tenant != nil {
				tenant = cfg.Tenant(r)
			}
			scope := ""
			if cfg.Scope != nil {
				scope = cfg.Scope(r)
			}
			storeKey := idemStoreKey(principal, tenant, scope, key)

			replay, ok, beginErr := cfg.Store.Begin(r.Context(), storeKey, fp)
			switch {
			case errors.Is(beginErr, ErrFingerprintMismatch):
				i18n.HTTPError(w, r, http.StatusUnprocessableEntity, reasonIdemKeyReused)
				return
			case errors.Is(beginErr, ErrInFlight):
				w.Header().Set("Retry-After", "1")
				i18n.HTTPError(w, r, http.StatusConflict, reasonIdemInFlight)
				return
			case beginErr != nil:
				if cfg.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				i18n.HTTPError(w, r, http.StatusServiceUnavailable, reasonIdemStoreDown)
				return
			}
			if ok && replay != nil {
				writeReplay(w, replay)
				return
			}

			// Snapshot the set of header keys upstream middleware has
			// already written so the cache only stores headers the
			// handler itself adds.
			upstreamKeys := make(map[string]bool, len(w.Header()))
			for k := range w.Header() {
				upstreamKeys[k] = true
			}

			rec := &idempotencyRecorder{
				ResponseWriter: w,
				status:         http.StatusOK,
				headers:        w.Header(),
				upstreamKeys:   upstreamKeys,
				maxBody:        cfg.MaxResponseBytes,
			}
			// Finish persists (or releases) the claim AFTER the handler has
			// answered. A failure here strands the entry: the client already
			// got its response, but the same Idempotency-Key now 409s on
			// retry until the claim TTLs. The response is already sent, so the
			// only correct action is to make the loss observable; silently
			// dropping it is the bug this fixes.
			finish := func(resp *IdempotentResponse) {
				// Use a fresh context for the cleanup write so a client
				// disconnect doesn't strand the claim in-flight until the
				// 30-second TTL; that would block legitimate retries.
				finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := cfg.Store.Finish(finishCtx, storeKey, fp, resp); err != nil {
					cfg.Logger.Error("idempotency: Finish failed (claim stranded; retries of this key will 409 until TTL)",
						// The key is request-borne: scrub it the way every
						// slog sink does, so a forged key cannot paint a
						// forged line into the operator's tail.
						"key", textsafe.ScrubControlBytes(key), "error", err)
				}
			}
			// A panicking handler (http.ErrAbortHandler included) skips
			// the Finish below. Release the claim on the way out, or every
			// retry of the same request 409s "in flight" until the claim
			// TTLs although nothing is running; then let the panic go on
			// to Recovery or net/http.
			defer func() {
				if v := recover(); v != nil {
					finish(nil)
					panic(v)
				}
			}()
			next.ServeHTTP(rec, r)

			switch {
			case rec.bodyOverflow, rec.streamed:
				finish(nil)
			case rec.status >= 200 && rec.status < 300:
				snap := &IdempotentResponse{
					Status: rec.status,
					Header: rec.handlerHeaders(),
					Body:   rec.body.Bytes(),
				}
				finish(snap)
			default:
				finish(nil)
			}
		})
	}
}

// readBodyLimit drains up to limit+1 bytes from the request body. If the
// extra byte is consumed the body is "too large" and the caller should
// bypass idempotency rather than retain it.
func readBodyLimit(r *http.Request, limit int64) ([]byte, bool, error) {
	if r.Body == nil {
		return nil, false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, sentinelLimit(limit)))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return buf, true, nil
	}
	return buf, false, nil
}

// sentinelLimit is limit+1, the one extra byte that tells "exactly
// limit" from "more than limit". At math.MaxInt64 (no cap) the +1 would
// wrap negative and io.LimitReader would read nothing, so the cap stays
// put there: no body can be longer anyway.
func sentinelLimit(limit int64) int64 {
	if limit >= math.MaxInt64 {
		return limit
	}
	return limit + 1
}

// idemStoreKey is the key the store sees: the principal's byte length,
// NUL, the principal, the tenant and the scope the same way, then the
// client's key. The length prefixes keep it injective over (principal,
// tenant, scope, key).
func idemStoreKey(principal, tenant, scope, key string) string {
	return strconv.Itoa(len(principal)) + "\x00" + principal +
		strconv.Itoa(len(tenant)) + "\x00" + tenant +
		strconv.Itoa(len(scope)) + "\x00" + scope + key
}

// idemPrincipal reads the principal back out of an idemStoreKey key,
// so a store can file the row under the user it was cached for. A key
// in any other shape names nobody.
func idemPrincipal(storeKey string) string {
	n, rest, ok := strings.Cut(storeKey, "\x00")
	if !ok {
		return ""
	}
	size, err := strconv.Atoi(n)
	if err != nil || size < 0 || size > len(rest) {
		return ""
	}
	return rest[:size]
}

// requestFingerprint hashes the parts of the request that define
// "sameness" for idempotency: principal, method, path, query, content-
// type, and the body. Headers other than Content-Type are excluded;
// they vary with auth tokens, request IDs, etc., and aren't part of
// the client's intent.
//
// Including principal in the fingerprint closes the cross-tenant replay
// hole: two principals submitting the same body with the same key now
// hash differently, so each gets its own cached response.
//
// Every field is length-prefixed (uint32 big-endian), never
// separator-joined: a bare "\x00" separator is ambiguous when a field
// carries NUL — the path from %00, the raw body, a NUL-bearing
// principal — and two DIFFERENT field tuples fold onto the same
// digest (("a\x00b","c") and ("a","b\x00c" both hash
// "a\x00b\x00c")), so the second request replays the first's cached
// response under the same key. The length prefix keeps the field
// stream injective (gofastrcompositekey, the same shape the storage
// shard in Idempotency above already carries).
func requestFingerprint(r *http.Request, body []byte, principal string) string {
	h := sha256.New()
	writeField := func(field string) {
		var lenPrefix [4]byte
		binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(field)))
		h.Write(lenPrefix[:])
		h.Write([]byte(field))
	}
	writeField(principal)
	writeField(r.Method)
	// The escaped path, not the decoded one: /items/a%2Fb and /items/a/b
	// decode alike but route apart, so they are different requests.
	writeField(r.URL.EscapedPath())
	writeField(r.URL.RawQuery)
	writeField(r.Header.Get("Content-Type"))
	// The body is last and already bytes: prefix it without the
	// string() copy of what can be a MaxBodyBytes-sized slice.
	var bodyLen [4]byte
	binary.BigEndian.PutUint32(bodyLen[:], uint32(len(body)))
	h.Write(bodyLen[:])
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// writeReplay applies the cached response. Cached entries only carry
// headers the handler itself wrote and never include Set-Cookie,
// Authorization, or other identity-bearing headers.
func writeReplay(w http.ResponseWriter, replay *IdempotentResponse) {
	maps.Copy(w.Header(), replay.Header)
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(replay.Status)
	_, _ = w.Write(replay.Body)
}

// idempotencyRecorder captures status + body so the response can be
// cached. headers is the live http.Header from the upstream
// ResponseWriter; upstreamKeys is the set of keys already present
// before the handler ran, so handlerHeaders() can isolate the headers
// the handler itself set.
type idempotencyRecorder struct {
	http.ResponseWriter
	status       int
	headers      http.Header
	upstreamKeys map[string]bool
	body         bytes.Buffer
	maxBody      int64
	bodyOverflow bool
	wroteHeader  bool
	// streamed marks a response the handler flushed or hijacked: what a
	// replay would send is not what the client got, so none is kept.
	streamed bool
}

// FlushError passes a flush through and marks the response streamed.
func (r *idempotencyRecorder) FlushError() error {
	r.streamed = true
	return http.NewResponseController(r.ResponseWriter).Flush()
}

// Flush is FlushError for handlers that assert http.Flusher.
func (r *idempotencyRecorder) Flush() { _ = r.FlushError() }

// Hijack hands the connection to the handler; nothing is kept for replay.
func (r *idempotencyRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.streamed = true
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

// Unwrap lets http.ResponseController reach the connection's deadlines;
// Flush and Hijack stop at the methods above.
func (r *idempotencyRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *idempotencyRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	// An informational status (103 Early Hints) comes before the final
	// one: pass it on and keep waiting. Latching it dropped the final
	// status, so the client saw 200 and nothing was stored to replay.
	// 101 is final: the connection changes protocol.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(status)
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *idempotencyRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if !r.bodyOverflow {
		if int64(r.body.Len())+int64(len(p)) > r.maxBody {
			r.bodyOverflow = true
			r.body.Reset()
		} else {
			r.body.Write(p)
		}
	}
	return r.ResponseWriter.Write(p)
}

// handlerHeaders returns a fresh http.Header containing only the
// entries the handler added during the recorded request, minus the
// per-identity headers we never want to replay.
func (r *idempotencyRecorder) handlerHeaders() http.Header {
	out := make(http.Header, len(r.headers))
	for k, vs := range r.headers {
		if r.upstreamKeys[k] {
			continue
		}
		if _, stripped := headersStrippedFromReplay[http.CanonicalHeaderKey(k)]; stripped {
			continue
		}
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// defaultMaxIdemEntries caps how many entries the in-process store retains
// at once. The store key is client-chosen (the Idempotency-Key header, no
// auth required), so without a cap a flood of unique keys grows the map
// until OOM — the same shape framework/ratelimit's maxKeys guards. The cap
// is far above any legitimate concurrent-key count and eviction is
// idle-first, so a flood can only evict flood.
const defaultMaxIdemEntries = 100_000

// defaultMaxIdemEntryBytes caps the retained footprint of ONE cached
// response (body + headers). It re-bounds the store even when a host
// raises the middleware-level MaxResponseBytes, and matches that default.
const defaultMaxIdemEntryBytes = 1 << 20

// memoryIdempotencyStore is the default in-process IdempotencyStore.
// Entries expire after TTL; in-flight claims expire faster (30s) so a
// crashed handler doesn't lock out retries forever. The store is bounded
// by default: at most maxEntries entries (idle-first eviction when the
// cap is hit) and at most maxEntryBytes retained per entry (an oversized
// response is dropped rather than cached), so a flood of unique keys
// cannot exhaust process memory.
type memoryIdempotencyStore struct {
	ttl           time.Duration
	inFlightTTL   time.Duration
	maxEntries    int
	maxEntryBytes int
	mu            sync.Mutex
	entries       map[string]*idemEntry
	lastReap      time.Time
}

type idemEntry struct {
	fingerprint string
	resp        *IdempotentResponse // nil while in-flight
	expires     time.Time
	lastAccess  time.Time // idle-first eviction rank: touched by Begin hits and Finish
}

// MemoryIdempotencyOption configures the in-process store.
type MemoryIdempotencyOption func(*memoryIdempotencyStore)

// WithMemoryStoreMaxEntries caps the number of resident entries. When the
// cap is hit the store sheds idle entries first (least recently used),
// down to a low-water mark so the shed runs at most once per ~10% of the
// cap rather than on every insert; in-flight claims are evicted only when
// every entry is in-flight. Defaults to 100_000 (the framework/ratelimit
// maxKeys precedent); n <= 0 keeps the default.
func WithMemoryStoreMaxEntries(n int) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// WithMemoryStoreMaxEntryBytes caps the retained footprint of one cached
// response (body + headers). A response larger than the cap is not
// retained: the entry is dropped at Finish, the client still received the
// live response, and the next request with the same key re-executes
// rather than replays. Defaults to 1 MiB (matching MaxResponseBytes);
// n <= 0 keeps the default.
func WithMemoryStoreMaxEntryBytes(n int64) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if n > 0 {
			s.maxEntryBytes = int(n)
		}
	}
}

// NewMemoryIdempotencyStore returns an in-process IdempotencyStore.
// Suitable for single-instance deployments and tests. Use a Redis- or
// DB-backed implementation behind the same interface for clusters.
// The store is bounded by default (defaultMaxIdemEntries entries,
// defaultMaxIdemEntryBytes per entry); see the options to change either.
//
// ttl 0 keeps the 24h default. A negative ttl panics: it can only be a
// sign or unit error, and substituting the default would turn the
// caller's most restrictive input into the longest retention the store
// offers (mirroring MemorySessionStore.Create's fail-closed posture).
func NewMemoryIdempotencyStore(ttl time.Duration, opts ...MemoryIdempotencyOption) IdempotencyStore {
	if ttl < 0 {
		panic(fmt.Sprintf("idempotency: NewMemoryIdempotencyStore: ttl must be >= 0 (got %v)", ttl))
	}
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	s := &memoryIdempotencyStore{
		ttl:           ttl,
		inFlightTTL:   30 * time.Second,
		maxEntries:    defaultMaxIdemEntries,
		maxEntryBytes: defaultMaxIdemEntryBytes,
		entries:       map[string]*idemEntry{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *memoryIdempotencyStore) Begin(_ context.Context, key, fingerprint string) (*IdempotentResponse, bool, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[key]; ok && now.Before(e.expires) {
		if e.fingerprint != fingerprint {
			return nil, false, ErrFingerprintMismatch
		}
		if e.resp == nil {
			return nil, false, ErrInFlight
		}
		e.lastAccess = now
		return e.resp, true, nil
	}

	// Shed idle entries when at capacity, BEFORE inserting the new claim,
	// so the map is bounded under a unique-key flood. maxEntries == 0
	// (a hand-built store literal, never the New constructor) means
	// unlimited and skips the shed.
	if s.maxEntries > 0 && len(s.entries) >= s.maxEntries {
		s.evictLocked()
	}
	s.entries[key] = &idemEntry{
		fingerprint: fingerprint,
		expires:     now.Add(s.inFlightTTL),
		lastAccess:  now,
	}
	return nil, false, nil
}

// evictLocked sheds entries down to a low-water mark (90% of the cap) so
// the O(n) scan runs at most once per ~10% of the cap rather than on every
// insert under sustained flood — the framework/ratelimit evictLocked
// precedent. The victim order is idle-first:
//
//   - completed entries (resp != nil) are shed before in-flight claims;
//     dropping an in-flight claim opens a concurrent-execution window for
//     that key, so claims are only shed when EVERY entry is in-flight;
//   - within each class, the least recently accessed entry goes first, so
//     a flood of fresh keys can only evict flood — a key that is being
//     replayed stays warm.
func (s *memoryIdempotencyStore) evictLocked() {
	lowWater := s.maxEntries * 9 / 10
	type victim struct {
		key        string
		inFlight   bool
		lastAccess time.Time
	}
	ranked := make([]victim, 0, len(s.entries))
	for k, e := range s.entries {
		ranked = append(ranked, victim{key: k, inFlight: e.resp == nil, lastAccess: e.lastAccess})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].inFlight != ranked[j].inFlight {
			return !ranked[i].inFlight
		}
		if !ranked[i].lastAccess.Equal(ranked[j].lastAccess) {
			return ranked[i].lastAccess.Before(ranked[j].lastAccess)
		}
		return ranked[i].key < ranked[j].key // deterministic tie-break under one clock tick
	})
	for i := 0; i < len(ranked) && len(s.entries) > lowWater; i++ {
		delete(s.entries, ranked[i].key)
	}
}

func (s *memoryIdempotencyStore) Finish(_ context.Context, key, fingerprint string, resp *IdempotentResponse) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return nil
	}
	// Only the owner of the in-flight claim may mutate or release it. If the
	// claim expired and was re-assigned to a different fingerprint while this
	// handler was still running, Finish must be a no-op: writing would clobber
	// the new owner's claim and replay this caller's response to them on retry,
	// a cross-user disclosure. See TestIdemFinishOnlyWritesOwnClaim.
	if e.fingerprint != fingerprint {
		return nil
	}
	if resp == nil {
		delete(s.entries, key)
		return nil
	}
	// Per-entry byte cap: an oversized response is dropped, not retained.
	// The client already received the live response; the cost of dropping
	// is that the next request with this key re-executes instead of
	// replaying — availability over memory, the same posture as the
	// middleware-level MaxResponseBytes bypass.
	if responseBytes(resp) > s.maxEntryBytes {
		delete(s.entries, key)
		return nil
	}
	e.resp = resp
	e.expires = now.Add(s.ttl)
	e.lastAccess = now
	return nil
}

// responseBytes returns the retained footprint of a cached response:
// body plus header keys and values.
func responseBytes(resp *IdempotentResponse) int {
	n := len(resp.Body)
	for k, vs := range resp.Header {
		n += len(k)
		for _, v := range vs {
			n += len(v)
		}
	}
	return n
}

func (s *memoryIdempotencyStore) reapLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
}

// printableASCII reports whether s holds only bytes 0x21-0x7e and
// spaces, the bytes a client-minted Idempotency-Key is made of.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
