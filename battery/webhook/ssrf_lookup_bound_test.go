package webhook

import (
	"context"
	"net"
	"testing"
	"time"
)

// A subscriber whose DNS never answers is registered within the shared 3s lookup bound.
func TestSubscriberURLLookupBounded(t *testing.T) {
	prev := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	t.Cleanup(func() { net.DefaultResolver = prev })

	start := time.Now()
	err := validateSubscriberURL("https://hangs.example.test/hook", false)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("registration waited %s on a DNS server that never answers, want at most the 3s bound", d)
	}
	if err != nil {
		t.Fatalf("a DNS timeout refused registration: %v", err)
	}
}
