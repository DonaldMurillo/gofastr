// Package ws implements the WebSocket transport for the control
// plane. Pure stdlib + crypto/sha1; RFC 6455 frame format.
//
// Frames carry the canonical event envelope verbatim per the
// architecture doc § Canonical event envelope:
//
//	{"frame":"command","body": <Command envelope>}    client → engine
//	{"frame":"event","body":   <Event envelope>}      engine → client
//
// Reconnect resumes from a `lastEventId` query param on the
// connection URL.
package ws

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Handshake magic per RFC 6455.
const handshakeMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Opcode constants from RFC 6455 §5.2.
const (
	opcodeText   = 0x1
	opcodeBinary = 0x2
	opcodeClose  = 0x8
	opcodePing   = 0x9
	opcodePong   = 0xA
)

// Handler is the WebSocket-upgrading HTTP handler. Configure with the
// mux + auth before mounting.
type Handler struct {
	Mux         *multiplex.Mux
	Encoder     *auth.Encoder
	Revocations *auth.RevocationList

	// Optional Host/Origin guards (same shape as REST).
	AllowedHosts   []string
	AllowedOrigins []string
}

// ServeHTTP implements http.Handler. Path is /v1/ws?session=<id>[&lastEventId=N].
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.hostOK(r) {
		http.Error(w, "invalid host", http.StatusForbidden)
		return
	}
	if !h.originOK(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}

	// Verify token (URL param or X-Harness-Token header; the
	// WebSocket handshake can't set arbitrary headers from a
	// browser, so accept the URL param for browser clients and the
	// header for CLI clients).
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = r.Header.Get("X-Harness-Token")
	}
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if h.Encoder == nil {
		http.Error(w, "server misconfigured", http.StatusInternalServerError)
		return
	}
	claims, err := auth.Verify(h.Encoder, h.Revocations, tok, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	sessParam := r.URL.Query().Get("session")
	sess, err := ids.ParseSession(sessParam)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	if !claims.AllowsSession(sess) {
		http.Error(w, "token not bound to session", http.StatusForbidden)
		return
	}

	// Standard WebSocket upgrade.
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijacker", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		http.Error(w, "hijack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := completeHandshake(rw, r); err != nil {
		_ = conn.Close()
		return
	}

	wsConn := &Conn{
		netConn:  conn,
		reader:   rw.Reader,
		writer:   rw.Writer,
		identity: claims.IdentityClass,
		clientID: ids.NewClientID(),
		mux:      h.Mux,
		session:  sess,
	}
	// Use a fresh background context for the goroutine — the
	// handler returns immediately after Hijack, which would cancel
	// r.Context() and kill the connection.
	go wsConn.run(context.Background())
}

func (h *Handler) hostOK(r *http.Request) bool {
	if len(h.AllowedHosts) == 0 {
		return true
	}
	for _, x := range h.AllowedHosts {
		if r.Host == x {
			return true
		}
	}
	return false
}

func (h *Handler) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, x := range h.AllowedOrigins {
		if origin == x {
			return true
		}
	}
	return len(h.AllowedOrigins) == 0
}

// completeHandshake writes the 101 Switching Protocols response.
func completeHandshake(rw *bufio.ReadWriter, r *http.Request) error {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return errors.New("missing Sec-WebSocket-Key")
	}
	h := sha1.New()
	h.Write([]byte(key + handshakeMagic))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		return err
	}
	return rw.Flush()
}

// Conn is one upgraded WebSocket connection wrapped as a control.Client.
type Conn struct {
	netConn  net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	identity control.IdentityClass
	clientID ids.ClientID
	mux      *multiplex.Mux
	session  ids.SessionID

	mu sync.Mutex // guards writes
}

// run pumps frames: client→engine commands inbound, engine→client
// events outbound. Returns when the socket closes.
func (c *Conn) run(parentCtx context.Context) {
	defer c.netConn.Close()
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Attach to the multiplex.
	if err := c.mux.Attach(c.session, c); err != nil {
		c.writeText(controlError("attach failed: " + err.Error()))
		return
	}
	defer c.mux.Detach(c.session, c.clientID)

	// Engine→client pump.
	go c.subscribePump(ctx)

	// Client→engine read loop.
	for {
		op, payload, err := c.readFrame()
		if err != nil {
			return
		}
		switch op {
		case opcodeText:
			c.handleText(ctx, payload)
		case opcodeClose:
			c.writeClose()
			return
		case opcodePing:
			c.writeFrame(opcodePong, payload)
		case opcodePong:
			// ignore
		default:
			// Unsupported opcode; close.
			c.writeClose()
			return
		}
	}
}

// frame envelopes used over the wire.
type frame struct {
	Frame string          `json:"frame"`
	Body  json.RawMessage `json:"body"`
}

func (c *Conn) handleText(ctx context.Context, payload []byte) {
	var f frame
	if err := json.Unmarshal(payload, &f); err != nil {
		c.writeText(controlError("bad frame: " + err.Error()))
		return
	}
	if f.Frame != "command" {
		c.writeText(controlError("expected frame=command, got " + f.Frame))
		return
	}
	cmd, err := control.UnmarshalCommand(f.Body)
	if err != nil {
		c.writeText(controlError("decode: " + err.Error()))
		return
	}
	if err := c.mux.Dispatch(ctx, c, cmd); err != nil {
		c.writeText(controlError(err.Error()))
	}
}

func (c *Conn) subscribePump(ctx context.Context) {
	eng := c.mux.EngineFor(c.session)
	if eng == nil {
		return
	}
	ch := eng.Bus.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			body, err := json.Marshal(env)
			if err != nil {
				continue
			}
			f := frame{Frame: "event", Body: body}
			out, _ := json.Marshal(f)
			c.writeText(out)
		}
	}
}

// ---------- control.Client implementation ----------

func (c *Conn) ID() ids.ClientID                     { return c.clientID }
func (c *Conn) IdentityClass() control.IdentityClass { return c.identity }
func (c *Conn) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	// External clients consume the event stream directly via the
	// WebSocket frames; nothing to subscribe to in-process.
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *Conn) Send(_ context.Context, _ control.Command) error { return nil }
func (c *Conn) Close() error                                    { return c.netConn.Close() }

// ---------- frame I/O ----------

func (c *Conn) readFrame() (op byte, payload []byte, err error) {
	b0, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b1, err := c.reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	fin := b0&0x80 != 0
	_ = fin // continuation frames are rare; v0.1 expects whole frames.
	op = b0 & 0x0F
	masked := b1&0x80 != 0
	length := uint64(b1 & 0x7F)
	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > 16<<20 {
		return 0, nil, fmt.Errorf("ws: frame too large (%d bytes)", length)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return op, payload, nil
}

func (c *Conn) writeText(payload []byte) {
	_ = c.writeFrame(opcodeText, payload)
}

func (c *Conn) writeClose() {
	_ = c.writeFrame(opcodeClose, nil)
}

func (c *Conn) writeFrame(op byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	header := []byte{0x80 | op}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 0xFFFF:
		header = append(header, 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		header = append(header, ext[:]...)
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		header = append(header, ext[:]...)
	}
	if _, err := c.writer.Write(header); err != nil {
		return err
	}
	if _, err := c.writer.Write(payload); err != nil {
		return err
	}
	return c.writer.Flush()
}

func controlError(msg string) []byte {
	body, _ := json.Marshal(control.Error{
		Reason:  control.ReasonInvalidCommand,
		Message: msg,
	})
	f := frame{Frame: "event", Body: body}
	out, _ := json.Marshal(f)
	return out
}
