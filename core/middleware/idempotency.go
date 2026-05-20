package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
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
//   - replay nil, ok=true: the caller is the first writer for this key.
//     It must call Finish exactly once with the captured response.
//   - ok=false, err=ErrFingerprintMismatch: same key was used previously
//     with a different request fingerprint. The middleware responds 422.
//   - ok=false, err=ErrInFlight: another request with the same key is
//     currently executing. The middleware responds 409.
//   - any other err: storage failure; middleware fails closed (503)
//     unless IdempotencyConfig.FailOpen is true.
type IdempotencyStore interface {
	Begin(ctx context.Context, key, fingerprint string) (replay *IdempotentResponse, ok bool, err error)
	Finish(ctx context.Context, key string, resp *IdempotentResponse) error
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
// idempotency to keep memory bounded — they receive a Vary header
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
// (400). Default false — header is opt-in per request.
//
// Principal extracts the authenticated subject (user/tenant id) from
// each request. When set, the fingerprint is namespaced by the result
// so two principals using the SAME Idempotency-Key value never see
// each other's cached responses — closing a cross-tenant replay leak.
// Default: empty principal (no namespacing); apps SHOULD wire one.
//
// FailOpen flips behaviour on store error: true falls through to the
// handler (availability-first), false returns 503 to the client
// (correctness-first). Default false — a broken store no longer
// silently allows duplicate writes.
type IdempotencyConfig struct {
	Store            IdempotencyStore
	TTL              time.Duration
	MaxBodyBytes     int64
	MaxResponseBytes int64
	Methods          []string
	Required         bool
	Principal        func(r *http.Request) string
	FailOpen         bool
}

// headersStrippedFromReplay are response headers the middleware never
// caches — they're per-request and/or per-identity and replaying them
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
func Idempotency(cfg IdempotencyConfig) Middleware {
	if cfg.TTL <= 0 {
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
		cfg.Store = NewMemoryIdempotencyStore(cfg.TTL)
	}
	methods := map[string]bool{}
	for _, m := range cfg.Methods {
		methods[m] = true
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
					http.Error(w, "missing Idempotency-Key header", http.StatusBadRequest)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > 255 {
				http.Error(w, "Idempotency-Key too long", http.StatusBadRequest)
				return
			}

			body, tooLarge, err := readBodyLimit(r, cfg.MaxBodyBytes)
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			if tooLarge {
				w.Header().Set("Vary", "Idempotency-Key")
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
			fp := requestFingerprint(r, body, principal)
			// Namespace the storage key by principal too — that defends
			// even when the Principal function returns empty for some
			// callers, by binding the key shard to "principal:key".
			storeKey := principal + "\x00" + key

			replay, ok, beginErr := cfg.Store.Begin(r.Context(), storeKey, fp)
			switch {
			case errors.Is(beginErr, ErrFingerprintMismatch):
				http.Error(w, "Idempotency-Key reused with different request", http.StatusUnprocessableEntity)
				return
			case errors.Is(beginErr, ErrInFlight):
				w.Header().Set("Retry-After", "1")
				http.Error(w, "concurrent request for this Idempotency-Key", http.StatusConflict)
				return
			case beginErr != nil:
				if cfg.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "idempotency store unavailable", http.StatusServiceUnavailable)
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
			next.ServeHTTP(rec, r)

			// Use a fresh context for the cleanup write so a client
			// disconnect doesn't strand the claim in-flight until the
			// 30-second TTL — that would block legitimate retries.
			finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			switch {
			case rec.bodyOverflow:
				_ = cfg.Store.Finish(finishCtx, storeKey, nil)
			case rec.status >= 200 && rec.status < 300:
				snap := &IdempotentResponse{
					Status: rec.status,
					Header: rec.handlerHeaders(),
					Body:   rec.body.Bytes(),
				}
				_ = cfg.Store.Finish(finishCtx, storeKey, snap)
			default:
				_ = cfg.Store.Finish(finishCtx, storeKey, nil)
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
	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return buf, true, nil
	}
	return buf, false, nil
}

// requestFingerprint hashes the parts of the request that define
// "sameness" for idempotency: principal, method, path, query, content-
// type, and the body. Headers other than Content-Type are excluded —
// they vary with auth tokens, request IDs, etc., and aren't part of the
// client's intent.
//
// Including principal in the fingerprint closes the cross-tenant replay
// hole: two principals submitting the same body with the same key now
// hash differently, so each gets its own cached response.
func requestFingerprint(r *http.Request, body []byte, principal string) string {
	h := sha256.New()
	h.Write([]byte(principal))
	h.Write([]byte{0})
	h.Write([]byte(r.Method))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.Path))
	h.Write([]byte{0})
	h.Write([]byte(r.URL.RawQuery))
	h.Write([]byte{0})
	h.Write([]byte(r.Header.Get("Content-Type")))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// writeReplay applies the cached response. Cached entries only carry
// headers the handler itself wrote and never include Set-Cookie,
// Authorization, or other identity-bearing headers.
func writeReplay(w http.ResponseWriter, replay *IdempotentResponse) {
	for k, vs := range replay.Header {
		w.Header()[k] = vs
	}
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
}

func (r *idempotencyRecorder) WriteHeader(status int) {
	if r.wroteHeader {
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

// memoryIdempotencyStore is the default in-process IdempotencyStore.
// Entries expire after TTL; in-flight claims expire faster (30s) so a
// crashed handler doesn't lock out retries forever. An optional
// maxEntries cap evicts the oldest entries when full so a flood of
// unique keys cannot exhaust process memory.
type memoryIdempotencyStore struct {
	ttl         time.Duration
	inFlightTTL time.Duration
	maxEntries  int // 0 = unlimited
	mu          sync.Mutex
	entries     map[string]*idemEntry
	lastReap    time.Time
}

type idemEntry struct {
	fingerprint string
	resp        *IdempotentResponse // nil while in-flight
	expires     time.Time
	createdAt   time.Time // for LRU-by-creation eviction when maxEntries is set
}

// MemoryIdempotencyOption configures the in-process store.
type MemoryIdempotencyOption func(*memoryIdempotencyStore)

// WithMemoryStoreMaxEntries caps the number of resident entries. When
// the cap is hit, the oldest entry (by creation time) is evicted to
// make room. Default is unlimited; set this when accepting traffic
// from anywhere a single attacker could submit unique keys forever.
func WithMemoryStoreMaxEntries(n int) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// NewMemoryIdempotencyStore returns an in-process IdempotencyStore.
// Suitable for single-instance deployments and tests. Use a Redis- or
// DB-backed implementation behind the same interface for clusters.
func NewMemoryIdempotencyStore(ttl time.Duration, opts ...MemoryIdempotencyOption) IdempotencyStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	s := &memoryIdempotencyStore{
		ttl:         ttl,
		inFlightTTL: 30 * time.Second,
		entries:     map[string]*idemEntry{},
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

	if now.Sub(s.lastReap) > time.Minute {
		s.reapLocked(now)
		s.lastReap = now
	}

	if e, ok := s.entries[key]; ok && now.Before(e.expires) {
		if e.fingerprint != fingerprint {
			return nil, false, ErrFingerprintMismatch
		}
		if e.resp == nil {
			return nil, false, ErrInFlight
		}
		return e.resp, true, nil
	}

	// Evict oldest when at capacity. Comparing createdAt across the
	// map is O(n) but n is bounded by maxEntries and this only fires
	// when at the cap.
	if s.maxEntries > 0 && len(s.entries) >= s.maxEntries {
		var oldestKey string
		var oldestAt time.Time
		for k, e := range s.entries {
			if oldestKey == "" || e.createdAt.Before(oldestAt) {
				oldestKey = k
				oldestAt = e.createdAt
			}
		}
		if oldestKey != "" {
			delete(s.entries, oldestKey)
		}
	}
	s.entries[key] = &idemEntry{
		fingerprint: fingerprint,
		expires:     now.Add(s.inFlightTTL),
		createdAt:   now,
	}
	return nil, false, nil
}

func (s *memoryIdempotencyStore) Finish(_ context.Context, key string, resp *IdempotentResponse) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return nil
	}
	if resp == nil {
		delete(s.entries, key)
		return nil
	}
	e.resp = resp
	e.expires = now.Add(s.ttl)
	return nil
}

func (s *memoryIdempotencyStore) reapLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
}
