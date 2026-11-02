package queue

import (
	"context"
	"testing"
	"time"
)

// WithHandlerTimeout(0) armed a zero budget: the job's context was done
// before the handler started and the worker's timer fired at once, so
// every job failed with "exceeded its 0s budget", ran again, and was
// dead-lettered after its attempts while its handler may have succeeded
// each time. Its DBQueue sibling reads zero as "no timeout", so a port
// between the two hit it. A non-positive budget now keeps the 30s
// default, as outbox.WithHandlerTimeout does.
func TestMemoryNonPositiveHandlerTimeoutKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		q := NewMemoryQueue(0, WithHandlerTimeout(d))
		t.Cleanup(func() { _ = q.Close() })
		if q.handlerTimeout != defaultHandlerTimeout {
			t.Errorf("WithHandlerTimeout(%s): budget %s, want the %s default", d, q.handlerTimeout, defaultHandlerTimeout)
		}
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- q.awaitHandler(context.Background(), func(ctx context.Context, _ Job) error {
				<-release
				return ctx.Err()
			}, Job{Type: "t"})
		}()
		close(release)
		if err := <-done; err != nil {
			t.Errorf("WithHandlerTimeout(%s): a handler that returned at once failed: %v", d, err)
		}
	}
}
