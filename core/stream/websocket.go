package stream

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WebSocketConn wraps a hijacked HTTP connection as a simple WebSocket
// client. It implements a minimal WebSocket frame parser sufficient for
// the framework's needs: text and binary messages, close frames.
//
// For production use with full RFC 6455 compliance, use a dedicated
// WebSocket library (nhooyr.io/websocket, gorilla/websocket). This
// implementation avoids external dependencies so the core/stream package
// compiles without `go get` additions.
//
// Backpressure: writes block when the send buffer is full; reads come
// from a small internal buffer fed by the connection's own read pump.
// The read pump owns ALL socket reads and runs from the moment the
// connection starts, so control frames (Ping/Pong/Close) are handled
// even if the application never calls Read. Read() consumes complete
// messages from that buffer; beyond it, TCP backpressure applies. A
// push-only server needs no read loop; it stays alive while a healthy
// peer answers Pongs, and Close()'s handshake completes promptly when
// the peer reciprocates.
type WebSocketConn struct {
	conn       io.ReadWriteCloser
	mu         sync.Mutex
	sendBuffer chan []byte
	closed     chan struct{}
	closeOnce  sync.Once
	onClose    []func()
	config     WSConfig

	// connectionID identifies this WebSocket across logs and reconnects.
	// Set from WSConfig.ConnectionID or generated at Upgrade; see
	// ConnectionID for the reconnect-generation contract.
	connectionID string

	// lastReadActivity is updated on every successful frame read and on
	// pong receipt. Used by the keepalive goroutine to decide when to
	// send a ping. Stored as unix nanos for lock-free reads.
	lastReadActivity atomic.Int64

	// awaitingPong is set when a ping has been sent and is true until a
	// matching pong arrives (or the timeout expires).
	awaitingPong atomic.Bool
	pingSentAt   atomic.Int64

	// peerClosed is closed by readFrame the first time it parses a Close
	// frame from the peer. Close() waits on this (or CloseTimeout) so the
	// closing handshake is signal-driven: no parallel byte-scan, no
	// reader race. peerCloseOnce protects the close-of-channel from
	// concurrent readers.
	peerClosed    chan struct{}
	peerCloseOnce sync.Once
	// peerClosePayload stores the sanitized bytes to echo in our Close
	// frame: the peer's status code if it is echoable per §7.4.1, 1002
	// (protocol error) if the peer sent a reserved/invalid code or an
	// illegal 1-byte body, or empty if the peer sent a bodyless Close
	// (1005 = no status). Stored as a *[]byte so we can distinguish "no
	// Close seen yet" (nil) from "Close with empty payload" (non-nil).
	peerClosePayload atomic.Pointer[[]byte]

	// readMsgs delivers decoded data messages from the internal read
	// pump to Read(). Buffered (cap 8): beyond that, TCP backpressure
	// applies. A push-only server that never drains still receives
	// control frames because the pump keeps reading the wire.
	readMsgs chan []byte
	// readDone is closed by the read pump when it exits (after setting
	// readErr). Read() observes readErr only after receiving from
	// readDone: the Go memory model guarantees the store that happened
	// before close(readDone) is visible to a reader after the receive.
	readDone chan struct{}
	readErr  error
	// handoffPending is true while the pump is blocked handing a
	// message to a slow (or absent) consumer. The keepalive skips its
	// tick while this is set: a stalled handoff means the app is slow
	// to drain, not that the peer is dead; killing there would
	// recreate the very bug the pump exists to fix.
	handoffPending atomic.Bool
}

