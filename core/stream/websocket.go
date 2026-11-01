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
	sendBuffer chan wsMessage
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

	// ctrlTokens and ctrlLast are the MaxControlFramesPerSecond bucket.
	// Only the read pump touches them.
	ctrlTokens float64
	ctrlLast   time.Time

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
	// ownClosePayload is the status code and reason CloseWithStatus
	// asked for; used only when the peer did not close first.
	ownClosePayload atomic.Pointer[[]byte]
	// closing is set by the first detached closer a StateChannel
	// spawns for this connection, so a stalled socket costs one
	// goroutine however many events pile up behind it (every later
	// Close parks on closeOnce until the first one, waiting on the
	// stalled pump, returns, so Close itself cannot be the guard).
	closing atomic.Bool

	// takeMu is held by the write pump from the moment it waits for a
	// queued message until that message is written, and by Close while
	// it drains the queue and writes the Close frame. A message the
	// pump has taken off sendBuffer is therefore always on the wire
	// before Close looks at the queue: nothing Write accepted is
	// written out of order or lost behind our Close frame.
	takeMu sync.Mutex

	// writeFailed is set by the write pump when a frame write fails: the
	// socket is dead or stalled past WriteTimeout, so Close writes no
	// queued messages to it, only the Close frame.
	writeFailed atomic.Bool

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
	// recreate the very bug the pump exists to fix. The stall itself
	// is bounded (see handoff), so the skip cannot last for ever.
	handoffPending atomic.Bool
	// readCalled is set by the first Read. A stall past the bound with
	// no Read ever made marks a push-only app (discardUnread): data it
	// never asked for is dropped so the pump keeps reading control
	// frames. A stall in an app that did read closes the connection.
	readCalled    atomic.Bool
	discardUnread atomic.Bool

	// closeSent is set, under mu, by the write that put our Close
	// frame on the wire. writeFrame refuses every frame after it, so a
	// writer queued on mu behind Close cannot follow the Close frame
	// with data (RFC 6455 §5.5.1).
	closeSent bool
}

// WSConfig configures the WebSocket connection.
type WSConfig struct {
	// ReadLimit is the maximum message size in bytes. 0 = default 1MB.
	ReadLimit int64

	// SendBuffer is the number of messages that can be buffered before
	// Write blocks. 0 or negative = 32.
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

	// MaxControlFramesPerSecond caps the Ping and Pong frames a client
	// sends, as a token bucket with a burst of twice the rate. The read
	// pump answers a Ping itself, so an application's own frame limit
	// never counts them; past the cap the connection is closed with
	// 1008 (policy violation). 0 means default 32. Set negative to
	// disable.
	MaxControlFramesPerSecond int

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

// CheckUpgradeRequest reports why r is not a WebSocket opening
// handshake RFC 6455 §4.2.1 accepts, or nil. originOK decides the Origin
// the way WSConfig.CheckOrigin does: nil means same-origin. Upgrade runs
// it before the hijack, and a transport that hijacks the connection
// itself runs it before writing its 101.
func CheckUpgradeRequest(r *http.Request, originOK func(*http.Request) bool) error {
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
		return errors.New("stream: websocket upgrade must be GET")
	}
	// §4.1: the opening handshake is an HTTP/1.1 (or later) request,
	// and RFC 9110 §7.8 has a server ignore Upgrade on HTTP/1.0. An
	// intermediary that reads the request as 1.0 forwards it as an
	// ordinary request, the desync described above.
	if !r.ProtoAtLeast(1, 1) {
		return errors.New("stream: websocket upgrade requires HTTP/1.1 or later")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return errors.New("stream: not a websocket upgrade request")
	}

	// Origin check: block CSWSH by default. Caller may opt in to
	// cross-origin via originOK. Runs before the remaining
	// shape checks so a cross-origin attempt reports the security
	// reason rather than whichever header it also happened to omit.
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
	// §4.2.1: the key is base64 of exactly 16 random bytes.
	if kb, err := base64.StdEncoding.DecodeString(key); err != nil || len(kb) != 16 {
		return errors.New("stream: malformed Sec-WebSocket-Key")
	}
	return nil
}

// upgradeHookKey carries the WithUpgradeHook callback on a request
// context.
type upgradeHookKey struct{}

