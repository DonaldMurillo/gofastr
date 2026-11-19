package stream

import (
	"context"
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
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// WebSocketConn is a server-side RFC 6455 connection over a hijacked HTTP
// connection, with no external dependency. Its read pump owns every
// socket read and answers control frames whether or not the application
// calls Read.
type WebSocketConn struct {
	conn       io.ReadWriteCloser
	mu         sync.Mutex
	sendBuffer chan wsMessage
	closed     chan struct{}
	closeOnce  sync.Once
	onClose    []func()
	config     WSConfig

	connectionID string

	lastReadActivity atomic.Int64

	awaitingPong atomic.Bool
	pingSentAt   atomic.Int64

	ctrlTokens float64
	ctrlLast   time.Time

	peerClosed       chan struct{}
	peerCloseOnce    sync.Once
	peerClosePayload atomic.Pointer[[]byte]
	ownClosePayload  atomic.Pointer[[]byte]
	closing          atomic.Bool

	takeMu sync.Mutex

	admitMu   sync.Mutex
	admitting sync.WaitGroup
	closeBy   atomic.Int64

	writeFailed atomic.Bool

	readMsgs       chan []byte
	readDone       chan struct{}
	readErr        error
	handoffPending atomic.Bool
	readCalled     atomic.Bool
	discardUnread  atomic.Bool

	closeSent bool
}

// WSConfig configures the WebSocket connection.
type WSConfig struct {
	// ReadLimit is the maximum message size in bytes. 0 = default 1MB.
	ReadLimit int64

	// SendBuffer is the number of messages that can be buffered before
	// Write blocks. 0 or negative = 32.
	SendBuffer int

	// WriteTimeout bounds each frame write. 0 = 10s; negative disables it.
	WriteTimeout time.Duration

	// CheckOrigin reports whether the Origin header is acceptable. Nil means
	// same-origin: the Origin host must equal the request Host, and a request
	// with no Origin passes.
	CheckOrigin func(*http.Request) bool

	requireMask bool

	// ReadIdleTimeout is the read silence after which keepalive sends a Ping.
	// 0 = 60s; negative disables keepalive.
	ReadIdleTimeout time.Duration

	// PongTimeout is how long a Ping waits for its Pong before the connection
	// closes. 0 = 10s; negative disables the check.
	PongTimeout time.Duration

	// MaxControlFramesPerSecond caps the Pings and Pongs a client sends, with
	// a burst of twice the rate; past it the connection closes with 1008.
	// 0 = 32; negative disables it.
	MaxControlFramesPerSecond int

	// CloseTimeout caps how long Close waits for the peer's Close frame.
	// 0 = 1s.
	CloseTimeout time.Duration

	// Subprotocols is the server's subprotocols in preference order; Upgrade
	// echoes the first one the client offered, or none.
	Subprotocols []string

	// ConnectionID names the connection in logs. Empty means Upgrade mints a
	// random one.
	ConnectionID string

	// OnClose is called when the connection closes.
	OnClose func()
}

// CheckUpgradeRequest reports why r is not an opening handshake RFC 6455
// §4.2.1 accepts, or nil. originOK decides the Origin as
// WSConfig.CheckOrigin does.
func CheckUpgradeRequest(r *http.Request, originOK func(*http.Request) bool) error {
	if r.Method != http.MethodGet {
		return errors.New("stream: websocket upgrade must be GET")
	}
	if !r.ProtoAtLeast(1, 1) {
		return errors.New("stream: websocket upgrade requires HTTP/1.1 or later")
	}
	if !headerHasToken(r.Header.Values("Upgrade"), "websocket") {
		return errors.New("stream: not a websocket upgrade request")
	}

	if !checkOrigin(r, originOK) {
		return errors.New("stream: cross-origin websocket upgrade rejected (set WSConfig.CheckOrigin to allow)")
	}
	if !headerHasToken(r.Header.Values("Connection"), "upgrade") {
		return errors.New("stream: missing Connection: Upgrade")
	}
	if v := r.Header.Get("Sec-WebSocket-Version"); v != "13" {
		return fmt.Errorf("stream: unsupported websocket version %q (want 13)", v)
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return errors.New("stream: missing Sec-WebSocket-Key")
	}
	if kb, err := base64.StdEncoding.DecodeString(key); err != nil || len(kb) != 16 {
		return errors.New("stream: malformed Sec-WebSocket-Key")
	}
	return nil
}

type upgradeHookKey struct{}

// WithUpgradeHook returns a copy of ctx under which Upgrade hands fn each
// connection it opens before returning it.
func WithUpgradeHook(ctx context.Context, fn func(*WebSocketConn)) context.Context {
	return context.WithValue(ctx, upgradeHookKey{}, fn)
}

// Upgrade checks the handshake, hijacks the connection, writes the 101
// response and returns the running connection.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg WSConfig) (*WebSocketConn, error) {
	if err := CheckUpgradeRequest(r, cfg.CheckOrigin); err != nil {
		return nil, err
	}
	key := r.Header.Get("Sec-WebSocket-Key")

	acceptKey := computeAcceptKey(key)

	sendBuf := cfg.SendBuffer
	if sendBuf <= 0 {
		sendBuf = 32
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("stream: response writer does not support hijacking")
	}

	hijackConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("stream: hijack failed: %w", err)
	}
	handshakeTimeout := cfg.WriteTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultWriteTimeout
	}
	if err := hijackConn.SetWriteDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		hijackConn.Close()
		return nil, fmt.Errorf("stream: set handshake deadline: %w", err)
	}

	var conn io.ReadWriteCloser = hijackConn
	if bufrw != nil {
		if err := bufrw.Flush(); err != nil {
			hijackConn.Close()
			return nil, fmt.Errorf("stream: flush hijacked writer: %w", err)
		}
		if n := bufrw.Reader.Buffered(); n > 0 {
			prefix := make([]byte, n)
			if _, err := io.ReadFull(bufrw.Reader, prefix); err != nil {
				hijackConn.Close()
				return nil, fmt.Errorf("stream: drain hijacked reader: %w", err)
			}
			conn = &prefixConn{conn: hijackConn, prefix: prefix}
		}
	}

	subprotoHeader := ""
	if chosen := pickSubprotocol(r, cfg.Subprotocols); chosen != "" {
		subprotoHeader = "Sec-WebSocket-Protocol: " + chosen + "\r\n"
	}

	upgradeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n" +
		subprotoHeader + "\r\n"
	if _, err := conn.Write([]byte(upgradeResp)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("stream: write upgrade response: %w", err)
	}
	if err := hijackConn.SetWriteDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("stream: clear handshake deadline: %w", err)
	}

	readLimit := cfg.ReadLimit
	if readLimit == 0 {
		readLimit = 1 << 20
	}
	if cfg.ReadIdleTimeout == 0 {
		cfg.ReadIdleTimeout = 60 * time.Second
	}
	if cfg.PongTimeout == 0 {
		cfg.PongTimeout = 10 * time.Second
	}
	if cfg.CloseTimeout == 0 {
		cfg.CloseTimeout = 1 * time.Second
	}
	if cfg.MaxControlFramesPerSecond == 0 {
		cfg.MaxControlFramesPerSecond = defaultControlFramesPerSecond
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}

	cfg.requireMask = true
	cfg.ReadLimit = readLimit
	cfg.SendBuffer = sendBuf

	connectionID := cfg.ConnectionID
	if connectionID == "" {
		connectionID = randomConnectionID()
	}

	wsc := &WebSocketConn{
		conn:         conn,
		sendBuffer:   make(chan wsMessage, sendBuf),
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

	go wsc.writePump()
	wsc.startKeepalive()
	wsc.startReadPump()

	if hook, ok := r.Context().Value(upgradeHookKey{}).(func(*WebSocketConn)); ok && hook != nil {
		hook(wsc)
	}
	return wsc, nil
}

