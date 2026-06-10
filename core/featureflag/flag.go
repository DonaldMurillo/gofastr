// Package featureflag is a minimal feature-flag primitive for GoFastr apps.
//
// The package name avoids collision with the standard library's `flag`;
// users who want a shorter local name can alias on import:
//
//	import flag "github.com/DonaldMurillo/gofastr/core/featureflag"
//
// The model is intentionally small: a flag has a global on/off switch,
// an optional percentage rollout, and explicit user/tenant allow lists.
// Rules are evaluated against an EvalContext that you build from the
// request (user, tenant, env, attributes).
//
// Anything more elaborate — variants with weights, dependent flags,
// scheduled ramps — should be a separate evaluator. This package's
// only job is to make "is feature X on for this caller right now"
// trivial to express in handler code.
//
// Wiring:
//
//	store := featureflag.NewMemoryStore()
//	store.Set(featureflag.Flag{Key: "new-checkout", Enabled: true, Rollout: 25})
//	featureflag.SetDefault(featureflag.NewEvaluator(store))
//
//	// later, in a handler
//	if featureflag.Bool(ctx, "new-checkout") {
//	    return newCheckout(w, r)
//	}
//	return oldCheckout(w, r)
//
// Concurrency: the bundled memory store is safe for concurrent reads
// and writes. Evaluator and the package-level helpers are safe for
// concurrent use as long as the underlying store is.
package featureflag

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
)

// Flag is the rule definition stored for a key.
//
// Enabled is the global kill switch; when false, Bool always returns
// false regardless of the other fields.
//
// Rollout is a percentage (0–100) of subjects that see the flag as on,
// computed from a stable hash of the flag key and subject id. Set 0
// for "off-by-default" and 100 to enable everywhere the kill switch
// allows.
//
// Users and Tenants are explicit allow lists. A subject matching
// either list short-circuits to true (regardless of rollout), so flags
// can be force-enabled for testers and beta tenants.
//
// Envs restricts the flag to specific deployment environments. When
// non-empty, the EvalContext's Env must match one of the listed values
// or the flag evaluates to false — even for explicitly allow-listed
// users. When empty, no environment restriction applies. The match is
// case-sensitive string equality.
type Flag struct {
	Key     string
	Enabled bool
	Rollout int
	Users   []string
	Tenants []string
	Envs    []string
}

// EvalContext is the per-call subject identity used to evaluate a flag.
// Build one from the request — typically the authenticated user id and
// the resolved tenant id.
//
// The Env field is matched by string equality against any rule that
// references it; this is how "dev only" or "staging only" flags work
// without coupling the flag definition to deployment plumbing.
//
// Attrs is open-ended additional context (region, plan tier, etc.).
// The default evaluator doesn't consult Attrs, but a custom Store /
// Evaluator wrapper can. Future versions of this package may grow
// attribute-rule matching; storing them now keeps the migration cheap.
type EvalContext struct {
	UserID   string
	TenantID string
	Env      string
	Attrs    map[string]string
}

// Store is the pluggable backend that holds flag definitions.
// Implementations must be safe for concurrent use.
//
// Get returns (nil, nil) — not an error — when the key isn't defined.
// Evaluator treats that as "fall through to the supplied default."
type Store interface {
	Get(ctx context.Context, key string) (*Flag, error)
}

// MutableStore extends Store with write operations. The memory store
// implements it; persistent stores (db, redis) typically should too so
// admin tools can edit flags at runtime.
type MutableStore interface {
	Store
	Set(f Flag) error
	Delete(key string) error
}

// Evaluator is the entry point for runtime checks.
type Evaluator struct {
	store Store
	// salt is folded into the rollout bucket hash. A non-empty salt
	// makes the bucket assignment unpredictable to an adversary who
	// can only see flag keys — without it, a 1% rollout can be
	// brute-forced by trying ~100 distinct subject ids.
	salt string
}

// NewEvaluator wraps a Store. Passing nil yields an evaluator that
// answers every question with the caller's default — useful for tests
// that don't want to wire a store but still call featureflag.Bool.
func NewEvaluator(s Store) *Evaluator {
	return &Evaluator{store: s}
}

// NewEvaluatorWithSalt wraps a Store with a process-private salt mixed
// into the rollout bucket hash. Set this when the rollout cohort needs
// to be unpredictable to attackers (kill switches whose evasion would
// re-enable a payments flow, etc.) — without a salt, FNV-1a buckets
// are deterministic from public flag keys.
//
// The salt is process-private by convention; rotating it shuffles
// every cohort so use only when that's acceptable.
func NewEvaluatorWithSalt(s Store, salt string) *Evaluator {
	return &Evaluator{store: s, salt: salt}
}

// Bool returns whether the named flag is on for the supplied context.
// Missing keys, storage errors, and disabled flags all return false.
//
// The context's user / tenant lists are checked first, then the rollout
// percentage. The decision is stable across processes — same key +
// same subject id always hashes the same way.
func (e *Evaluator) Bool(ctx context.Context, key string) bool {
	if e == nil || e.store == nil {
		return false
	}
	f, err := e.store.Get(ctx, key)
	if err != nil || f == nil {
		return false
	}
	return e.evaluate(ctx, f)
}