// WithUpgradeHook returns a copy of ctx that makes Upgrade, serving a
// request whose context carries it, hand fn each connection it opens,
// before Upgrade returns it. A server uses it to reach the WebSockets
// its handlers own: framework.App closes each one with 1001 (going
// away) when Shutdown starts, since a hijacked connection is outside
// http.Server.Shutdown's drain.
func WithUpgradeHook(ctx context.Context, fn func(*WebSocketConn)) context.Context {
	return context.WithValue(ctx, upgradeHookKey{}, fn)
}

// Upgrade upgrades an HTTP connection to a simple WebSocket.
// Performs the HTTP upgrade handshake and returns a managed connection.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg WSConfig) (*WebSocketConn, error) {
	if err := CheckUpgradeRequest(r, cfg.CheckOrigin); err != nil {
		return nil, err
	}
	key := r.Header.Get("Sec-WebSocket-Key")

	// Compute accept key (SHA-1 of key + magic GUID)
	acceptKey := computeAcceptKey(key)

	// Settle the config before the hijack: once the socket is ours,
	// nothing below may fail without closing it, and a negative
	// SendBuffer used to reach make(chan) and panic there, leaking the
	// hijacked connection. Negative means the default, as zero does.
	sendBuf := cfg.SendBuffer
	if sendBuf <= 0 {
		sendBuf = 32
	}

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("stream: response writer does not support hijacking")
	}

	hijackConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("stream: hijack failed: %w", err)
	}

	var conn io.ReadWriteCloser = hijackConn
	// Flush any buffered response data, then drain the request reader:
	// net/http may have read past the handshake when the client pipelined
	// its first data frame into the same TCP write. Those bytes belong to
	// the websocket stream, not the HTTP request; leaving them inside the
	// hijacked *bufio.Reader orphans them — the read pump is handed the
	// raw conn and never sees the frame, so an authenticate-first client
	// silently loses its auth frame and hangs. Serve them in front of the
	// raw conn instead.
	if bufrw != nil {
		bufrw.Flush()
		if n := bufrw.Reader.Buffered(); n > 0 {
			prefix := make([]byte, n)
			if _, err := io.ReadFull(bufrw.Reader, prefix); err != nil {
				hijackConn.Close()
				return nil, fmt.Errorf("stream: drain hijacked reader: %w", err)
			}
			conn = &prefixConn{conn: hijackConn, prefix: prefix}
		}
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
	if cfg.MaxControlFramesPerSecond == 0 {
		cfg.MaxControlFramesPerSecond = defaultControlFramesPerSecond
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultWriteTimeout
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

	// Start the write pump, keepalive, and the internal read pump. The
	// read pump owns all socket reads so control frames (Ping/Pong/
	// Close) are handled even when the app never calls Read.
	go wsc.writePump()
	wsc.startKeepalive()
	wsc.startReadPump()

	if hook, ok := r.Context().Value(upgradeHookKey{}).(func(*WebSocketConn)); ok && hook != nil {
		hook(wsc)
	}
	return wsc, nil
}

// prefixConn is the read path Upgrade hands the connection when the
// hijacked request reader had already buffered bytes past the handshake:
// a client that pipelines its first data frame into the same TCP write
// as the handshake leaves them there, and the raw conn the pumps get
// cannot see behind the bufio.Reader. prefix is served first, then
// every method delegates. Only the read pump reads; writes, deadlines,
// and Close pass straight through to the underlying conn.
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

// SetReadDeadline / SetWriteDeadline forward so the deadline probes in
// readFrame and writeFrame keep working behind the prefix wrapper.
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
	// Every field line counts: a client may send its offers as one
	// comma-separated line or as several (RFC 9110 §5.3).
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
// is full (backpressure). Returns ErrClosed once Close has started.
//
// A text message must be valid UTF-8 (RFC 6455 §5.6): a browser fails
// the whole connection with 1007 on one that is not. Write checks data
// before queueing it and returns ErrInvalidUTF8, queueing nothing, when
// it is not; send arbitrary bytes with WriteBinary.
//
// A nil return means the message is queued, not yet written. Close
// writes every queued message before its Close frame, within the
// WriteTimeout bound described on Close, so a message Write accepted
// before Close reaches the peer unless the peer is too slow to take it
// in that time, the socket already failed, or the peer started the
// close. A Write running concurrently with Close may return nil for a
// message that lands after the drain and is dropped.
func (c *WebSocketConn) Write(data []byte) error {
	if !utf8.Valid(data) {
		return ErrInvalidUTF8
	}
	return c.enqueue(wsMessage{opcode: wsopcodeText, data: data})
}

// WriteString is a convenience for sending a text message. Like Write,
// it returns ErrInvalidUTF8 for a string that is not valid UTF-8.
func (c *WebSocketConn) WriteString(data string) error {
	return c.Write([]byte(data))
}

// WriteBinary sends a binary message (opcode 0x2) to the client: the
// bytes arrive exactly as given, with no UTF-8 requirement. A browser
// hands it to the page as a Blob or ArrayBuffer (per binaryType), not a
// string. It shares Write's queue, so text and binary messages reach
// the peer in the order they were written, Close's drain included, and
// it blocks and returns ErrClosed exactly as Write does.
func (c *WebSocketConn) WriteBinary(data []byte) error {
	return c.enqueue(wsMessage{opcode: wsopcodeBinary, data: data})
}

// enqueue puts one data message on the send queue, blocking while it is
// full, and refuses it with ErrClosed once Close has started.
func (c *WebSocketConn) enqueue(msg wsMessage) error {
	// Check closed first to avoid non-deterministic select
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	select {
	case <-c.closed:
		return ErrClosed
	case c.sendBuffer <- msg:
		return nil
	}
}

// wsMessage is one queued data message and the opcode it goes out
// with, so text and binary share one FIFO and keep Write's order.
type wsMessage struct {
	opcode byte
	data   []byte
}

// textMessage wraps data as a queued text message, or refuses it with
// ErrInvalidUTF8. Every path that queues text without Write (Hub,
// StateChannel) goes through it, so no text frame leaves this package
// with bytes a browser would fail the connection over.
func textMessage(data []byte) (wsMessage, error) {
	if !utf8.Valid(data) {
		return wsMessage{}, ErrInvalidUTF8
	}
	return wsMessage{opcode: wsopcodeText, data: data}, nil
}

// Read reads a complete message from the client. It consumes from the
// internal read pump's buffer (readMsgs); it never reads the socket
// directly, so the pump remains the sole owner of the wire and control
// frames stay processed even between Read calls. A push-only server
// need not call Read at all; the pump still answers Pings, clears the
// keepalive's Pong watch, and completes Close()'s handshake.
//
// Read returns text and binary messages alike, as their payload bytes,
// and does not say which opcode a message came in on. A text message
// has already been checked as valid UTF-8 (one that is not closes the
// connection with 1007), so a protocol that mixes the two must tell
// them apart in its own framing.
//
// Messages decoded by the pump before a terminal error are delivered
// before the error surfaces (drain-before-error). Concurrent Read
// callers are safe: the buffer channel serializes delivery.
//
// An app that reads must take a message at least once per
// ReadIdleTimeout+PongTimeout while the peer is sending: once the
// buffer is full the pump cannot read the peer's Pongs, so a reader
// stalled past that bound is indistinguishable from an abandoned one
// and the connection is closed. An app that never calls Read is
// push-only: past the same bound, data that does not fit the buffer
// is dropped and control frames keep flowing.
func (c *WebSocketConn) Read() ([]byte, error) {
	c.readCalled.Store(true)
	select {
	case msg := <-c.readMsgs:
		return msg, nil
	case <-c.readDone:
		// The pump's last send happens before close(readDone), so a
		// message it handed off is in the buffer now. select picks a
		// ready case at random: drain once more so the terminal error
		// never leapfrogs a decoded message.
		select {
		case msg := <-c.readMsgs:
			return msg, nil
		default:
		}
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
// Before the Close frame, Close stops accepting writes (Write returns
// ErrClosed) and writes the messages Write already queued, oldest
// first. The drain is bounded by WriteTimeout from the moment Close is
// called (10s when WriteTimeout is zero or negative), and the same bound
// is set as the socket's write deadline, so a frame the pump is still
// writing to a peer that stopped reading cannot hold Close open either;
// what is still queued when the bound passes or a write fails is
// dropped. There is no drain when the peer started the close (it
// discards data once it sent its Close frame) or a write already failed:
// the Close frame goes out at once.
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
		// Refuse new writes, then flush what Write already accepted.
		// Taking takeMu waits out the frame the pump is writing (the
		// pump stops once it sees closed), so the drain starts after
		// it and the wire keeps Write's order.
		by := time.Now().Add(c.drainBound())
		close(c.closed)
		// The frame the pump is writing carries no deadline when
		// WriteTimeout is disabled; one stalled peer would then hold
		// takeMu, and Close, forever. Cut it at the drain's bound.
		if sd, ok := c.conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = sd.SetWriteDeadline(by)
		}
		c.takeMu.Lock()
		// Drain only a Close this side started. After the peer's Close
		// frame the peer discards data (RFC 6455 5.5.1: reply as soon as
		// practical), and after a failed write the socket takes nothing.
		if c.peerClosePayload.Load() == nil && !c.writeFailed.Load() {
			c.drainQueued(by)
		}

		// If readFrame already captured a peer Close payload, echo the
		// (already-sanitized) bytes back. Otherwise send an empty Close.
		echo := c.peerClosePayload.Load()
		var payload []byte
		if echo != nil {
			payload = *echo
		} else if own := c.ownClosePayload.Load(); own != nil {
			payload = *own
		}
		// Send close frame. Ignore the error: if writing fails the peer
		// is likely already gone, but we still want to drop the TCP conn.
		_ = c.writeFrame(wsopcodeClose, payload)
		c.takeMu.Unlock()

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
						slog.Default().Error("stream: websocket close hook panicked", "panic", textsafe.Recovered(rec))
					}
				}()
				fn()
			}(fn)
		}
	})
	return err
}

