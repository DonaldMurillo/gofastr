// Package featureflag evaluates feature flags for the caller an EvalContext
// names.
package featureflag

import (
	"context"
	"hash/fnv"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Flag is the rule stored for a key: a kill switch, a 0-100 rollout, and
// [UserEntry] users, tenant ids and environments that allow it.
type Flag struct {
	Key     string
	Enabled bool
	Rollout int
	Users   []string
	Tenants []string
	Envs    []string
}

// EvalContext is the caller a flag is evaluated for.
type EvalContext struct {
	UserID   string
	TenantID string
	Env      string
	Attrs    map[string]string
}

// Store holds flags; Get returns (nil, nil) for an undefined key.
type Store interface {
	Get(ctx context.Context, key string) (*Flag, error)
}

// MutableStore is a Store that can also write flags.
type MutableStore interface {
	Store
	Set(f Flag) error
	Delete(key string) error
}

// Evaluator answers flag checks from a Store.
type Evaluator struct {
	store Store
	salt  string
}

// NewEvaluator wraps a Store; a nil Store answers every default.
func NewEvaluator(s Store) *Evaluator {
	return &Evaluator{store: s}
}

// NewEvaluatorWithSalt wraps a Store with a salt for the rollout hash.
func NewEvaluatorWithSalt(s Store, salt string) *Evaluator {
	return &Evaluator{store: s, salt: salt}
}

// Bool reports whether the flag is on; missing or unreadable is false.
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

func (e *Evaluator) evaluate(ctx context.Context, f *Flag) bool {
	if f == nil || !f.Enabled {
		return false
	}
	ec := FromContext(ctx)
	if len(f.Envs) > 0 && !slices.Contains(f.Envs, ec.Env) {
		return false
	}
	tenant := evalTenant(ctx, ec)
	if ec.UserID != "" && userAllowed(f.Users, tenant, ec.UserID) {
		return true
	}
	if slices.Contains(f.Tenants, tenant) && tenant != "" {
		return true
	}
	if f.Rollout <= 0 {
		return false
	}
	if f.Rollout >= 100 {
		return true
	}
	subject := subjectID(tenant, ec.UserID)
	if subject == "" {
		return false
	}
	return bucket(e.salt, f.Key, subject) < f.Rollout
}

// BoolDefault is Bool, but missing or unreadable returns fallback.
func (e *Evaluator) BoolDefault(ctx context.Context, key string, fallback bool) bool {
	if e == nil || e.store == nil {
		return fallback
	}
	f, err := e.store.Get(ctx, key)
	if err != nil || f == nil {
		return fallback
	}
	return e.evaluate(ctx, f)
}

func subjectID(tenantID, userID string) string {
	switch {
	case userID != "" && tenantID != "":
		return UserEntry(tenantID, userID)
	case userID != "":
		return userID
	}
	return tenantID
}

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

func evalTenant(ctx context.Context, ec EvalContext) string {
	if ec.TenantID != "" || ctx == nil {
		return ec.TenantID
	}
	return handler.GetTenantID(ctx)
}

var (
	defaultMu   sync.RWMutex
	defaultEval *Evaluator
)

// SetDefault installs the evaluator Bool uses when ctx carries none.
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

// Bool is EvaluatorFrom(ctx).Bool(ctx, key).
func Bool(ctx context.Context, key string) bool {
	return EvaluatorFrom(ctx).Bool(ctx, key)
}

type evaluatorCtxKey struct{}

type ctxEvaluator struct{ e *Evaluator }

// WithEvaluator attaches e, nil included, to ctx ahead of Default.
func WithEvaluator(ctx context.Context, e *Evaluator) context.Context {
	return context.WithValue(ctx, evaluatorCtxKey{}, ctxEvaluator{e})
}

// EvaluatorFrom returns ctx's evaluator, or Default() when it has none.
func EvaluatorFrom(ctx context.Context) *Evaluator {
	if ctx != nil {
		if v, ok := ctx.Value(evaluatorCtxKey{}).(ctxEvaluator); ok {
			return v.e
		}
	}
	return Default()
}

type ctxKey struct{}

// WithContext attaches an EvalContext to ctx.
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