type prefixConn struct {
	conn   io.ReadWriteCloser
	prefix []byte
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.conn.Read(b)
}

func (p *prefixConn) Write(b []byte) (int, error) { return p.conn.Write(b) }
func (p *prefixConn) Close() error                { return p.conn.Close() }

func (p *prefixConn) SetReadDeadline(t time.Time) error {
	if nc, ok := p.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		return nc.SetReadDeadline(t)
	}
	return nil
}

func (p *prefixConn) SetWriteDeadline(t time.Time) error {
	if nc, ok := p.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return nc.SetWriteDeadline(t)
	}
	return nil
}

func randomConnectionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("stream: crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b[:])
}

func pickSubprotocol(r *http.Request, serverPrefs []string) string {
	if len(serverPrefs) == 0 {
		return ""
	}
	offered := make(map[string]struct{})
	for _, raw := range r.Header.Values("Sec-WebSocket-Protocol") {
		for p := range strings.SplitSeq(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				offered[p] = struct{}{}
			}
		}
	}
	if len(offered) == 0 {
		return ""
	}
	for _, p := range serverPrefs {
		if _, ok := offered[p]; ok {
			return p
		}
	}
	return ""
}

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

// Write queues a text message, blocking while the send queue is full. It
// returns ErrInvalidUTF8 for data that is not valid UTF-8 and ErrClosed
// once Close has started. A nil return means queued, not yet written.
func (c *WebSocketConn) Write(data []byte) error {
	if !utf8.Valid(data) {
		return ErrInvalidUTF8
	}
	return c.enqueue(wsMessage{opcode: wsopcodeText, data: data})
}