// CloseWithStatus is Close with a status code and reason in the close
// frame, for a server that accepted the handshake only to refuse the
// connection: a browser cannot read an HTTP status off a failed
// handshake (the WHATWG spec withholds it so a page cannot probe the
// network), but it can read a close code. The reason is kept to
// printable ASCII and at most 123 bytes, the frame's limit. A close
// the peer initiated first still echoes the peer's own code.
func (c *WebSocketConn) CloseWithStatus(code uint16, reason string) error {
	var codeErr error
	if !wsEchoableCloseCode(code) {
		// A code RFC 6455 reserves (1004-1006, 1015) or that is out of
		// range never reaches the wire: the frame says 1002 and the
		// caller learns it asked for something illegal.
		codeErr = fmt.Errorf("stream: close code %d is reserved or out of range", code)
		code = 1002
	}
	c.setOwnClose(code, reason)
	if err := c.Close(); err != nil {
		return err
	}
	return codeErr
}

// setOwnClose records the status code and reason Close sends, the
// reason kept to printable ASCII within the frame's limit.
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

// allowControl spends one MaxControlFramesPerSecond token. A conn built
// without Upgrade (zero rate) and the negative opt-out are unlimited.
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

// awaitPeerClose waits for the read pump to signal that it parsed the
// peer's reciprocal Close frame, or that its read failed, or for
// CloseTimeout to elapse. The
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
	case <-c.readDone:
		// The connection died under the handshake (the peer vanished, or
		// Shutdown force-closed the socket): no Close frame can come.
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
	if pongTimeout == 0 {
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
			// ping goes unanswered, and PongTimeout kills it. A peer
			// that dies behind a stall is reclaimed by the pump's own
			// bound on the stall (handoff), after which these checks
			// resume or the connection is already closing.
			if c.handoffPending.Load() {
				continue
			}
			lastRead := time.Unix(0, c.lastReadActivity.Load())
			// If awaiting a pong, enforce PongTimeout.
			if c.awaitingPong.Load() {
				sent := time.Unix(0, c.pingSentAt.Load())
				if pongTimeout > 0 && now.Sub(sent) > pongTimeout {
					c.Close()
					return
				}
				continue
			}
			if now.Sub(lastRead) >= idle {
				// Send ping and start the pong clock once it is on the
				// wire: a data write can hold the lock past PongTimeout,
				// and a clock started before that wait expired before
				// the peer could answer. awaitingPong goes first so a
				// Pong that beats the Store still clears it.
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

// writePump drains the send buffer and writes WebSocket frames. It
// exits when Close starts; Close writes what is still queued.
func (c *WebSocketConn) writePump() {
	for {
		c.takeMu.Lock()
		done, err := c.pumpOne()
		c.takeMu.Unlock()
		if !done {
			continue
		}
		// ErrClosed means Close already wrote our Close frame and is
		// waiting out the peer's; a second Close would park this
		// goroutine on closeOnce for that whole wait. Close takes
		// takeMu, so it is called only after the unlock above.
		if err != nil && !errors.Is(err, ErrClosed) {
			c.writeFailed.Store(true)
			c.Close()
		}
		return
	}
}

// pumpOne waits for one queued message and writes it. done reports
// that the pump must stop: Close started, or the write failed. The
// caller holds takeMu.
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

// drainQueued writes the messages still in sendBuffer, oldest first,
// each with its own opcode, until the buffer is empty, a write fails,
// or by passes; each write carries by as its deadline. The caller holds
// takeMu.
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

// drainBound is how long Close may spend writing queued messages:
// WriteTimeout, or defaultWriteTimeout when it is unset or disabled,
// because the drain is new work Close does and must stay bounded.
func (c *WebSocketConn) drainBound() time.Duration {
	if c.config.WriteTimeout > 0 {
		return c.config.WriteTimeout
	}
	return defaultWriteTimeout
}

// defaultWriteTimeout is WSConfig.WriteTimeout's default.
const defaultWriteTimeout = 10 * time.Second

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
		c.handoff(frame.payload)
	}
}