// WSConfig configures the WebSocket connection.
type WSConfig struct {
	// ReadLimit is the maximum message size in bytes. 0 = default 1MB.
	ReadLimit int64

	// SendBuffer is the number of messages that can be buffered before
	// Write blocks. 0 = 32.
	SendBuffer int

	// WriteTimeout bounds each frame write. 0 means default 10s. Set
	// negative to disable (not recommended): a peer with a full TCP send
	// buffer otherwise pins the writePump and keepalive goroutines forever.
	WriteTimeout time.Duration

	// CheckOrigin returns true if the Origin header is acceptable.
	// If nil, Upgrade enforces same-origin by comparing Origin host to
	// the request Host. Use a custom CheckOrigin to allow cross-origin
	// upgrades (e.g. for trusted third-party clients).
	CheckOrigin func(*http.Request) bool

	// requireMask, when true, rejects unmasked client frames per RFC 6455.
	// Upgrade always sets this to true; the field is unexported so callers
	// cannot accidentally disable it. Test helpers that craft synthetic
	// unmasked frames construct the WSConfig directly via this package.
	requireMask bool

	// ReadIdleTimeout bounds the longest period of read inactivity before
	// the keepalive sends a Ping. 0 means default 60s. Set negative to
	// disable keepalive entirely.
	ReadIdleTimeout time.Duration

	// PongTimeout bounds how long after a Ping we wait for the matching
	// Pong. If exceeded, the connection is closed. 0 means default 10s.
	// Set negative to disable the pong timeout check.
	PongTimeout time.Duration

	// CloseTimeout caps how long Close() waits for the peer's reciprocal
	// Close frame after sending our own. 0 means default 1s.
	CloseTimeout time.Duration

	// Subprotocols is the server's preferred list of WebSocket subprotocols
	// in priority order. During Upgrade, the first subprotocol that the
	// client offered AND we support is echoed back via
	// Sec-WebSocket-Protocol. If no match, no header is sent (RFC 6455).
	Subprotocols []string

	// ConnectionID identifies this connection in logs and reconnect
	// bookkeeping. Empty (the default) means Upgrade generates a random
	// id. Set it explicitly when the application mints its own ids
	// (e.g. one per browser session, so a client's reconnect after a
	// socket drop correlates across server-side logs). See
	// WebSocketConn.ConnectionID for the reconnect-generation contract.
	ConnectionID string

	// OnClose is called when the connection closes.
	OnClose func()
}