// WriteString is Write for a string.
func (c *WebSocketConn) WriteString(data string) error {
	return c.Write([]byte(data))
}

// WriteBinary queues a binary message on Write's queue, with no UTF-8
// check.
func (c *WebSocketConn) WriteBinary(data []byte) error {
	return c.enqueue(wsMessage{opcode: wsopcodeBinary, data: data})
}

func (c *WebSocketConn) enqueue(msg wsMessage) error {
	c.admitMu.Lock()
	select {
	case <-c.closed:
		c.admitMu.Unlock()
		return ErrClosed
	default:
	}
	c.admitting.Add(1)
	c.admitMu.Unlock()
	defer c.admitting.Done()
	select {
	case <-c.closed:
		return ErrClosed
	case c.sendBuffer <- msg:
		return nil
	}
}

type wsMessage struct {
	opcode byte
	data   []byte
}

func textMessage(data []byte) (wsMessage, error) {
	if !utf8.Valid(data) {
		return wsMessage{}, ErrInvalidUTF8
	}
	return wsMessage{opcode: wsopcodeText, data: data}, nil
}

// Read returns the next complete text or binary message, without its
// opcode. Messages the pump decoded before a terminal error are returned
// before the error. An application that reads must take a message at
// least once per ReadIdleTimeout+PongTimeout while the peer sends, or the
// connection closes.
func (c *WebSocketConn) Read() ([]byte, error) {
	c.readCalled.Store(true)
	select {
	case msg := <-c.readMsgs:
		return msg, nil
	case <-c.readDone:
		select {
		case msg := <-c.readMsgs:
			return msg, nil
		default:
		}
		return nil, c.readErr
	}
}

// Close stops accepting writes, writes the messages already queued within
// WriteTimeout (10s when unset), sends a Close frame and waits up to
// CloseTimeout for the peer's. A Close the peer started is echoed with its
// status code, or 1002 when that code may not be echoed. Safe to call
// twice.
func (c *WebSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		by := time.Now().Add(c.drainBound())
		c.closeBy.Store(by.UnixNano())
		c.admitMu.Lock()
		close(c.closed)
		c.admitMu.Unlock()
		if sd, ok := c.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = sd.SetWriteDeadline(by)
		}
		c.admitting.Wait()
		c.takeMu.Lock()
		if c.peerClosePayload.Load() == nil && !c.writeFailed.Load() {
			c.drainQueued(by)
		}

		echo := c.peerClosePayload.Load()
		var payload []byte
		if echo != nil {
			payload = *echo
		} else if own := c.ownClosePayload.Load(); own != nil {
			payload = *own
		}
		_ = c.writeFrameBy(wsopcodeClose, payload, time.Now().Add(c.drainBound()))
		c.takeMu.Unlock()

		c.awaitPeerClose()

		c.mu.Lock()
		callbacks := append([]func(){}, c.onClose...)
		c.onClose = nil
		c.mu.Unlock()

		if closer, ok := c.conn.(interface{ Close() error }); ok {
			err = closer.Close()
		}
		for _, fn := range callbacks {
			go func(fn func()) {
				defer func() {
					if rec := recover(); rec != nil {
						slog.Default().Error("stream: websocket close hook panicked", "panic", textsafe.Recovered(rec))
					}
				}()
				fn()
			}(fn)
		}
	})
	return err
}

