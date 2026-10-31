package main

import (
	"fmt"
	"net"
	"testing"
)

// With every port in the scan range busy, pickPort returned 0: serve
// bound a kernel-chosen port while kilnURL said localhost:0, so the
// readiness poll timed out and the cleanup killed a healthy server
// (2026-10-27 K16).
func TestPickPortFallbackIsARealPort(t *testing.T) {
	base := busyRange(t, 10)
	p := pickPort(base)
	if p == 0 {
		t.Fatal("pickPort returned 0 with the scan range busy")
	}
	if p >= base && p < base+10 {
		t.Fatalf("pickPort returned %d, inside the busy range", p)
	}
	if !portFree(p) {
		t.Fatalf("pickPort returned %d, which has a listener", p)
	}
}

// busyRange holds n consecutive loopback ports for the test's life and
// returns the first.
func busyRange(t *testing.T, n int) int {
	t.Helper()
	for try := 0; try < 20; try++ {
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := probe.Addr().(*net.TCPAddr).Port
		held := []net.Listener{probe}
		for p := base + 1; p < base+n; p++ {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
			if err != nil {
				break
			}
			held = append(held, ln)
		}
		if len(held) == n {
			t.Cleanup(func() {
				for _, ln := range held {
					_ = ln.Close()
				}
			})
			return base
		}
		for _, ln := range held {
			_ = ln.Close()
		}
	}
	t.Skip("could not hold a run of consecutive loopback ports")
	return 0
}
