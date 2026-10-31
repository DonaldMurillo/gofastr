package access_test

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// At a zero TTL the result expires as it lands, and the callers waiting on
// the lookup still share it: each went on to run its own lookup, one
// after another, in place of the single flight the option promises.
func TestZeroTTLWaitersShareFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		release := make(chan struct{})
		resolver := access.NewCachedResolver(func(context.Context) []string {
			calls.Add(1)
			<-release
			return []string{"member"}
		}, access.WithTTL(0))
		ctx := resolverCtx("alice")

		var wg sync.WaitGroup
		results := make(chan []string, 10)
		for range 10 {
			wg.Go(func() { results <- resolver.Resolve(ctx) })
		}
		synctest.Wait() // one lookup runs, the other nine wait on it
		close(release)
		wg.Wait()
		close(results)

		if got := calls.Load(); got != 1 {
			t.Fatalf("resolver calls = %d, want the waiters to share one", got)
		}
		for got := range results {
			if !reflect.DeepEqual(got, []string{"member"}) {
				t.Fatalf("Resolve result = %v", got)
			}
		}
		// Expired at once, so the next call looks up again.
		_ = resolver.Resolve(ctx)
		if got := calls.Load(); got != 2 {
			t.Fatalf("resolver calls after expiry = %d, want 2", got)
		}
	})
}

// A flight invalidated mid-lookup shares nothing: a caller waiting on it
// resolves again, so it never gets roles from before the Invalidate.
func TestInvalidatedFlightNotShared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		release := make(chan struct{})
		resolver := access.NewCachedResolver(func(context.Context) []string {
			if calls.Add(1) == 1 {
				<-release
				return []string{"admin"}
			}
			return []string{"member"}
		})
		ctx := resolverCtx("alice")

		go resolver.Resolve(ctx)
		synctest.Wait()
		waited := make(chan []string, 1)
		go func() { waited <- resolver.Resolve(ctx) }()
		synctest.Wait()
		resolver.Invalidate("alice")
		close(release)

		if got := <-waited; !reflect.DeepEqual(got, []string{"member"}) {
			t.Fatalf("waiter got %v from the invalidated flight, want a fresh [member]", got)
		}
	})
}