// CloseWithStatus is Close with a status code and reason (printable ASCII,
// at most 123 bytes). A reserved or out-of-range code is sent as 1002 and
// returned as an error.
func (c *WebSocketConn) CloseWithStatus(code uint16, reason string) error {
	var codeErr error
	if !wsEchoableCloseCode(code) {
		codeErr = fmt.Errorf("stream: close code %d is reserved or out of range", code)
		code = 1002
	}
	c.setOwnClose(code, reason)
	if err := c.Close(); err != nil {
		return err
	}
	return codeErr
}

func (c *WebSocketConn) setOwnClose(code uint16, reason string) {
	buf := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(buf, code)
	for i := 0; i < len(reason) && len(buf) < 125; i++ {
		if b := reason[i]; b >= 0x20 && b <= 0x7e {
			buf = append(buf, b)
		}
	}
	c.ownClosePayload.Store(&buf)
}

func (c *WebSocketConn) allowControl(now time.Time) bool {
	rate := float64(c.config.MaxControlFramesPerSecond)
	if rate <= 0 {
		return true
	}
	if c.ctrlLast.IsZero() {
		c.ctrlTokens = 2 * rate
	} else if elapsed := now.Sub(c.ctrlLast).Seconds(); elapsed > 0 {
		c.ctrlTokens = min(2*rate, c.ctrlTokens+elapsed*rate)
	}
	c.ctrlLast = now
	if c.ctrlTokens < 1 {
		return false
	}
	c.ctrlTokens--
	return true
}

func (c *WebSocketConn) awaitPeerClose() {
	timeout := c.config.CloseTimeout
	if timeout <= 0 {
		timeout = 1 * time.Second
	}
	select {
	case <-c.peerClosed:
	case <-c.readDone:
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

// ConnectionID returns WSConfig.ConnectionID or the id Upgrade minted. A
// reconnect is a new connection with a new id.
func (c *WebSocketConn) ConnectionID() string {
	return c.connectionID
}

func (c *WebSocketConn) startKeepalive() {
	if c.config.ReadIdleTimeout <= 0 {
		return
	}
	c.lastReadActivity.CompareAndSwap(0, time.Now().UnixNano())
	go c.keepalive()
}

func (c *WebSocketConn) keepalive() {
	idle := c.config.ReadIdleTimeout
	pongTimeout := c.config.PongTimeout
	if pongTimeout == 0 {
		pongTimeout = 10 * time.Second
	}
	tick := idle / 4
	if pongTimeout > 0 {
		tick = min(tick, pongTimeout/4)
	}
	tick = max(tick, 10*time.Millisecond)
	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-c.closed:
			return
		case now := <-t.C:
			if c.handoffPending.Load() {
				continue
			}
			lastRead := time.Unix(0, c.lastReadActivity.Load())
			if c.awaitingPong.Load() {
				sent := time.Unix(0, c.pingSentAt.Load())
				if pongTimeout > 0 && now.Sub(sent) > pongTimeout {
					c.Close()
					return
				}
				continue
			}
			if now.Sub(lastRead) >= idle {
				c.awaitingPong.Store(true)
				if err := c.writeFrame(wsopcodePing, nil); err != nil {
					c.Close()
					return
				}
				c.pingSentAt.Store(time.Now().UnixNano())
			}
		}
	}
}

func (c *WebSocketConn) writePump() {
	for {
		c.takeMu.Lock()
		done, err := c.pumpOne()
		c.takeMu.Unlock()
		if !done {
			continue
		}
		if err != nil && !errors.Is(err, ErrClosed) {
			c.writeFailed.Store(true)
			c.Close()
		}
		return
	}
}

func (c *WebSocketConn) pumpOne() (done bool, err error) {
	select {
	case <-c.closed:
		return true, nil
	case msg := <-c.sendBuffer:
		if err := c.writeFrame(msg.opcode, msg.data); err != nil {
			return true, err
		}
		return false, nil
	}
}

