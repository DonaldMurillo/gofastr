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

// Every branch of the resolved-address guard refuses before any dial: a
// localhost name off loopback, a nil address, and a mixed public/internal
// set, which is refused whole even though its first entry is public.
func TestGuardedDialRejectsUnsafeResolutions(t *testing.T) {
	cases := []struct {
		name string
		addr string
		ips  []net.IPAddr
	}{
		{"localhost name resolving off loopback", "api.localhost:80", []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}},
		{"bare localhost resolving to a private address", "localhost:80", []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}},
		{"nil address", "vendor.example:443", []net.IPAddr{{IP: nil}}},
		{"mixed public and internal", "vendor.example:443", []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dialCalls := 0
			dial := guardedDialContext(
				func(context.Context, string) ([]net.IPAddr, error) { return tc.ips, nil },
				func(context.Context, string, string) (net.Conn, error) {
					dialCalls++
					return nil, errors.New("dial should not be reached")
				},
			)
			if _, err := dial(context.Background(), "tcp", tc.addr); err == nil {
				t.Fatal("unsafe resolution was accepted")
			}
			if dialCalls != 0 {
				t.Fatalf("dialed %d time(s) before refusing", dialCalls)
			}
		})
	}
}
