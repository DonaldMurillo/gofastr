package ws

import (
	"bufio"
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

const handshakeMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opcodeContinuation = 0x0
	opcodeText         = 0x1
	opcodeBinary       = 0x2
	opcodeClose        = 0x8
	opcodePing         = 0x9
	opcodePong         = 0xA
)

// Handler upgrades GET /v1/ws?session=<id>[&lastEventId=N] to a WebSocket control connection.
type Handler struct {
	Mux         *multiplex.Mux
	Encoder     *auth.Encoder
	Revocations *auth.RevocationList

	// AllowedHosts are the exact Host headers admitted; empty admits any.
	AllowedHosts []string
	// AllowedOrigins are the exact browser Origins admitted; empty admits none.
	AllowedOrigins []string
	// Seats caps the sockets per token; nil uses control.DefaultSeatTable.
	Seats *control.SeatTable

	recheck time.Duration
}

func (h *Handler) seatTable() *control.SeatTable {
	if h.Seats != nil {
		return h.Seats
	}
	return control.DefaultSeatTable()
}

// ServeHTTP checks Host, Origin, token, session scope and the upgrade request, then upgrades.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.hostOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonCrossOriginHost)
		return
	}
	if !h.originOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonOriginRefused)
		return
	}

	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = r.Header.Get("X-Harness-Token")
	}
	if tok == "" {
		i18n.HTTPError(w, r, http.StatusUnauthorized, reasonTokenMissing)
		return
	}
	if h.Encoder == nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	claims, err := auth.Verify(h.Encoder, h.Revocations, tok, time.Now())
	if err != nil {
		i18n.HTTPErrorFor(w, r, http.StatusUnauthorized, err)
		return
	}

	sessParam := r.URL.Query().Get("session")
	sess, err := ids.ParseSession(sessParam)
	if err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonInvalidSessionID)
		return
	}
	if !claims.AllowsSession(sess) {
		i18n.HTTPError(w, r, http.StatusForbidden, reasonTokenUnbound)
		return
	}
	var since uint64
	rawSince := r.URL.Query().Get("lastEventId")
	resume := rawSince != ""
	if resume {
		if since, err = strconv.ParseUint(rawSince, 10, 64); err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, reasonBadLastEventID)
			return
		}
	}

	if err := stream.CheckUpgradeRequest(r, func(*http.Request) bool { return true }); err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonWebSocketRequired)
		return
	}
	seat, seated := h.seatTable().AcquireSeat(string(claims.JTI), 0, stream.SeatOverflowRefuse)
	if !seated {
		w.Header().Set("Retry-After", "1")
		i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		h.seatTable().ReleaseSeat(seat)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		h.seatTable().ReleaseSeat(seat)
		log.Printf("ws: hijack failed: %v", err)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	if err := completeHandshake(rw, r); err != nil {
		h.seatTable().ReleaseSeat(seat)
		_ = conn.Close()
		return
	}
	wsConn := &Conn{
		netConn:     conn,
		reader:      rw.Reader,
		writer:      rw.Writer,
		identity:    claims.IdentityClass,
		clientID:    ids.NewClientID(),
		mux:         h.Mux,
		session:     sess,
		claims:      claims,
		token:       tok,
		encoder:     h.Encoder,
		revocations: h.Revocations,
		seats:       h.seatTable(),
		seat:        seat,
		recheck:     cmp.Or(h.recheck, revocationRecheck),
		resume:      resume,
		since:       since,
	}
	go wsConn.run(context.Background())
}

func (h *Handler) hostOK(r *http.Request) bool {
	if len(h.AllowedHosts) == 0 {
		return true
	}
	return slices.Contains(h.AllowedHosts, r.Host)
}

func (h *Handler) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return slices.Contains(h.AllowedOrigins, origin)
}

