package harnesscmd

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// The chat page at / embeds a 24 h human-class token, and the Host
// guard does not stop a peer that reaches the port and sends
// Host: localhost. So -listen binds loopback only.
func TestListenRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0"} {
		if ln, err := listenLoopback(addr); err == nil {
			ln.Close()
			t.Fatalf("listenLoopback(%q) bound a non-loopback address", addr)
		}
	}
	if ip := lanIP(); ip != "" {
		if ln, err := listenLoopback(net.JoinHostPort(ip, "0")); err == nil {
			ln.Close()
			t.Fatalf("listenLoopback bound the LAN address %s", ip)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:0"} {
		ln, err := listenLoopback(addr)
		if err != nil {
			t.Fatalf("listenLoopback(%q): %v", addr, err)
		}
		ln.Close()
	}

	// A refused bind does not keep the port: the same wildcard bind
	// succeeds right after.
	probe, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()
	if ln, err := listenLoopback("0.0.0.0:" + port); err == nil {
		ln.Close()
		t.Fatal("wildcard bind accepted")
	}
	again, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		t.Fatalf("refused bind kept port %s: %v", port, err)
	}
	again.Close()
}

// startHTTPListener refuses the bind before it touches the harness, so
// a nil one is enough to see the refusal.
func TestHTTPListenerRefusesWildcard(t *testing.T) {
	_, _, _, err := startHTTPListener(nil, ids.NewSessionID(), "0.0.0.0:0")
	if err == nil || !strings.Contains(err.Error(), "not a loopback address") {
		t.Fatalf("startHTTPListener on 0.0.0.0: err = %v, want the loopback refusal", err)
	}
}

// lanIP returns a non-loopback IPv4 address of this machine, or "".
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}
