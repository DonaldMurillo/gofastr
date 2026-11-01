package relay

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The relay is a bounded request/response proxy: body caps, a 30 s
// deadline, no-store. A protocol switch would hand a vendor an open
// socket past every one of those, so the relay refuses it. Before round 7
// (R7-060) an inbound Upgrade reached the vendor, the vendor's 101 met
// cappedBody (not an io.ReadWriteCloser), ReverseProxy answered 502 and
// returned without closing the upstream socket: one leaked conn per try.

func TestRelayStripsInboundUpgrade(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)
	req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/socket", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	_, _, _, _, h := cap.snapshot()
	if h == nil {
		t.Fatal("upstream saw no request")
	}
	if got := h.Get("Upgrade"); got != "" {
		t.Fatalf("upstream got Upgrade: %q; the relay must not ask the vendor to switch protocols", got)
	}
	if strings.Contains(strings.ToLower(h.Get("Connection")), "upgrade") {
		t.Fatalf("upstream got Connection: %q", h.Get("Connection"))
	}
}

func TestRelayRefuses101AndClosesUpstream(t *testing.T) {
	closed := make(chan struct{})
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, r *http.Request) {
		// A vendor that switches protocols whatever it was asked.
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = brw.Flush()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, err = io.Copy(io.Discard, conn)
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			close(closed) // EOF or reset: the relay closed its end
		}
	})

	conn, err := net.Dial("tcp", strings.TrimPrefix(base, "http://"))
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET /__gofastr/t/e/socket HTTP/1.1\r\nHost: app\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read relay response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for a refused protocol switch", resp.StatusCode)
	}
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("the relay left the upstream socket open after refusing its 101")
	}
}