// Upgrade upgrades an HTTP connection to a simple WebSocket.
// Performs the HTTP upgrade handshake and returns a managed connection.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg WSConfig) (*WebSocketConn, error) {
	// Validate the handshake per RFC 6455 §4.2.1.
	//
	// Accepting a looser definition of "upgrade" than an intermediary
	// uses is an upgrade-desync primitive: a proxy that does not
	// recognise the request as an upgrade forwards it as an ordinary
	// request on a pooled backend connection and waits for an ordinary
	// response. If we hijack and start writing frames, those bytes can
	// surface as the response to a DIFFERENT user's queued request,
	// and the peer partly controls them, since ping payloads are
	// echoed verbatim. So every condition below must be checked before
	// the 101 is written.
	if r.Method != http.MethodGet {
		return nil, errors.New("stream: websocket upgrade must be GET")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("stream: not a websocket upgrade request")
	}

	// Origin check: block CSWSH by default. Caller may opt in to
	// cross-origin via cfg.CheckOrigin. Runs before the remaining
	// shape checks so a cross-origin attempt reports the security
	// reason rather than whichever header it also happened to omit.
	if !checkOrigin(r, cfg.CheckOrigin) {
		return nil, errors.New("stream: cross-origin websocket upgrade rejected (set WSConfig.CheckOrigin to allow)")
	}
	if !headerHasToken(r.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("stream: missing Connection: Upgrade")
	}
	if v := r.Header.Get("Sec-WebSocket-Version"); v != "13" {
		return nil, fmt.Errorf("stream: unsupported websocket version %q (want 13)", v)
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("stream: missing Sec-WebSocket-Key")
	}
	// §4.2.1: the key is base64 of exactly 16 random bytes.
	if kb, err := base64.StdEncoding.DecodeString(key); err != nil || len(kb) != 16 {
		return nil, errors.New("stream: malformed Sec-WebSocket-Key")
	}

	// Compute accept key (SHA-1 of key + magic GUID)
	acceptKey := computeAcceptKey(key)

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("stream: response writer does not support hijacking")
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("stream: hijack failed: %w", err)
	}

	// Flush any buffered data from bufrw
	if bufrw != nil {
		bufrw.Flush()
	}

	// Negotiate subprotocol per RFC 6455 §4.2.2.
	subprotoHeader := ""
	if chosen := pickSubprotocol(r, cfg.Subprotocols); chosen != "" {
		subprotoHeader = "Sec-WebSocket-Protocol: " + chosen + "\r\n"
	}

	// Write the upgrade response directly
	upgradeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n" +
		subprotoHeader + "\r\n"
	if _, err := conn.Write([]byte(upgradeResp)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("stream: write upgrade response: %w", err)
	}

	readLimit := cfg.ReadLimit
	if readLimit == 0 {
		readLimit = 1 << 20 // 1MB
	}
	sendBuf := cfg.SendBuffer
	if sendBuf == 0 {
		sendBuf = 32
	}
	// 0 = default, negative = disable. Negative values are honored verbatim
	// so callers can opt out of keepalive (and explicitly disable the pong
	// timeout) without the constructor silently rewriting their intent.
	if cfg.ReadIdleTimeout == 0 {
		cfg.ReadIdleTimeout = 60 * time.Second
	}
	if cfg.PongTimeout == 0 {
		cfg.PongTimeout = 10 * time.Second
	}
	if cfg.CloseTimeout == 0 {
		cfg.CloseTimeout = 1 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}

	// Real upgrades always enforce mask-from-client per RFC 6455.
	cfg.requireMask = true
	cfg.ReadLimit = readLimit
	cfg.SendBuffer = sendBuf

	// Per-connection identity for logs and reconnect correlation. A
	// caller-supplied id wins; otherwise mint a random one.
	connectionID := cfg.ConnectionID
	if connectionID == "" {
		connectionID = randomConnectionID()
	}

	wsc := &WebSocketConn{
		conn:         conn,
		sendBuffer:   make(chan []byte, sendBuf),
		closed:       make(chan struct{}),
		peerClosed:   make(chan struct{}),
		readMsgs:     make(chan []byte, 8),
		readDone:     make(chan struct{}),
		config:       cfg,
		connectionID: connectionID,
	}
	wsc.lastReadActivity.Store(time.Now().UnixNano())

	if cfg.OnClose != nil {
		wsc.onClose = append(wsc.onClose, cfg.OnClose)
	}

	// Start the write pump, keepalive, and the internal read pump. The
	// read pump owns all socket reads so control frames (Ping/Pong/
	// Close) are handled even when the app never calls Read.
	go wsc.writePump()
	wsc.startKeepalive()
	wsc.startReadPump()

	return wsc, nil
}

// randomConnectionID mints a per-connection id: 8 random bytes, hex.
// Random (not monotonic) so the id carries no ordering information
// across users; distinctness per reconnect is what correlates a
// browser generation with a server-side connection. crypto/rand.Read
// cannot fail on a supported platform, and a time-derived fallback
// would be enumerable precisely when randomness is gone — so a failure
// is fatal instead.
func randomConnectionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("stream: crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b[:])
}

// pickSubprotocol returns the first server-preferred subprotocol that
// appears in the client's Sec-WebSocket-Protocol header. Returns "" if
// the client offered none or none match. Server preference order wins
// per RFC 6455 §4.2.2.
func pickSubprotocol(r *http.Request, serverPrefs []string) string {
	if len(serverPrefs) == 0 {
		return ""
	}
	raw := r.Header.Get("Sec-WebSocket-Protocol")
	if raw == "" {
		return ""
	}
	offered := make(map[string]struct{})
	for p := range strings.SplitSeq(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			offered[p] = struct{}{}
		}
	}
	for _, p := range serverPrefs {
		if _, ok := offered[p]; ok {
			return p
		}
	}
	return ""
}

