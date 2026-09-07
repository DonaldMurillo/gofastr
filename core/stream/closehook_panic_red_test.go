//go:build red

package stream

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only; no fix applied.
// Property: a recovered panic value embedding hook-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi — the scrubControlBytes
// rule pinned for RecoveryFn + Timeout's late-panic log (core/middleware)
// and battery/log's SlogErrorReporter.
// Surface: core/stream/websocket.go::Close (runCloseHooks block)
// :470-472 — slog.Default().Error("stream: websocket close hook
// panicked", "panic", rec) with the raw recovered value, fired from a
// fresh goroutine per hook.
// Finding: an app-supplied OnClose hook that panics with a value carrying
// raw control bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI)
// gets them logged verbatim by slog.Default(); those bytes paint forged
// lines into the operator's tail (terminal-injection). The recovery
// itself is the designed net — the scrub of the recovered value is the
// open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// redWsScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away.
type redWsScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redWsScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redWsScrubSink) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.attrs[a.Key] = a.Value.String()
		return true
	})
	return nil
}

func (h *redWsScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redWsScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redWsScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

func TestWSCloseHookRedPanicScrubbed(t *testing.T) {
	prev := slog.Default()
	sink := &redWsScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup broken: listen: %v", err)
	}
	defer ln.Close()

	conns := make(chan *WebSocketConn, 1)
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r, WSConfig{CloseTimeout: 100 * time.Millisecond})
		if err != nil {
			return
		}
		conns <- conn
	}))

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("setup broken: dial: %v", err)
	}
	defer cli.Close()

	handshake := "GET /ws HTTP/1.1\r\n" +
		"Host: " + ln.Addr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	if _, err := cli.Write([]byte(handshake)); err != nil {
		t.Fatalf("setup broken: handshake write: %v", err)
	}
	if err := cli.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setup broken: client deadline: %v", err)
	}
	resp := make([]byte, 0, 512)
	buf := make([]byte, 512)
	for !strings.Contains(string(resp), "\r\n\r\n") {
		n, rerr := cli.Read(buf)
		resp = append(resp, buf[:n]...)
		if rerr != nil {
			t.Fatalf("setup broken: reading 101: %v (got %s)", rerr, resp)
		}
	}
	if !strings.Contains(string(resp), "101 Switching Protocols") {
		t.Fatalf("setup broken: expected 101, got: %s", resp)
	}

	var conn *WebSocketConn
	select {
	case conn = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatalf("setup broken: server never completed the upgrade")
	}

	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	conn.OnClose(func() { panic(malicious) })
	_ = conn.Close() // bounded: CloseTimeout 100ms caps the peer-close wait

	// The hook fires on its own goroutine: bounded wait for the log.
	var got string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got = sink.redGet("panic"); got != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got == "" {
		t.Fatalf("setup broken: close-hook panic never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [ws-closehook-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: hook-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every pinned "+
				"recover path (RecoveryFn, Timeout, SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [ws-closehook-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
