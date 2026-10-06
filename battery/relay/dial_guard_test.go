package relay

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestGuardedDialRejectsDNSRebindingToLoopback(t *testing.T) {
	dialCalls := 0
	dial := guardedDialContext(
		func(context.Context, string) ([]net.IPAddr, error) {
			// Simulate an external name resolving to a private endpoint after
			// the construction-time check accepted it or could not resolve it.
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("dial should not be reached")
		},
	)
	if _, err := dial(context.Background(), "tcp", "vendor.example:443"); err == nil {
		t.Fatal("request-time DNS result pointing at loopback was accepted")
	}
	if dialCalls != 0 {
		t.Fatalf("dialed internal address %d times", dialCalls)
	}
}

func TestGuardedDialUsesValidatedIPForPublicHost(t *testing.T) {
	var gotAddr string
	stop := errors.New("stop after observing address")
	dial := guardedDialContext(
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		},
		func(_ context.Context, _ string, addr string) (net.Conn, error) {
			gotAddr = addr
			return nil, stop
		},
	)
	if _, err := dial(context.Background(), "tcp", "vendor.example:443"); !errors.Is(err, stop) {
		t.Fatalf("dial error = %v, want injected stop", err)
	}
	if gotAddr != "8.8.8.8:443" {
		t.Fatalf("dial address = %q, want pinned DNS result", gotAddr)
	}
}
