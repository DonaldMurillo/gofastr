package kit

// The ctxless per-request-value arm: store.Slice.Default() where a
// request context is in scope (af3292807).

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
)

type CounterConfig struct {
	Ctx   context.Context
	Slice *store.Slice[int]
}

// Counter is ui.Counter before af3292807: cfg.Ctx is right there and
// the first paint is the declaration.
func Counter(cfg CounterConfig) string {
	initial := 0
	if cfg.Slice != nil {
		initial = cfg.Slice.Default() // want `store.Slice.Default\(\) is the declaration, not this request's value.*read cfg.Slice.Value\(cfg.Ctx\)`
	}
	return strconv.Itoa(initial)
}

// CounterFixed is the fix spelling.
func CounterFixed(cfg CounterConfig) string {
	initial := 0
	if cfg.Slice != nil {
		initial = cfg.Slice.Value(cfg.Ctx)
	}
	return strconv.Itoa(initial)
}

// tabIndex reads the default with a ctx parameter in scope.
func tabIndex(ctx context.Context, sl *store.Slice[int]) int {
	return sl.Default() // want `read sl.Value\(ctx\)`
}

type ToggleConfig struct {
	Slice *store.Slice[bool]
}

// Toggle is ui.SignalToggle before af3292807: no context reaches it, so
// Default is all it can read (the fix gave its config a Ctx).
func Toggle(cfg ToggleConfig) bool {
	return cfg.Slice != nil && cfg.Slice.Default()
}

// declared reads the default at declaration time: no request.
var declared = func() int {
	return (&store.Slice[int]{}).Default()
}()

// fallback is not a store Slice: a Default() on another type stays quiet
// with a context in scope (a near-miss for a name-only matcher).
type fallback struct{}

func (fallback) Default() int { return 0 }

func otherDefault(ctx context.Context) int {
	return fallback{}.Default()
}