// handoff gives one decoded message to Read. While the buffer is full
// the pump cannot read, so it cannot see the peer's Pong, FIN, or Close
// either, and the keepalive skips its checks. That skip is bounded by
// handoffStallLimit, the same ReadIdleTimeout+PongTimeout budget a
// silent peer gets. Past it the stall is resolved one of two ways:
//
//   - The app never called Read (push-only): drop the message, and
//     every later one that finds the buffer full, so the pump keeps
//     reading and the keepalive judges the peer again.
//   - The app reads but has not taken a message in that time: close
//     the connection. A reader that slow cannot be told apart from an
//     abandoned one, and dropping its data in silence would be worse.
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
		// Closing: drop undelivered data but keep reading so the
		// peer's reciprocal Close reaches readFrame, signals
		// peerClosed, and lets awaitPeerClose return promptly.
		return
	case <-stall:
	}
	if !c.readCalled.Load() {
		c.discardUnread.Store(true)
		return
	}
	// Close on its own goroutine: Close waits for the peer's Close
	// frame, which only this pump can read.
	go c.Close()
	select {
	case c.readMsgs <- msg:
	case <-c.closed:
	}
}

// handoffStallLimit bounds how long the pump waits on a full read
// buffer: ReadIdleTimeout+PongTimeout, or no bound when either is
// disabled (a connection with keepalive off is never reclaimed for
// silence either).
// endStall ends a blocking handoff. A Ping sent before the stall had its
// Pong stuck behind frames the pump could not read, so the pong clock
// restarts here: it measures the peer, not the app's stall. The restart
// lands before handoffPending clears, so the keepalive never sees the
// stall's age as the peer's.
func (c *WebSocketConn) endStall() {
	if c.awaitingPong.Load() {
		c.pingSentAt.Store(time.Now().UnixNano())
	}
	c.handoffPending.Store(false)
}

