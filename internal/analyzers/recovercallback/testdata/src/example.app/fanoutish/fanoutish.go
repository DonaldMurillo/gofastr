// Package fanoutish mirrors core/fanout's Fanout interface, the
// cross-package module backend framework/event's AttachFanout takes
// (reduced to Publish). The dotted directory places it in the
// fixture-run's module (set via the analyzer's -module flag), standing
// in for github.com/DonaldMurillo/gofastr/core/fanout.
package fanoutish

import "context"

// Fanout carries real-time messages between replicas; implementations
// are host-supplied backends (the Postgres backend, a future Redis one).
type Fanout interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}