// checkOrigin returns true if the Origin header is acceptable for the
// upgrade. If a custom check is provided, it wins. Otherwise the default
// is same-origin: Origin host must equal the request Host. Missing Origin
// (non-browser client) is permitted.
func checkOrigin(r *http.Request, custom func(*http.Request) bool) bool {
	if custom != nil {
		return custom(r)
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// Write sends a text message to the client. Blocks if the send buffer
// is full (backpressure). Returns an error if the connection is closed.
func (c *WebSocketConn) Write(data []byte) error {
	// Check closed first to avoid non-deterministic select
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	select {
	case <-c.closed:
		return ErrClosed
	case c.sendBuffer <- data:
		return nil
	}
}

// WriteString is a convenience for sending a text message.
func (c *WebSocketConn) WriteString(data string) error {
	return c.Write([]byte(data))
}

// Read reads a complete message from the client. It consumes from the
// internal read pump's buffer (readMsgs); it never reads the socket
// directly, so the pump remains the sole owner of the wire and control
// frames stay processed even between Read calls. A push-only server
// need not call Read at all; the pump still answers Pings, clears the
// keepalive's Pong watch, and completes Close()'s handshake.
//
// Messages decoded by the pump before a terminal error are delivered
// before the error surfaces (drain-before-error). Concurrent Read
// callers are safe: the buffer channel serializes delivery.
func (c *WebSocketConn) Read() ([]byte, error) {
	// Non-blocking drain first: a message already buffered must be
	// returned even after the pump has died (readDone closed), so a
	// random select never lets the terminal error leapfrog a decoded
	// message.
	select {
	case msg := <-c.readMsgs:
		return msg, nil
	default:
	}
	// Block for the next message or for pump termination.
	select {
	case msg := <-c.readMsgs:
		return msg, nil
	case <-c.readDone:
		// readErr was stored before close(readDone); the channel-close
		// happens-before this read, so the store is visible. Do NOT
		// call Close() here; the pump already tore the connection down.
		return nil, c.readErr
	}
}

// Close closes the WebSocket connection. Safe to call multiple times.
// Performs the RFC 6455 closing handshake: sends a Close frame, then
// waits up to CloseTimeout for the peer's reciprocal Close before tearing
// down the underlying TCP connection. This avoids the abnormal 1006
// close code on the peer side.
//
// If the peer initiated the close, the echo Close frame preserves the
// peer's 2-byte status code per RFC 6455 §5.5.1, sanitized per §7.4.1
// so a reserved (1004/1005/1006/1015), sub-1000, or otherwise invalid
// code, or an illegal 1-byte body, is never echoed verbatim and is
// replaced by 1002. Otherwise we send an empty Close payload (status
// 1000 implied by absence).
func (c *WebSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// If readFrame already captured a peer Close payload, echo the
		// (already-sanitized) bytes back. Otherwise send an empty Close.
		echo := c.peerClosePayload.Load()
		var payload []byte
		if echo != nil {
			payload = *echo
		}
		// Send close frame. Ignore the error: if writing fails the peer
		// is likely already gone, but we still want to drop the TCP conn.
		_ = c.writeFrame(wsopcodeClose, payload)
		close(c.closed)

		// Drain incoming frames briefly so we receive the peer's Close
		// (or timeout). Bound the deadline so a silent peer can't pin us.
		c.awaitPeerClose()

		// Snapshot onClose callbacks under the mutex, then fire outside
		// the lock so callbacks cannot deadlock against OnClose() callers.
		c.mu.Lock()
		callbacks := append([]func(){}, c.onClose...)
		c.onClose = nil
		c.mu.Unlock()

		if closer, ok := c.conn.(interface{ Close() error }); ok {
			err = closer.Close()
		}
		for _, fn := range callbacks {
			// Close hooks are app-supplied callbacks on their own
			// goroutine, which has no net: a panicking hook must not
			// take the process down with the connection.
			go func(fn func()) {
				defer func() {
					if rec := recover(); rec != nil {
						slog.Default().Error("stream: websocket close hook panicked", "panic", rec)
					}
				}()
				fn()
			}(fn)
		}
	})
	return err
}

// awaitPeerClose waits for the read pump to signal that it parsed the
// peer's reciprocal Close frame, or for CloseTimeout to elapse. The
// pump is the sole reader, so this never races another reader on the
// same TCP stream. If the peer already sent Close before our Close()
// was invoked (the common responder path), peerClosed is already
// closed and this returns immediately.
func (c *WebSocketConn) awaitPeerClose() {
	timeout := c.config.CloseTimeout
	if timeout <= 0 {
		timeout = 1 * time.Second
	}
	select {
	case <-c.peerClosed:
	case <-time.After(timeout):
	}
}

