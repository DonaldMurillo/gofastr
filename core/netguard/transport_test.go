package netguard

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestGuardedTransportRefusesLoopback pins the dial-time half of the
// SSRF posture: even past validation, a guarded transport refuses to
// CONNECT to a loopback address (the DNS-rebinding backstop), and the
// allowPrivate posture does dial it. Moved here from core/a2a alongside
// the GuardedTransport constructor it exercises.
func TestGuardedTransportRefusesLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	tr := GuardedTransport(false, "netguard-test")
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := tr.DialContext(dialCtx, "tcp", ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("guarded transport dialed a loopback address")
	}
	// and the permissive posture does dial it
	open := GuardedTransport(true, "netguard-test")
	conn2, err := open.DialContext(dialCtx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("allowPrivate transport must dial loopback: %v", err)
	}
	_ = conn2.Close()
}
