// Package lib holds the cross-package halves: a runner that invokes a
// callback under its context (runsCallback fact) and a detached, bounded
// settle helper (detaches fact).
package lib

import (
	"context"
	"time"
)

// Run invokes fn under ctx.
func Run(ctx context.Context, fn func(context.Context) error) error { // want Run:`runsCallback\[0\]`
	ctx = context.WithValue(ctx, key{}, 1)
	return fn(ctx)
}

// Lookup takes a context and a callback but never runs the callback
// under that context.
func Lookup(ctx context.Context, fn func() error) error {
	_ = ctx
	return fn()
}

type key struct{}

// SettleContext is the outbox settleContext spelling.
func SettleContext(ctx context.Context) (context.Context, context.CancelFunc) { // want SettleContext:`detaches\(bounded=true\)`
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

// Detach drops the cancel but sets no bound: an unbounded detaches fact.
func Detach(ctx context.Context) context.Context { // want Detach:`detaches\(bounded=false\)`
	return context.WithoutCancel(ctx)
}