// evaluate applies the rule chain to an already-loaded flag. It performs
// no store access, so callers control exactly how many fetches happen —
// Bool and BoolDefault both fetch once and hand the result here. Keeping
// the decision logic store-free closes the TOCTOU double-read window and
// lets BoolDefault honour its fail-closed contract.
func (e *Evaluator) evaluate(ctx context.Context, f *Flag) bool {
	if f == nil || !f.Enabled {
		return false
	}
	ec := FromContext(ctx)
	// Env gate runs first — a flag restricted to staging must not be
	// reachable from production, even for explicitly allow-listed users.
	if len(f.Envs) > 0 && !containsString(f.Envs, ec.Env) {
		return false
	}
	if containsString(f.Users, ec.UserID) && ec.UserID != "" {
		return true
	}
	if containsString(f.Tenants, ec.TenantID) && ec.TenantID != "" {
		return true
	}
	if f.Rollout <= 0 {
		return false
	}
	if f.Rollout >= 100 {
		return true
	}
	// Anonymous subjects (no UserID or TenantID) hash to a single
	// constant bucket per flag — so a "50% rollout" would be 100% or
	// 0% of anonymous traffic depending on the key's hash. Force off
	// at any partial rollout to avoid that silent footgun. Apps that
	// want anonymous traffic in a rollout should derive a stable
	// pseudo-subject (request signature, session) and put it in
	// EvalContext.UserID themselves.
	subject := subjectID(ec)
	if subject == "" {
		return false
	}
	return bucket(e.salt, f.Key, subject) < f.Rollout
}

// BoolDefault returns the supplied fallback when the named flag is
// genuinely absent from the store; existing flags evaluate normally
// (so an explicitly-disabled flag returns false, ignoring fallback).
//
// Use this for kill switches where a typo or accidental delete must
// not silently re-enable the protected path. Example:
//
//	if app.Flags().BoolDefault(ctx, "kill-payments", true) {
//	    return payment.Block(...) // safe default: block on missing flag
//	}
func (e *Evaluator) BoolDefault(ctx context.Context, key string, fallback bool) bool {
	if e == nil || e.store == nil {
		return fallback
	}
	// Single guarded fetch: evaluating the flag we already loaded avoids
	// a second, independent store.Get inside Bool. That second read could
	// error (transient DB blip) and make Bool return false — fail open —
	// or observe a changed definition (TOCTOU). One read here keeps the
	// fallback authoritative whenever the store can't give a clean answer.
	f, err := e.store.Get(ctx, key)
	if err != nil || f == nil {
		return fallback
	}
	return e.evaluate(ctx, f)
}

// subjectID picks the stable identifier to hash for rollout.
// Preference order: user → tenant → empty (yields a uniform 0 bucket).
// "Empty" subjects always fall in bucket 0 so the rollout still gates
// them — that matches the principle of "anonymous traffic sees the new
// thing only at high rollout percentages."
func subjectID(ec EvalContext) string {
	if ec.UserID != "" {
		return ec.UserID
	}
	return ec.TenantID
}

// bucket returns an integer in [0, 100) for the (salt, key, subject)
// tuple. FNV-1a is good enough for distribution and far cheaper than
// crypto hashes — when adversary resistance matters, supply a non-
// empty salt at evaluator construction.
func bucket(salt, key, subject string) int {
	h := fnv.New32a()
	if salt != "" {
		h.Write([]byte(salt))
		h.Write([]byte{0})
	}
	h.Write([]byte(key))
	h.Write([]byte{0})
	h.Write([]byte(subject))
	return int(h.Sum32() % 100)
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ----- package-level default ------------------------------------------------

var (
	defaultMu   sync.RWMutex
	defaultEval *Evaluator
)

// SetDefault installs the process-wide evaluator used by the package-
// level helpers (Bool). Pass nil to disable and have Bool return false
// for every call.
func SetDefault(e *Evaluator) {
	defaultMu.Lock()
	defaultEval = e
	defaultMu.Unlock()
}

// Default returns the installed evaluator, or nil if none.
func Default() *Evaluator {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultEval
}

// Bool is a convenience wrapper around Default().Bool.
func Bool(ctx context.Context, key string) bool {
	return Default().Bool(ctx, key)
}

// ----- context plumbing -----------------------------------------------------

type ctxKey struct{}

// WithContext attaches an EvalContext to ctx. Middleware that knows the
// user and tenant should call this once per request; handlers then
// just call flag.Bool(ctx, ...).
func WithContext(ctx context.Context, ec EvalContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, ec)
}

// FromContext returns the attached EvalContext, or a zero value if none.
func FromContext(ctx context.Context) EvalContext {
	if ctx == nil {
		return EvalContext{}
	}
	v, _ := ctx.Value(ctxKey{}).(EvalContext)
	return v
}

// ----- memory store ---------------------------------------------------------

// MemoryStore is the bundled in-memory MutableStore implementation.
// Suitable for single-instance apps, tests, and as a cache layer in
// front of a persistent store.
type MemoryStore struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{flags: map[string]Flag{}}
}

// Get returns the stored flag, or (nil, nil) when absent.
func (m *MemoryStore) Get(_ context.Context, key string) (*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	if !ok {
		return nil, nil
	}
	return &f, nil
}

// Set creates or updates a flag definition. Empty Key returns an error.
func (m *MemoryStore) Set(f Flag) error {
	if f.Key == "" {
		return errors.New("flag: empty key")
	}
	if f.Rollout < 0 {
		f.Rollout = 0
	}
	if f.Rollout > 100 {
		f.Rollout = 100
	}
	m.mu.Lock()
	m.flags[f.Key] = f
	m.mu.Unlock()
	return nil
}

// Delete removes a flag. Missing keys are not an error.
func (m *MemoryStore) Delete(key string) error {
	m.mu.Lock()
	delete(m.flags, key)
	m.mu.Unlock()
	return nil
}

// All returns a snapshot of every defined flag, suitable for /admin
// listings. The result is a copy — mutations don't affect the store.
func (m *MemoryStore) All() []Flag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Flag, 0, len(m.flags))
	for _, f := range m.flags {
		out = append(out, f)
	}
	return out
}