func completeHandshake(rw *bufio.ReadWriter, r *http.Request) error {
	key := r.Header.Get("Sec-WebSocket-Key")
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

// Conn is one upgraded WebSocket connection, attached to the multiplexer as a control.Client.
type Conn struct {
	netConn  net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	identity control.IdentityClass
	clientID ids.ClientID
	mux      *multiplex.Mux
	session  ids.SessionID

	seats   *control.SeatTable
	seat    *control.Seat
	recheck time.Duration

	resume bool
	since  uint64

	claims      auth.Claims
	token       string
	encoder     *auth.Encoder
	revocations *auth.RevocationList

	mu sync.Mutex
}

func (c *Conn) run(parentCtx context.Context) {
	defer c.netConn.Close()
	defer c.seats.ReleaseSeat(c.seat)
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	go func() {
		select {
		case <-c.seat.Done:
			_ = c.netConn.Close()
		case <-ctx.Done():
		}
	}()

	if err := c.mux.Attach(c.session, c); err != nil {
		c.writeText(controlError("attach failed: " + err.Error()))
		return
	}
	defer c.mux.Detach(c.session, c.clientID)

	var events <-chan control.EventEnvelope
	if eng := c.mux.EngineFor(c.session); eng != nil {
		if c.resume {
			events = eng.Bus.SubscribeSince(ctx, c.since)
		} else {
			events = eng.Bus.Subscribe(ctx)
		}
	}
	go c.eventPump(ctx, events)
	go c.revocationWatch(ctx)

	var msg []byte
	assembling := false
	for {
		fin, op, payload, err := c.readFrame(wsMessageLimit - len(msg))
		if err != nil {
			if errors.Is(err, errFrameTooLarge) {
				c.writeClose()
			}
			return
		}
		if op >= opcodeClose && !fin {
			c.writeClose()
			return
		}
		switch op {
		case opcodeText, opcodeContinuation:
			if (op == opcodeText) == assembling {
				c.writeClose()
				return
			}
			msg = append(msg, payload...)
			assembling = !fin
			if fin {
				c.handleText(ctx, msg)
				msg = nil
			}
		case opcodeClose:
			c.writeClose()
			return
		case opcodePing:
			c.writeFrame(opcodePong, payload)
		case opcodePong:
		default:
			c.writeClose()
			return
		}
	}
}

func (c *Conn) revocationWatch(ctx context.Context) {
	t := time.NewTicker(c.recheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := auth.Verify(c.encoder, c.revocations, c.token, time.Now()); err != nil {
				_ = c.netConn.Close()
				return
			}
		}
	}
}

type frame struct {
	Frame string          `json:"frame"`
	Body  json.RawMessage `json:"body"`
}

func (c *Conn) handleText(ctx context.Context, payload []byte) {
	var f frame
	if err := handler.UnmarshalStrict(payload, &f); err != nil {
		c.writeText(controlError("bad frame: " + err.Error()))
		return
	}
	if f.Frame != "command" {
		c.writeText(controlError("expected frame=command, got " + f.Frame))
		return
	}
	if _, err := auth.Verify(c.encoder, c.revocations, c.token, time.Now()); err != nil {
		c.writeText(controlError("token rejected: " + err.Error()))
		c.writeClose()
		_ = c.netConn.Close()
		return
	}
	cmd, err := control.UnmarshalCommand(f.Body)
	if err != nil {
		c.writeText(controlError("decode: " + err.Error()))
		return
	}
	if !c.claims.AllowsCommand(cmd.CommandKind()) {
		c.writeText(controlError("token is not scoped for " + cmd.CommandKind()))
		return
	}
	if sid, ok := commandSession(cmd); ok {
		switch cmd.(type) {
		case control.AttachSession, control.DetachSession:
			if !c.claims.AllowsSession(sid) {
				c.writeText(controlError("token is not scoped for session " + string(sid)))
				return
			}
		default:
			if sid != c.session {
				c.writeText(controlError("command names session " + string(sid) + " but this connection is attached to " + string(c.session)))
				return
			}
		}
	}
	if err := c.mux.Dispatch(ctx, c, cmd); err != nil {
		c.writeText(controlError(err.Error()))
	}
}

