package access_test

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

type resolverUser string

func (u resolverUser) GetID() string { return string(u) }

func resolverCtx(id string) context.Context {
	return handler.SetUser(context.Background(), resolverUser(id))
}

func TestResolverCachesPerUser(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(context.Context) []string {
		calls.Add(1)
		return []string{"member"}
	})

	if got := resolver.Resolve(resolverCtx("alice")); !reflect.DeepEqual(got, []string{"member"}) {
		t.Fatalf("Resolve(alice) = %v", got)
	}
	_ = resolver.Resolve(resolverCtx("alice"))
	_ = resolver.Resolve(resolverCtx("bob"))
	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want one per user", got)
	}
}

func TestResolverTTLExpires(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(context.Context) []string {
		calls.Add(1)
		return []string{"member"}
	}, access.WithTTL(0))

	_ = resolver.Resolve(resolverCtx("alice"))
	_ = resolver.Resolve(resolverCtx("alice"))
	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want expired entry reloaded", got)
	}
}

func TestResolverInvalidatesUser(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(context.Context) []string {
		calls.Add(1)
		return []string{"member"}
	})
	ctx := resolverCtx("alice")

	_ = resolver.Resolve(ctx)
	resolver.Invalidate("alice")
	_ = resolver.Resolve(ctx)
	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want reload after invalidation", got)
	}
}

func TestResolverInvalidatesAll(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(context.Context) []string {
		calls.Add(1)
		return []string{"member"}
	})
	alice := resolverCtx("alice")
	bob := resolverCtx("bob")

	_ = resolver.Resolve(alice)
	_ = resolver.Resolve(bob)
	resolver.InvalidateAll()
	_ = resolver.Resolve(alice)
	_ = resolver.Resolve(bob)
	if got := calls.Load(); got != 4 {
		t.Fatalf("resolver calls = %d, want both users reloaded", got)
	}
}

func TestResolverSingleFlight(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	resolver := access.NewCachedResolver(func(context.Context) []string {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []string{"member"}
	})
	ctx := resolverCtx("alice")

	var wg sync.WaitGroup
	results := make(chan []string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- resolver.Resolve(ctx)
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)

	if got := calls.Load(); got != 1 {
		t.Fatalf("resolver calls = %d, want one concurrent load", got)
	}
	for got := range results {
		if !reflect.DeepEqual(got, []string{"member"}) {
			t.Fatalf("Resolve result = %v", got)
		}
	}
}

func TestResolverInvalidatesFlight(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	resolver := access.NewCachedResolver(func(context.Context) []string {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return []string{"member"}
	})
	ctx := resolverCtx("alice")

	firstDone := make(chan struct{})
	go func() {
		_ = resolver.Resolve(ctx)
		close(firstDone)
	}()
	<-started
	resolver.Invalidate("alice")
	close(release)
	<-firstDone
	_ = resolver.Resolve(ctx)

	if got := calls.Load(); got != 2 {
		t.Fatalf("resolver calls = %d, want invalidated flight reloaded", got)
	}
}

func TestResolverReturnsCopies(t *testing.T) {
	resolver := access.NewCachedResolver(func(context.Context) []string {
		return []string{"member"}
	})
	ctx := resolverCtx("alice")

	first := resolver.Resolve(ctx)
	first[0] = "admin"
	if got := resolver.Resolve(ctx); !reflect.DeepEqual(got, []string{"member"}) {
		t.Fatalf("cached roles mutated through caller: %v", got)
	}
}

func TestResolverSkipsAnonymousCache(t *testing.T) {
	var calls atomic.Int32
	resolver := access.NewCachedResolver(func(context.Context) []string {
		calls.Add(1)
		return nil
	})

	_ = resolver.Resolve(context.Background())
	_ = resolver.Resolve(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("anonymous resolver calls = %d, want uncached", got)
	}
}

func TestResolverFitsMiddleware(t *testing.T) {
	resolver := access.NewCachedResolver(func(context.Context) []string {
		return []string{"member"}
	})
	var middlewareResolver func(context.Context) []string = resolver.Resolve
	if middlewareResolver == nil {
		t.Fatal("Resolve method is not usable as Middleware resolver")
	}
}
