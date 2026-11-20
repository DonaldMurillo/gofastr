package auth

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// rendezvous pins a read→write race window open deterministically in a
// store wrapper: when armed, the FIRST gated call blocks until the SECOND
// arrives, so both callers hold pre-write copies of the state before
// either can reach the write — no scheduling luck involved. The 10s
// timeout falls through rather than hang forever; the caller's assertions
// report whatever actually happened.
type rendezvous struct {
	armed     atomic.Bool
	entered   atomic.Int64
	release   chan struct{}
	closeOnce sync.Once
}

// gate blocks per the rendezvous protocol and returns the context error,
// if any, that ended the wait. It is a no-op until armed.
func (r *rendezvous) gate(ctx context.Context) error {
	if !r.armed.Load() {
		return nil
	}
	if r.entered.Add(1) == 1 {
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			// Fall through rather than hang forever; the assertions
			// below report whatever actually happened.
		}
	} else {
		r.closeOnce.Do(func() { close(r.release) })
	}
	return nil
}
