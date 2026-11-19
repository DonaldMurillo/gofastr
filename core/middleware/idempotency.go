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

// IdempotencyKeyHeader is the request header that names a write, so a retry with the same value is answered from the cache.
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyStore holds claims and cached responses. Begin returns a replay with ok true, nil and ok false for a fresh claim the caller must Finish once, ErrFingerprintMismatch, ErrInFlight, or another error for a store failure.
type IdempotencyStore interface {
	Begin(ctx context.Context, key, fingerprint string) (replay *IdempotentResponse, ok bool, err error)
	Finish(ctx context.Context, key, fingerprint string, resp *IdempotentResponse) error
}

// InFlightTTLStore is implemented by a store that reports how long an in-flight claim holds its key.
type InFlightTTLStore interface {
	InFlightTTL() time.Duration
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

// IdempotencyConfig configures the idempotency middleware; a request whose Principal is "" (or with no Principal set) is served uncached, and Tenant and Scope join the principal in the storage key.
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
	// MaxStoreEntries caps the default memory store's entries; 0 means 100_000. Ignored when Store is set.
	MaxStoreEntries int
	// MaxStoreEntryBytes caps one cached response in the default memory store; 0 means 1 MiB. Ignored when Store is set.
	MaxStoreEntryBytes int64
}

var headersStrippedFromReplay = map[string]struct{}{
	"Set-Cookie":          {},
	"Cookie":              {},
	"Authorization":       {},
	"Proxy-Authorization": {},
	"Www-Authenticate":    {},
}

// Idempotency returns middleware that replays the cached 2xx response, with Idempotent-Replay: true, for a repeated Idempotency-Key; a negative TTL panics.
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
				w.Header().Add("Vary", "Idempotency-Key")
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
			if principal == "" {
				next.ServeHTTP(w, r)
				return
			}
			fp := requestFingerprint(r, body, principal)
			tenant := ""
			if cfg.Tenant != nil {
				tenant = cfg.Tenant(r)
			}
			scope := ""
			if cfg.Scope != nil {
				scope = cfg.Scope(r)
			}
			storeKey := idemStoreKey(principal, tenant, scope, key)

			//gofastr:allow(textbind) the store key's NUL bytes are escaped injectively by pgKey at bind time (Postgres) and invalid UTF-8 is refused by columnPrincipal before any write; SQLite stores raw bytes, so no half of the check applies there
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
			finish := func(resp *IdempotentResponse) {
				finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				//gofastr:allow(textbind) Finish binds the same pgKey-escaped column Begin claimed under; the key was vetted by columnPrincipal on the Begin that must precede it, and NUL keys are stored, not refused (the round-trip contract)
				if err := cfg.Store.Finish(finishCtx, storeKey, fp, resp); err != nil {
					cfg.Logger.Error("idempotency: Finish failed (claim stranded; retries of this key will 409 until TTL)",
						"key", textsafe.ScrubControlBytes(key), "error", err)
				}
			}
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
			case !rec.wroteHeader && r.Context().Err() != nil:
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

func sentinelLimit(limit int64) int64 {
	if limit >= math.MaxInt64 {
		return limit
	}
	return limit + 1
}

func idemStoreKey(principal, tenant, scope, key string) string {
	return strconv.Itoa(len(principal)) + "\x00" + principal +
		strconv.Itoa(len(tenant)) + "\x00" + tenant +
		strconv.Itoa(len(scope)) + "\x00" + scope + key
}

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
	writeField(r.URL.EscapedPath())
	writeField(r.URL.RawQuery)
	writeField(r.Header.Get("Content-Type"))
	var bodyLen [4]byte
	binary.BigEndian.PutUint32(bodyLen[:], uint32(len(body)))
	h.Write(bodyLen[:])
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func writeReplay(w http.ResponseWriter, replay *IdempotentResponse) {
	maps.Copy(w.Header(), replay.Header)
	w.Header().Set("Idempotent-Replay", "true")
	//gofastr:allow(statusrange) replays the status this middleware recorded, kept by the store only for a 2xx response
	w.WriteHeader(replay.Status)
	_, _ = w.Write(replay.Body)
}

type idempotencyRecorder struct {
	http.ResponseWriter
	status       int
	headers      http.Header
	upstreamKeys map[string]bool
	body         bytes.Buffer
	maxBody      int64
	bodyOverflow bool
	wroteHeader  bool
	streamed     bool
}

func (r *idempotencyRecorder) FlushError() error {
	r.streamed = true
	return http.NewResponseController(r.ResponseWriter).Flush()
}

func (r *idempotencyRecorder) Flush() { _ = r.FlushError() }

func (r *idempotencyRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.streamed = true
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

func (r *idempotencyRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *idempotencyRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
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

const defaultMaxIdemEntries = 100_000

const defaultMaxIdemEntryBytes = 1 << 20

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
	resp        *IdempotentResponse
	expires     time.Time
	lastAccess  time.Time
}

// MemoryIdempotencyOption configures the in-process store.
type MemoryIdempotencyOption func(*memoryIdempotencyStore)

// WithMemoryStoreMaxEntries caps the entries the store holds, shedding completed and least recently used entries first; n <= 0 keeps 100_000.
func WithMemoryStoreMaxEntries(n int) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// WithMemoryIdempotencyInFlightTTL sets how long an in-flight claim holds its key; d <= 0 keeps 30s.
func WithMemoryIdempotencyInFlightTTL(d time.Duration) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if d > 0 {
			s.inFlightTTL = d
		}
	}
}

// InFlightTTL implements InFlightTTLStore.
func (s *memoryIdempotencyStore) InFlightTTL() time.Duration { return s.inFlightTTL }

// WithMemoryStoreMaxEntryBytes caps one cached response; a larger one is not kept. n <= 0 keeps 1 MiB.
func WithMemoryStoreMaxEntryBytes(n int64) MemoryIdempotencyOption {
	return func(s *memoryIdempotencyStore) {
		if n > 0 {
			s.maxEntryBytes = int(n)
		}
	}
}

// NewMemoryIdempotencyStore returns an in-process, bounded IdempotencyStore; ttl 0 means 24h and a negative ttl panics.
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

	if now.Sub(s.lastReap) > idemReapEvery {
		s.reapLocked(now)
		s.lastReap = now
	}

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
		return ranked[i].key < ranked[j].key
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
	if e.fingerprint != fingerprint {
		return nil
	}
	if resp == nil {
		delete(s.entries, key)
		return nil
	}
	if responseBytes(resp) > s.maxEntryBytes {
		delete(s.entries, key)
		return nil
	}
	e.resp = resp
	e.expires = now.Add(s.ttl)
	e.lastAccess = now
	return nil
}

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

const idemReapEvery = time.Minute

func (s *memoryIdempotencyStore) reapLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