// OnClose registers a callback for when the connection closes.
func (c *WebSocketConn) OnClose(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onClose = append(c.onClose, fn)
}

// Closed returns a channel closed when the connection closes.
func (c *WebSocketConn) Closed() <-chan struct{} {
	return c.closed
}

// ConnectionID returns the connection's stable identifier: the
// WSConfig.ConnectionID value, or a random id minted at Upgrade.
//
// The id distinguishes server-side connections from each other; it
// does NOT by itself model reconnects. A client that reconnects gets a
// NEW connection and therefore a new id (or a fresh browser-side
// generation, see core-ui/runtime/src/ws.js). Applications that need
// "same user, new transport" semantics correlate the two explicitly:
// echo this id to the client on connect, or key the client's
// generation counter against it in logs.
func (c *WebSocketConn) ConnectionID() string {
	return c.connectionID
}

// startKeepalive starts the idle-timeout / ping-pong watcher goroutine.
// No-op if both ReadIdleTimeout and PongTimeout are zero (keepalive
// disabled). The goroutine exits when the connection closes.
func (c *WebSocketConn) startKeepalive() {
	if c.config.ReadIdleTimeout <= 0 {
		return
	}
	c.lastReadActivity.CompareAndSwap(0, time.Now().UnixNano())
	go c.keepalive()
}

// keepalive periodically checks read activity. If the connection has
// been idle for ReadIdleTimeout, it sends a Ping. If no Pong arrives
// within PongTimeout, the connection is closed.
func (c *WebSocketConn) keepalive() {
	idle := c.config.ReadIdleTimeout
	pongTimeout := c.config.PongTimeout
	if pongTimeout <= 0 {
		pongTimeout = 10 * time.Second
	}
	// Check at a granularity finer than the smaller of the two thresholds.
	tick := max(min(pongTimeout/4, idle/4), 10*time.Millisecond)
	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-c.closed:
			return
		case now := <-t.C:
			// A stalled handoff means the app is slow to drain a
			// message, not that the peer is dead. While the pump is
			// blocked handing off, it also can't read the peer's Pong,
			// so enforcing PongTimeout here would recreate this very
			// bug one layer up and kill a healthy connection. Skip the
			// whole tick; a genuinely dead peer with an idle app still
			// dies: no messages arrive, handoffPending stays false, a
			// ping goes unanswered, and PongTimeout kills it.
			if c.handoffPending.Load() {
				continue
			}
			lastRead := time.Unix(0, c.lastReadActivity.Load())
			// If awaiting a pong, enforce PongTimeout.
			if c.awaitingPong.Load() {
				sent := time.Unix(0, c.pingSentAt.Load())
				if now.Sub(sent) > pongTimeout {
					c.Close()
					return
				}
				continue
			}
			if now.Sub(lastRead) >= idle {
				// Send ping and start the pong clock.
				c.pingSentAt.Store(now.UnixNano())
				c.awaitingPong.Store(true)
				if err := c.writeFrame(wsopcodePing, nil); err != nil {
					c.Close()
					return
				}
			}
		}
	}
}

// writePump drains the send buffer and writes WebSocket frames.
func (c *WebSocketConn) writePump() {
	for {
		select {
		case <-c.closed:
			return
		case msg := <-c.sendBuffer:
			if err := c.writeFrame(wsopcodeText, msg); err != nil {
				c.Close()
				return
			}
		}
	}
}

// startReadPump launches the internal read pump, the single goroutine
// that owns ALL socket reads. It processes control frames (Ping/Pong/
// Close) even when the application never calls Read, so a push-only
// server stays alive while a healthy peer answers Pongs, and Close()'s
// handshake completes promptly when the peer reciprocates.
func (c *WebSocketConn) startReadPump() {
	go c.readPump()
}