func (c *WebSocketConn) handoffStallLimit() time.Duration {
	pong := c.config.PongTimeout
	if pong == 0 {
		pong = 10 * time.Second // keepalive's default for an unset value
	}
	if c.config.ReadIdleTimeout <= 0 || pong < 0 {
		return 0
	}
	return c.config.ReadIdleTimeout + pong
}

// defaultControlFramesPerSecond is MaxControlFramesPerSecond's
// default. A browser sends a Pong per keepalive Ping and no Ping of its
// own, so honest traffic sits far below it.
const defaultControlFramesPerSecond = 32

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

// writeFrame writes a WebSocket frame to the connection. Nothing
// follows our Close frame: a writer that waited on mu while Close
// wrote it gets ErrClosed and puts nothing on the wire.
func (c *WebSocketConn) writeFrame(opcode byte, payload []byte) error {
	return c.writeFrameBy(opcode, payload, time.Time{})
}

// writeFrameBy is writeFrame with an explicit write deadline. A zero by
// means the per-frame WriteTimeout (none when it is negative).
func (c *WebSocketConn) writeFrameBy(opcode byte, payload []byte, by time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeSent {
		return ErrClosed
	}
	if opcode == wsopcodeClose {
		c.closeSent = true
	}

	// Apply per-frame write deadline so a slow client cannot pin the
	// writePump goroutine indefinitely. A negative WriteTimeout disables
	// the deadline (opt-in by callers that know what they're doing).
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
			return nil, c.failConn(1002, "stream: protocol error: reserved bits set")
		}

		// RFC 6455 §5.5: control frames MUST NOT be fragmented (FIN=1).
		if isControl && !fin {
			return nil, c.failConn(1002, "stream: protocol error: fragmented control frame")
		}

		// Extended length. RFC 6455 §5.2: "the minimal number of bytes
		// MUST be used to encode the length", so the 16-bit form is only
		// for 126..65535 and the 64-bit form only above that.
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

		// RFC 6455: control frames MUST be <=125 bytes and not fragmented.
		if isControl && length > 125 {
			return nil, c.failConn(1002, "stream: protocol error: oversized control frame")
		}

		// Compare as uint64 so a top-bit-set length cannot wrap negative
		// and bypass the read limit.
		if length > uint64(readLimit) {
			return nil, c.failConn(1009, fmt.Sprintf("stream: message too large (%d > %d)", length, readLimit))
		}

		// RFC 6455: client-to-server frames MUST be masked. Optional
		// enforcement so existing tests that craft unmasked frames keep
		// working until they explicitly opt in.
		if c.config.requireMask && !masked {
			return nil, c.failConn(1002, "stream: protocol error: client frame must be masked")
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

		if (opcode == wsopcodePing || opcode == wsopcodePong) && !c.allowControl(time.Now()) {
			c.setOwnClose(1008, "control frame rate")
			return nil, errors.New("stream: control frame rate exceeded")
		}

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
				// §5.5.1: a reason after the code is UTF-8; one that is
				// not makes the frame a protocol error.
				if wsEchoableCloseCode(binary.BigEndian.Uint16(payload[:2])) && utf8.Valid(payload[2:]) {
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
			// After our Close frame the Pong is not sent (ErrClosed),
			// but the pump keeps reading for the peer's Close.
			if err := c.writeFrame(wsopcodePong, payload); err != nil && !errors.Is(err, ErrClosed) {
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
			// Reserved opcodes (0x3-0x7, 0xB-0xF) were previously
			// delivered to the application as ordinary data frames.
			return nil, c.failConn(1002, fmt.Sprintf("stream: protocol error: reserved opcode 0x%X", opcode))
		}
	}
}

