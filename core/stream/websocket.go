package stream

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
}

// WSConfig configures the WebSocket connection.
type WSConfig struct {
	// ReadLimit is the maximum message size in bytes. 0 = default 1MB.
	ReadLimit int64

	// SendBuffer is the number of messages that can be buffered before
	// Write blocks. 0 = 32.
	SendBuffer int

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

	// Write the upgrade response directly
	upgradeResp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"
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

	wsc := &WebSocketConn{
		conn:       conn,
		sendBuffer: make(chan []byte, sendBuf),
		closed:     make(chan struct{}),
		config:     cfg,
	}

	if cfg.OnClose != nil {
		wsc.onClose = append(wsc.onClose, cfg.OnClose)
	}

	// Start the write pump
	go wsc.writePump()

	return wsc, nil
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
func (c *WebSocketConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// Send close frame
		c.writeFrame(wsopcodeClose, nil)
		close(c.closed)
		if closer, ok := c.conn.(interface{ Close() error }); ok {
			err = closer.Close()
		}
		for _, fn := range c.onClose {
			go fn()
		}
	})
	return err
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

// readFrame reads a WebSocket frame from the connection.
func (c *WebSocketConn) readFrame() (*wsFrame, error) {
	// Read first 2 bytes
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}

	opcode := header[0] & 0x0F
	masked := (header[1] & 0x80) != 0
	length := int64(header[1] & 0x7F)

	// Extended length
	if length == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.conn, ext); err != nil {
			return nil, err
		}
		length = int64(binary.BigEndian.Uint16(ext))
	} else if length == 127 {
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.conn, ext); err != nil {
			return nil, err
		}
		length = int64(binary.BigEndian.Uint64(ext))
	}

	if c.config.ReadLimit > 0 && length > c.config.ReadLimit {
		return nil, fmt.Errorf("stream: message too large (%d > %d)", length, c.config.ReadLimit)
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

	// Handle control frames
	switch opcode {
	case wsopcodeClose:
		return nil, io.EOF
	case wsopcodePing:
		c.writeFrame(wsopcodePong, payload)
		return c.readFrame() // read next frame
	}

	return &wsFrame{opcode: opcode, payload: payload}, nil
}

// computeAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func computeAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// ErrClosed is returned when writing to a closed connection.
var ErrClosed = &wsError{"websocket: connection closed"}

type wsError struct{ msg string }

func (e *wsError) Error() string { return e.msg }