// readPump reads frames in a loop and hands decoded data messages to
// Read() via readMsgs. Control frames are handled inline by readFrame
// (Pong clears awaitingPong, Ping auto-pongs, Close signals peerClosed),
// so the pump keeps the connection healthy with no help from the app.
//
// On any readFrame error the pump records readErr, closes readDone (the
// happens-before edge that lets Read observe readErr), and tears the
// connection down, mirroring the old Read()'s error path. After Close()
// closes the underlying conn, the blocked io.ReadFull inside readFrame
// errors and the pump exits; no deadline is needed because the teardown
// is the unblocker.
//
// When handing a message to a slow or absent consumer, the pump blocks
// on readMsgs but also selects on c.closed: if the connection is closing
// it drops the undelivered data and KEEPS LOOPING, so the peer's
// reciprocal Close still reaches readFrame and awaitPeerClose returns
// promptly instead of burning CloseTimeout.
func (c *WebSocketConn) readPump() {
	for {
		frame, err := c.readFrame()
		if err != nil {
			c.readErr = err
			close(c.readDone)
			c.Close() // idempotent; mirrors the old Read() error path
			return
		}
		c.handoffPending.Store(true)
		select {
		case c.readMsgs <- frame.payload:
		case <-c.closed:
			// Closing: drop undelivered data but keep reading so the
			// peer's reciprocal Close reaches readFrame, signals
			// peerClosed, and lets awaitPeerClose return promptly.
		}
		c.handoffPending.Store(false)
	}
}

// WebSocket frame opcodes
const (
	wsopcodeContinuation = 0x0
	wsopcodeText         = 0x1
	wsopcodeBinary       = 0x2
	wsopcodeClose        = 0x8
	wsopcodePing         = 0x9
	wsopcodePong         = 0xA
)

// wsFrame represents a parsed WebSocket frame.
type wsFrame struct {
	opcode  byte
	payload []byte
}

// wsEchoableCloseCode reports whether a close code received from the
// peer may be echoed back on the wire. RFC 6455 §7.4.1 defines status
// codes 1000-1011 (with 1004 reserved) and §7.4.2 reserves the whole
// 1000-2999 range for the protocol and its extensions. The IANA
// registry later assigned 1012-1014 (Service Restart / Try Again Later
// / Bad Gateway), but they are not defined by RFC 6455 itself, so a
// strict RFC-6455-only peer may treat them as unassigned and answer
// 1002. Because we are echoing the peer's code rather than originating
// one, and 1002 is universally safe, we sanitize conservatively:
//
//   - 1000-1003, 1007-1011 echo as-is (the RFC-6455-defined codes).
//   - 3000-4999 echo as-is (framework/library and private-use ranges).
//
// Never echoed (replaced by 1002 by the caller):
//   - 0-999 and 5000+: outside the close-code space (§7.4.2).
//   - 1004: reserved (§7.4.1).
//   - 1005, 1006, 1015: reserved for API-internal signalling; §7.4.1
//     says they MUST NOT appear in a Close frame.
//   - 1012-1014: IANA-registered but not RFC-6455-defined (conservative
//     deny against strict peers).
//   - 2000-2999: reserved for protocol/extensions per §7.4.2.
func wsEchoableCloseCode(code uint16) bool {
	switch {
	case code >= 1000 && code <= 1003:
		return true
	case code >= 1007 && code <= 1011:
		return true
	case code >= 3000 && code <= 4999:
		return true
	default:
		return false
	}
}

// writeFrame writes a WebSocket frame to the connection.
func (c *WebSocketConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Apply per-frame write deadline so a slow client cannot pin the
	// writePump goroutine indefinitely. A negative WriteTimeout disables
	// the deadline (opt-in by callers that know what they're doing).
	if c.config.WriteTimeout > 0 {
		if sd, ok := c.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = sd.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
			defer sd.SetWriteDeadline(time.Time{})
		}
	}

	var buf []byte
	length := len(payload)

	// First byte: FIN + opcode
	buf = append(buf, 0x80|opcode)

	// Length
	if length <= 125 {
		buf = append(buf, byte(length))
	} else if length <= 65535 {
		buf = append(buf, 126)
		buf = append(buf, byte(length>>8), byte(length))
	} else {
		buf = append(buf, 127)
		for i := 7; i >= 0; i-- {
			buf = append(buf, byte(length>>(i*8)))
		}
	}

	// Server frames are unmasked
	buf = append(buf, payload...)

	_, err := c.conn.Write(buf)
	return err
}