func commandSession(cmd control.Command) (ids.SessionID, bool) {
	switch v := cmd.(type) {
	case control.SendInput:
		return v.SessionID, true
	case control.CancelTurn:
		return v.SessionID, true
	case control.AnswerPermission:
		return v.SessionID, true
	case control.AttachSession:
		return v.SessionID, true
	case control.DetachSession:
		return v.SessionID, true
	case control.SetModel:
		return v.SessionID, true
	case control.EnterPlanMode:
		return v.SessionID, true
	case control.ExitPlanMode:
		return v.SessionID, true
	case control.CustomCommand:
		return v.SessionID, true
	}
	return "", false
}

func (c *Conn) eventPump(ctx context.Context, ch <-chan control.EventEnvelope) {
	if ch == nil {
		return
	}
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
			out, err := json.Marshal(f)
			if err != nil {
				log.Printf("harness ws: encode event frame: %v", err)
				continue
			}
			if err := c.writeFrame(opcodeText, out); err != nil {
				return
			}
		}
	}
}

func (c *Conn) ID() ids.ClientID                     { return c.clientID }
func (c *Conn) IdentityClass() control.IdentityClass { return c.identity }
func (c *Conn) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *Conn) Send(_ context.Context, _ control.Command) error { return nil }
func (c *Conn) Close() error                                    { return c.netConn.Close() }

const wsPayloadChunk = 32 << 10

const wsMessageLimit = 16 << 20

var errFrameTooLarge = errors.New("ws: frame too large")

const wsFrameReadTimeout = 60 * time.Second

var wsFrameWriteTimeout = 10 * time.Second

const revocationRecheck = 10 * time.Second

func (c *Conn) readFrame(room int) (fin bool, op byte, payload []byte, err error) {
	b0, err := c.reader.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	b1, err := c.reader.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	fin = b0&0x80 != 0
	op = b0 & 0x0F
	masked := b1&0x80 != 0
	length := uint64(b1 & 0x7F)
	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(c.reader, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	limit := uint64(room)
	if op >= opcodeClose {
		limit = 125
	}
	if length > limit {
		return false, 0, nil, fmt.Errorf("%w (%d bytes)", errFrameTooLarge, length)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	var payloadBuf []byte
	if length > 0 {
		_ = c.netConn.SetReadDeadline(time.Now().Add(wsFrameReadTimeout))
		payloadBuf = make([]byte, 0, min(int(length), wsPayloadChunk))
		buf := make([]byte, min(int(length), wsPayloadChunk))
		for uint64(len(payloadBuf)) < length {
			want := min(int(length-uint64(len(payloadBuf))), len(buf))
			n, rerr := io.ReadFull(c.reader, buf[:want])
			if rerr != nil {
				_ = c.netConn.SetReadDeadline(time.Time{})
				return false, 0, nil, rerr
			}
			payloadBuf = append(payloadBuf, buf[:n]...)
		}
		_ = c.netConn.SetReadDeadline(time.Time{})
	}
	if masked {
		for i := range payloadBuf {
			payloadBuf[i] ^= mask[i%4]
		}
	}
	return fin, op, payloadBuf, nil
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
	if err := c.netConn.SetWriteDeadline(time.Now().Add(wsFrameWriteTimeout)); err != nil {
		return err
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
	body, err := json.Marshal(control.Error{
		Reason:  control.ReasonInvalidCommand,
		Message: msg,
	})
	if err != nil {
		return []byte(`{"frame":"event","body":{"reason":"InvalidCommand","message":"invalid command"}}`)
	}
	out, err := json.Marshal(frame{Frame: "event", Body: body})
	if err != nil {
		return []byte(`{"frame":"event","body":{"reason":"InvalidCommand","message":"invalid command"}}`)
	}
	return out
}
