package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// HTTPHandler serves Server over MCP streamable HTTP: POST for requests, GET for a keepalive stream, DELETE to end a session.
type HTTPHandler struct {
	Server *Server

	// Encoder verifies bearer tokens; nil refuses every request with 503.
	Encoder     *auth.Encoder
	Revocations *auth.RevocationList

	// Seats caps the GET streams per token; nil uses control.DefaultSeatTable.
	Seats *control.SeatTable

	mu       sync.Mutex
	sessions map[string]*httpMCPSession
	inflight *inflightTable
}

type httpMCPSession struct {
	id        string
	mu        sync.Mutex
	pendingEv [][]byte
	closed    bool
	streams   int
}

// NewHTTPHandler returns an HTTPHandler; with a nil enc it refuses every request with 503.
func NewHTTPHandler(s *Server, enc *auth.Encoder, rl *auth.RevocationList) *HTTPHandler {
	return &HTTPHandler{
		Server:      s,
		Encoder:     enc,
		Revocations: rl,
		sessions:    make(map[string]*httpMCPSession),
		inflight:    newInflightTable(),
	}
}

func (h *HTTPHandler) seatTable() *control.SeatTable {
	if h.Seats != nil {
		return h.Seats
	}
	return control.DefaultSeatTable()
}

// ServeHTTP verifies the bearer token and serves POST, GET and DELETE on the MCP endpoint.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Encoder == nil {
		i18n.HTTPError(w, r, http.StatusServiceUnavailable, reasonNoAuthEncoder)
		return
	}
	tok := r.Header.Get("Authorization")
	c, ok := verifyBearer(h.Encoder, h.Revocations, tok)
	if !ok {
		i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
		return
	}
	claims := &c
	sessID := sanitizeSessionID(r.Header.Get("Mcp-Session-Id"))
	if sessID == "" {
		sessID = string(ids.NewSessionID())
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodPost:
		h.handlePOST(w, r, sessID, claims)
	case http.MethodGet:
		h.handleGET(w, r, sessID, claims)
	case http.MethodDelete:
		h.dropSession(sessID)
		w.WriteHeader(http.StatusNoContent)
	default:
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
	}
}

func sanitizeSessionID(s string) string {
	if s == "" {
		return s
	}
	if !strings.ContainsAny(s, "\r\n\x00") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' || c == 0 {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isAcceptableJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	if mt == "application/json" {
		return true
	}
	if strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json") {
		return true
	}
	return false
}

func sanitizeSSEData(payload []byte) []byte {
	cleaned := make([]byte, 0, len(payload))
	for _, c := range payload {
		if c == '\r' || c == 0 {
			continue
		}
		cleaned = append(cleaned, c)
	}
	lines := bytes.Split(cleaned, []byte{'\n'})
	var out bytes.Buffer
	for _, ln := range lines {
		out.WriteString("data: ")
		out.Write(defangSSEDirectives(ln))
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func defangSSEDirectives(ln []byte) []byte {
	s := string(ln)
	for _, kw := range []string{"event:", "id:", "retry:", "data:"} {
		s = strings.ReplaceAll(s, kw, kw[:len(kw)-1]+"_:")
	}
	return []byte(s)
}

func (h *HTTPHandler) handlePOST(w http.ResponseWriter, r *http.Request, sessID string, claims *auth.Claims) {
	defer r.Body.Close()
	if !isAcceptableJSONContentType(r.Header.Get("Content-Type")) {
		i18n.HTTPError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonBodyUnreadable)
		return
	}
	in := bytes.NewReader(append(bytes.TrimSpace(raw), '\n'))
	var out bytes.Buffer
	s := New(h.Server.Mux, h.Server.Catalog)
	s.IdentityClass = h.Server.IdentityClass
	s.RequiredToken = h.Server.RequiredToken
	s.Claims = claims
	s.inflight = h.inflight
	s.owner = inflightOwner{principal: string(claims.JTI), session: sessID}
	s.WithIO(in, &out)
	if err := s.Serve(r.Context()); err != nil && !errors.Is(err, context.Canceled) {
		if err != io.EOF {
			log.Printf("mcpserver: serve: %v", err)
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", sessID)
	resp := bytes.TrimSpace(out.Bytes())
	if len(resp) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func (h *HTTPHandler) handleGET(w http.ResponseWriter, r *http.Request, sessID string, claims *auth.Claims) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	var principal string
	if claims != nil {
		principal = string(claims.JTI)
	}
	seat, seated := h.seatTable().AcquireSeat(principal, 0, stream.SeatOverflowRefuse)
	if !seated {
		w.Header().Set("Retry-After", "1")
		i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
		return
	}
	defer h.seatTable().ReleaseSeat(seat)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sess := h.acquireSession(sessID)
	defer h.releaseSession(sessID)

	rc := http.NewResponseController(w)
	for _, ev := range sess.drain() {
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		_, _ = w.Write(sanitizeSSEData(ev))
		flusher.Flush()
		_ = rc.SetWriteDeadline(time.Time{})
	}
	ticker := keepaliveTicker()
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-seat.Done:
			return
		case <-ticker.C:
			if _, ok := verifyBearer(h.Encoder, h.Revocations, r.Header.Get("Authorization")); !ok {
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
			_ = rc.SetWriteDeadline(time.Time{})
		}
	}
}

func (h *HTTPHandler) acquireSession(sessID string) *httpMCPSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessID]
	if !ok {
		s = &httpMCPSession{id: sessID}
		h.sessions[sessID] = s
	}
	s.streams++
	return s
}

func (h *HTTPHandler) releaseSession(sessID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessID]
	if !ok {
		return
	}
	if s.streams > 0 {
		s.streams--
	}
	if s.streams > 0 || s.closed {
		return
	}
	s.mu.Lock()
	idle := len(s.pendingEv) == 0
	s.mu.Unlock()
	if idle {
		delete(h.sessions, sessID)
	}
}

func (h *HTTPHandler) dropSession(sessID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[sessID]; ok {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		delete(h.sessions, sessID)
	}
}

func (s *httpMCPSession) drain() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pendingEv
	s.pendingEv = nil
	return out
}

func verifyBearer(enc *auth.Encoder, rl *auth.RevocationList, header string) (auth.Claims, bool) {
	if len(header) < len("Bearer ") || header[:len("Bearer ")] != "Bearer " {
		return auth.Claims{}, false
	}
	tok := header[len("Bearer "):]
	claims, err := auth.Verify(enc, rl, tok, timeNow())
	if err != nil {
		return auth.Claims{}, false
	}
	return claims, true
}

var timeNow = realTimeNow

const sseWriteTimeout = 10 * time.Second

var keepaliveTicker = realKeepaliveTicker