// readFrame reads a WebSocket frame from the connection. It iterates
// over inline control frames (ping/pong) rather than recursing so a
// peer cannot blow the goroutine stack with a control-frame flood.
func (c *WebSocketConn) readFrame() (*wsFrame, error) {
	readLimit := c.config.ReadLimit
	if readLimit <= 0 {
		readLimit = 1 << 20
	}

	// Fragmented-message accumulator (RFC 6455 §5.4).
	var fragOpcode byte
	var fragBuf []byte

	for {
		// Read first 2 bytes
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.conn, header); err != nil {
			return nil, err
		}

		fin := (header[0] & 0x80) != 0
		rsv := header[0] & 0x70
		opcode := header[0] & 0x0F
		masked := (header[1] & 0x80) != 0
		length := uint64(header[1] & 0x7F)
		isControl := opcode >= 0x8

		// RFC 6455 §5.2: RSV1/2/3 MUST be 0 unless an extension was
		// negotiated. We negotiate no extensions, so any non-zero RSV
		// is a protocol error.
		if rsv != 0 {
			return nil, errors.New("stream: protocol error: reserved bits set")
		}

		// RFC 6455 §5.5: control frames MUST NOT be fragmented (FIN=1).
		if isControl && !fin {
			return nil, errors.New("stream: protocol error: fragmented control frame")
		}

		// Extended length
		if length == 126 {
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.conn, ext); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext))
		} else if length == 127 {
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.conn, ext); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext)
		}

		// RFC 6455: control frames MUST be <=125 bytes and not fragmented.
		if isControl && length > 125 {
			return nil, errors.New("stream: protocol error: oversized control frame")
		}

		// Compare as uint64 so a top-bit-set length cannot wrap negative
		// and bypass the read limit.
		if length > uint64(readLimit) {
			return nil, fmt.Errorf("stream: message too large (%d > %d)", length, readLimit)
		}

		// RFC 6455: client-to-server frames MUST be masked. Optional
		// enforcement so existing tests that craft unmasked frames keep
		// working until they explicitly opt in.
		if c.config.requireMask && !masked {
			return nil, errors.New("stream: protocol error: client frame must be masked")
		}

		// Masking key
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.conn, mask[:]); err != nil {
				return nil, err
			}
		}

		// Payload.
		//
		// Do NOT allocate `length` up front: the peer declares it in a
		// handful of header bytes, so an eager make() lets 12 bytes on
		// the wire pin ReadLimit (1 MiB by default) of heap per socket.
		// Grow while reading instead, so memory tracks bytes actually
		// delivered. A read deadline bounds the stall: net/http no
		// longer manages this connection after Hijack, and
		// lastReadActivity only advances on a COMPLETE frame, so
		// without one a half-sent frame holds its buffer until the
		// keepalive ping eventually tears the connection down.
		var payload []byte
		if length > 0 {
			c.setReadDeadline(c.config.ReadIdleTimeout)
			payload = make([]byte, 0, minInt(int(length), wsPayloadChunk))
			buf := make([]byte, minInt(int(length), wsPayloadChunk))
			for uint64(len(payload)) < length {
				want := minInt(int(length-uint64(len(payload))), len(buf))
				n, err := io.ReadFull(c.conn, buf[:want])
				if err != nil {
					c.clearReadDeadline()
					return nil, err
				}
				payload = append(payload, buf[:n]...)
			}
			c.clearReadDeadline()
		}

		// Unmask
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		// Any successful frame counts as activity for the keepalive clock.
		c.lastReadActivity.Store(time.Now().UnixNano())

		// Handle control frames inline (no recursion)
		switch opcode {
		case wsopcodeClose:
			// Capture the peer's status code so Close() can echo it, but
			// sanitize it first: a peer (or a hostile client) may send a
			// reserved/never-on-wire code, a sub-1000 code, or an illegal
			// 1-byte body. We never echo those verbatim: wsEchoableCloseCode
			// decides what is safe; the rest are replaced by 1002 (protocol
			// error) per RFC 6455 §7.4.1. peerClosePayload thus only ever
			// holds echoable bytes (or is empty), and Close() stays dumb.
			//
			// A bodyless Close (1005 = no status) stays internal: capture
			// empty so Close() writes an empty payload, which is legal.
			var status []byte
			switch {
			case len(payload) == 0:
				// No status; echo bodyless, exactly as before.
			case len(payload) >= 2:
				if wsEchoableCloseCode(binary.BigEndian.Uint16(payload[:2])) {
					status = []byte{payload[0], payload[1]}
				} else {
					status = []byte{0x03, 0xEA} // 1002 protocol error
				}
			default:
				// len(payload) == 1: illegal per §5.5.1 (a body's first
				// two bytes are the status). Echo 1002, not the lone byte.
				status = []byte{0x03, 0xEA}
			}
			c.peerClosePayload.Store(&status)
			// Signal Close() so its handshake wait can return immediately
			// instead of timing out. Idempotent under closeOnce semantics.
			c.peerCloseOnce.Do(func() { close(c.peerClosed) })
			return nil, io.EOF
		case wsopcodePing:
			if err := c.writeFrame(wsopcodePong, payload); err != nil {
				return nil, err
			}
			continue
		case wsopcodePong:
			// Clear the awaiting-pong flag so keepalive doesn't trip.
			c.awaitingPong.Store(false)
			continue
		}

		// RFC 6455 §5.4 message assembly. `fin` was previously read and
		// then used only for the control-frame check, so a TEXT frame
		// with FIN=0 returned HALF A MESSAGE to the application as if
		// it were whole, and browsers fragment large sends, so that
		// fired on legitimate traffic. ReadLimit is also documented as
		// a *message* bound, which only holds once fragments accumulate.
		switch opcode {
		case wsopcodeContinuation:
			if fragOpcode == 0 {
				return nil, errors.New("stream: protocol error: continuation frame with nothing to continue")
			}
			if uint64(len(fragBuf))+uint64(len(payload)) > uint64(readLimit) {
				return nil, fmt.Errorf("stream: message too large (> %d)", readLimit)
			}
			fragBuf = append(fragBuf, payload...)
			if !fin {
				continue
			}
			op, msg := fragOpcode, fragBuf
			fragOpcode, fragBuf = 0, nil
			return &wsFrame{opcode: op, payload: msg}, nil
		case wsopcodeText, wsopcodeBinary:
			if fragOpcode != 0 {
				return nil, errors.New("stream: protocol error: new data frame while a fragmented message is open")
			}
			if !fin {
				fragOpcode, fragBuf = opcode, append([]byte(nil), payload...)
				continue
			}
			return &wsFrame{opcode: opcode, payload: payload}, nil
		default:
			// Reserved opcodes (0x3-0x7, 0xB-0xF) were previously
			// delivered to the application as ordinary data frames.
			return nil, fmt.Errorf("stream: protocol error: reserved opcode 0x%X", opcode)
		}
	}
}