// dataMessage returns one complete data message. A text message is
// checked as a whole, after reassembly, so a character split across
// fragments passes and invalid bytes fail the connection with 1007
// (RFC 6455 §8.1, "Invalid frame payload data").
func (c *WebSocketConn) dataMessage(opcode byte, payload []byte) (*wsFrame, error) {
	if opcode == wsopcodeText && !utf8.Valid(payload) {
		return nil, c.failConn(1007, "stream: text message is not valid UTF-8")
	}
	return &wsFrame{opcode: opcode, payload: payload}, nil
}

// failConn records the status our Close frame carries when the pump
// tears the connection down for this error, and returns the error. A
// status already recorded (CloseWithStatus) is kept. Every protocol
// violation readFrame finds goes through here (1002 for a malformed
// frame, 1007 for bad UTF-8, 1009 for a message past ReadLimit): a
// bodyless Close would tell the peer only 1005, "no status".
func (c *WebSocketConn) failConn(code uint16, msg string) error {
	status := binary.BigEndian.AppendUint16(nil, code)
	c.ownClosePayload.CompareAndSwap(nil, &status)
	return errors.New(msg)
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

// headerHasToken reports whether a comma-separated header contains
// token (case-insensitive), e.g. "keep-alive, Upgrade". values is every
// field line of the header: RFC 9110 §5.3 lets a sender split one list
// across several lines, so "Connection: keep-alive" followed by
// "Connection: Upgrade" carries the token as surely as one line does.
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

// computeAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func computeAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// ErrClosed is returned when writing to a closed connection. It is a
// plain errors.New sentinel so callers can compare with errors.Is.
var ErrClosed = errors.New("stream: connection closed")

// ErrInvalidUTF8 is returned by Write, WriteString, Hub.Broadcast and
// Hub.BroadcastWait for data that is not valid UTF-8. A WebSocket text
// message must be (RFC 6455 §5.6), and a browser fails the connection
// with 1007 on one that is not, so the message is refused before it is
// queued. Send arbitrary bytes with WriteBinary or Hub.BroadcastBinary.
var ErrInvalidUTF8 = errors.New("stream: text message is not valid UTF-8")

// compile-time assertion that net.Conn implements SetWriteDeadline so
// the writeFrame interface upgrade is valid for real network conns.
var _ interface {
	SetWriteDeadline(time.Time) error
} = (net.Conn)(nil)