func (c *WebSocketConn) drainQueued(by time.Time) {
	for time.Now().Before(by) {
		select {
		case msg := <-c.sendBuffer:
			if err := c.writeFrameBy(msg.opcode, msg.data, by); err != nil {
				return
			}
		default:
			return
		}
	}
}

func (c *WebSocketConn) drainBound() time.Duration {
	if c.config.WriteTimeout > 0 {
		return c.config.WriteTimeout
	}
	return defaultWriteTimeout
}

const defaultWriteTimeout = 10 * time.Second

func (c *WebSocketConn) startReadPump() {
	go c.readPump()
}

func (c *WebSocketConn) readPump() {
	for {
		frame, err := c.readFrame()
		if err != nil {
			c.readErr = err
			close(c.readDone)
			c.Close()
			return
		}
		c.handoff(frame.payload)
	}
}

func (c *WebSocketConn) handoff(msg []byte) {
	select {
	case c.readMsgs <- msg:
		return
	default:
	}
	if c.discardUnread.Load() && !c.readCalled.Load() {
		return
	}
	c.handoffPending.Store(true)
	defer c.endStall()
	var stall <-chan time.Time
	if limit := c.handoffStallLimit(); limit > 0 {
		t := time.NewTimer(limit)
		defer t.Stop()
		stall = t.C
	}
	select {
	case c.readMsgs <- msg:
		return
	case <-c.closed:
		return
	case <-stall:
	}
	if !c.readCalled.Load() {
		c.discardUnread.Store(true)
		return
	}
	go c.Close()
	select {
	case c.readMsgs <- msg:
	case <-c.closed:
	}
}

func (c *WebSocketConn) endStall() {
	if c.awaitingPong.Load() {
		c.pingSentAt.Store(time.Now().UnixNano())
	}
	c.handoffPending.Store(false)
}

func (c *WebSocketConn) handoffStallLimit() time.Duration {
	pong := c.config.PongTimeout
	if pong == 0 {
		pong = 10 * time.Second
	}
	if c.config.ReadIdleTimeout <= 0 || pong < 0 {
		return 0
	}
	return c.config.ReadIdleTimeout + pong
}

const defaultControlFramesPerSecond = 32

const (
	wsopcodeContinuation = 0x0
	wsopcodeText         = 0x1
	wsopcodeBinary       = 0x2
	wsopcodeClose        = 0x8
	wsopcodePing         = 0x9
	wsopcodePong         = 0xA
)

type wsFrame struct {
	opcode  byte
	payload []byte
}

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

func (c *WebSocketConn) writeFrame(opcode byte, payload []byte) error {
	return c.writeFrameBy(opcode, payload, time.Time{})
}

func (c *WebSocketConn) writeFrameBy(opcode byte, payload []byte, by time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeSent {
		return ErrClosed
	}
	if opcode == wsopcodeClose {
		c.closeSent = true
	}

	if cb := c.closeBy.Load(); by.IsZero() && cb != 0 {
		by = time.Unix(0, cb)
	}
	if by.IsZero() && c.config.WriteTimeout > 0 {
		by = time.Now().Add(c.config.WriteTimeout)
	}
	if !by.IsZero() {
		if sd, ok := c.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = sd.SetWriteDeadline(by)
			defer sd.SetWriteDeadline(time.Time{})
		}
	}

	var buf []byte
	length := len(payload)

	buf = append(buf, 0x80|opcode)

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

	buf = append(buf, payload...)

	_, err := c.conn.Write(buf)
	return err
}