// setReadDeadline bounds a single frame-payload read. c.conn is an
// io.ReadWriteCloser so tests can inject fakes, so the deadline is
// best-effort: a fake without SetReadDeadline simply gets none.
func (c *WebSocketConn) setReadDeadline(d time.Duration) {
	if d <= 0 {
		return
	}
	if nc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = nc.SetReadDeadline(time.Now().Add(d))
	}
}

// clearReadDeadline removes a deadline set by setReadDeadline.
func (c *WebSocketConn) clearReadDeadline() {
	if nc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = nc.SetReadDeadline(time.Time{})
	}
}

// wsPayloadChunk bounds how much is allocated per read pass, so a
// declared length cannot commit memory ahead of delivered bytes.
const wsPayloadChunk = 32 << 10

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// headerHasToken reports whether a comma-separated header value
// contains token (case-insensitive), e.g. "keep-alive, Upgrade".
func headerHasToken(value, token string) bool {
	for part := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// computeAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func computeAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// ErrClosed is returned when writing to a closed connection. It is a
// plain errors.New sentinel so callers can compare with errors.Is.
var ErrClosed = errors.New("stream: connection closed")

// compile-time assertion that net.Conn implements SetWriteDeadline so
// the writeFrame interface upgrade is valid for real network conns.
var _ interface {
	SetWriteDeadline(time.Time) error
} = (net.Conn)(nil)
