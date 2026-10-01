// Package renderdiag connects recovered component panics to request-local test reporters.
package renderdiag

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

type observerKey struct{}

// WithObserver installs a request-local reporter without changing render recovery.
func WithObserver(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, observerKey{}, report)
}

// Report records the panicking subject's type, scrubbed panic and stack
// before fallback rendering. kind names WHAT panicked — "component",
// "layout build", "layout area", "fill fallback" — so an operator
// reading the log is not told a layout panic came from a component.
func Report(ctx context.Context, kind string, subj any, recovered any) {
	message := fmt.Sprintf("%s render panic: %T: %s", kind, subj, textsafe.Recovered(recovered))
	slog.ErrorContext(ctx, message, "stack", string(debug.Stack()))
	if report, ok := ctx.Value(observerKey{}).(func(string)); ok {
		report(message)
	}
}