func (c *WebSocketConn) readFrame() (*wsFrame, error) {
	readLimit := c.config.ReadLimit
	if readLimit <= 0 {
		readLimit = 1 << 20
	}

	var fragOpcode byte
	var fragBuf []byte

	for {
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

		if rsv != 0 {
			return nil, c.failConn(1002, "stream: protocol error: reserved bits set")
		}

		if isControl && !fin {
			return nil, c.failConn(1002, "stream: protocol error: fragmented control frame")
		}

		if length == 126 {
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.conn, ext); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext))
			if length < 126 {
				return nil, c.failConn(1002, "stream: protocol error: non-minimal 16-bit payload length")
			}
		} else if length == 127 {
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.conn, ext); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext)
			if length <= 0xFFFF {
				return nil, c.failConn(1002, "stream: protocol error: non-minimal 64-bit payload length")
			}
		}

		if isControl && length > 125 {
			return nil, c.failConn(1002, "stream: protocol error: oversized control frame")
		}

		if length > uint64(readLimit) {
			return nil, c.failConn(1009, fmt.Sprintf("stream: message too large (%d > %d)", length, readLimit))
		}

		if c.config.requireMask && !masked {
			return nil, c.failConn(1002, "stream: protocol error: client frame must be masked")
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.conn, mask[:]); err != nil {
				return nil, err
			}
		}

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

		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		c.lastReadActivity.Store(time.Now().UnixNano())

		if (opcode == wsopcodePing || opcode == wsopcodePong) && !c.allowControl(time.Now()) {
			c.setOwnClose(1008, "control frame rate")
			return nil, errors.New("stream: control frame rate exceeded")
		}

		switch opcode {
		case wsopcodeClose:
			var status []byte
			switch {
			case len(payload) == 0:
			case len(payload) >= 2:
				if wsEchoableCloseCode(binary.BigEndian.Uint16(payload[:2])) && utf8.Valid(payload[2:]) {
					status = []byte{payload[0], payload[1]}
				} else {
					status = []byte{0x03, 0xEA}
				}
			default:
				status = []byte{0x03, 0xEA}
			}
			c.peerClosePayload.Store(&status)
			c.peerCloseOnce.Do(func() { close(c.peerClosed) })
			return nil, io.EOF
		case wsopcodePing:
			if err := c.writeFrame(wsopcodePong, payload); err != nil && !errors.Is(err, ErrClosed) {
				return nil, err
			}
			continue
		case wsopcodePong:
			c.awaitingPong.Store(false)
			continue
		}

		switch opcode {
		case wsopcodeContinuation:
			if fragOpcode == 0 {
				return nil, c.failConn(1002, "stream: protocol error: continuation frame with nothing to continue")
			}
			if uint64(len(fragBuf))+uint64(len(payload)) > uint64(readLimit) {
				return nil, c.failConn(1009, fmt.Sprintf("stream: message too large (> %d)", readLimit))
			}
			fragBuf = append(fragBuf, payload...)
			if !fin {
				continue
			}
			op, msg := fragOpcode, fragBuf
			fragOpcode, fragBuf = 0, nil
			return c.dataMessage(op, msg)
		case wsopcodeText, wsopcodeBinary:
			if fragOpcode != 0 {
				return nil, c.failConn(1002, "stream: protocol error: new data frame while a fragmented message is open")
			}
			if !fin {
				fragOpcode, fragBuf = opcode, append([]byte(nil), payload...)
				continue
			}
			return c.dataMessage(opcode, payload)
		default:
			return nil, c.failConn(1002, fmt.Sprintf("stream: protocol error: reserved opcode 0x%X", opcode))
		}
	}
}

func (c *WebSocketConn) dataMessage(opcode byte, payload []byte) (*wsFrame, error) {
	if opcode == wsopcodeText && !utf8.Valid(payload) {
		return nil, c.failConn(1007, "stream: text message is not valid UTF-8")
	}
	return &wsFrame{opcode: opcode, payload: payload}, nil
}

func (c *WebSocketConn) failConn(code uint16, msg string) error {
	status := binary.BigEndian.AppendUint16(nil, code)
	c.ownClosePayload.CompareAndSwap(nil, &status)
	return errors.New(msg)
}

func (c *WebSocketConn) setReadDeadline(d time.Duration) {
	if d <= 0 {
		return
	}
	if nc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = nc.SetReadDeadline(time.Now().Add(d))
	}
}

func (c *WebSocketConn) clearReadDeadline() {
	if nc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = nc.SetReadDeadline(time.Time{})
	}
}

const wsPayloadChunk = 32 << 10

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func headerHasToken(values []string, token string) bool {
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func computeAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// ErrClosed is returned by a write to a closed connection.
var ErrClosed = errors.New("stream: connection closed")

// ErrInvalidUTF8 is returned when a text message is not valid UTF-8.
var ErrInvalidUTF8 = errors.New("stream: text message is not valid UTF-8")

var _ interface {
	SetWriteDeadline(time.Time) error
} = (net.Conn)(nil)
