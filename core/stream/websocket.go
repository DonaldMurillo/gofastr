package stream

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
// Backpressure: writes block when the send buffer is full. The caller
// controls the read loop.
type WebSocketConn struct {
	conn       io.ReadWriteCloser
	mu         sync.Mutex
	sendBuffer chan []byte
	closed     chan struct{}
	closeOnce  sync.Once
	onClose    []func()
	config     WSConfig

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
	// closing handshake is signal-driven — no parallel byte-scan, no
	// reader race. peerCloseOnce protects the close-of-channel from
	// concurrent readers.
	peerClosed    chan struct{}
	peerCloseOnce sync.Once
	// peerClosePayload stores the first 2 bytes of the peer's Close
	// frame payload (status code) so Close() can echo it back per RFC
	// 6455 §5.5.1. Stored as a *[]byte so we can distinguish "no Close
	// seen yet" (nil) from "Close with empty payload" (pointer to empty).
	peerClosePayload atomic.Pointer[[]byte]
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

	// OnClose is called when the connection closes.
	OnClose func()
}

// Upgrade upgrades an HTTP connection to a simple WebSocket.
// Performs the HTTP upgrade handshake and returns a managed connection.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg WSConfig) (*WebSocketConn, error) {
	// Validate WebSocket upgrade request
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("stream: not a websocket upgrade request")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("stream: missing Sec-WebSocket-Key")
	}

	// Origin check — block CSWSH by default. Caller may opt in to
	// cross-origin via cfg.CheckOrigin.
	if !checkOrigin(r, cfg.CheckOrigin) {
		return nil, errors.New("stream: cross-origin websocket upgrade rejected (set WSConfig.CheckOrigin to allow)")
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

	wsc := &WebSocketConn{
		conn:       conn,
		sendBuffer: make(chan []byte, sendBuf),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		config:     cfg,
	}
	wsc.lastReadActivity.Store(time.Now().UnixNano())

	if cfg.OnClose != nil {
		wsc.onClose = append(wsc.onClose, cfg.OnClose)
	}

	// Start the write pump and keepalive
	go wsc.writePump()
	wsc.startKeepalive()

	return wsc, nil
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
	for _, p := range strings.Split(raw, ",") {
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

// Read reads a message from the client. Blocks until a message arrives
// or the connection closes.
func (c *WebSocketConn) Read() ([]byte, error) {
	frame, err := c.readFrame()
	if err != nil {
		c.Close()
		return nil, err
	}
	return frame.payload, nil
}

// Close closes the WebSocket connection. Safe to call multiple times.
// Performs the RFC 6455 closing handshake: sends a Close frame, then
// waits up to CloseTimeout for the peer's reciprocal Close before tearing
// down the underlying TCP connection. This avoids the abnormal 1006
// close code on the peer side.
//
// If the peer initiated the close, the echo Close frame preserves the
// peer's 2-byte status code per RFC 6455 §5.5.1. Otherwise we send an
// empty Close payload (status 1000 implied by absence).
func (c *WebSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// If readFrame already captured a peer Close payload, echo the
		// peer's status code back. Otherwise send an empty Close.
		echo := c.peerClosePayload.Load()
		var payload []byte
		if echo != nil {
			payload = *echo
		}
		// Send close frame. Ignore the error — if writing fails the peer
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
			go fn()
		}
	})
	return err
}

// awaitPeerClose waits for the active readFrame goroutine to signal that
// it parsed the peer's reciprocal Close frame, or for CloseTimeout to
// elapse. Signal-driven so we never race the existing reader on the same
// TCP stream. If the peer already sent Close before our Close() was
// invoked (the common responder path), peerClosed is already closed and
// this returns immediately.
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
	tick := idle / 4
	if pongTimeout/4 < tick {
		tick = pongTimeout / 4
	}
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-c.closed:
			return
		case now := <-t.C:
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

// WebSocket frame opcodes
const (
	wsopcodeText   = 0x1
	wsopcodeBinary = 0x2
	wsopcodeClose  = 0x8
	wsopcodePing   = 0x9
	wsopcodePong   = 0xA
)

// wsFrame represents a parsed WebSocket frame.
type wsFrame struct {
	opcode  byte
	payload []byte
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

		// Payload
		payload := make([]byte, length)
		if length > 0 {
			if _, err := io.ReadFull(c.conn, payload); err != nil {
				return nil, err
			}
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
			// Capture the peer's status code (first 2 bytes per RFC 6455
			// §5.5.1) so Close() can echo it. A Close with <2 payload
			// bytes is legal (1005 = no status) — capture empty in that
			// case so Close() also writes an empty payload.
			var status []byte
			if len(payload) >= 2 {
				status = []byte{payload[0], payload[1]}
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

		return &wsFrame{opcode: opcode, payload: payload}, nil
	}
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
