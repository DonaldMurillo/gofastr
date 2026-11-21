// Package settle owns the bounded, detached context an outcome write
// runs on after the unit of work it records has returned.
package settle

import (
	"context"
	"time"
)

// Timeout bounds one outcome write (an ack or nack, a state write, a
// claim release) after the handler, delivery or run that produced the
// outcome returned.
const Timeout = 10 * time.Second

// Context detaches ctx from the caller's cancellation and bounds it by
// Timeout: a shutdown that lands as the work finishes must not drop the
// outcome write, and a store blocked on a dead database cannot hold the
// process past Stop.
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), Timeout)
}
