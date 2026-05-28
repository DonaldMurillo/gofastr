package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// HTTPHandler wraps Server with the MCP streamable-HTTP transport.
//
// Per the MCP spec (2024-11-05), streamable HTTP uses a single
// endpoint that accepts POST for request/notification + GET for
// server-initiated events. Mcp-Session-Id header binds a logical MCP
// session across requests; Last-Event-ID enables stream resumption.
//
// v0.1 supports the POST + GET split with in-memory session tracking;
// resumption replays only events from the active connection's buffer
// (full historical replay against the session log is on the agenda
// once mcpserver formally subscribes to engine buses).
type HTTPHandler struct {
	Server      *Server
	Encoder     *auth.Encoder
	Revocations *auth.RevocationList

	mu       sync.Mutex
	sessions map[string]*httpMCPSession
}

type httpMCPSession struct {
	id        string
	mu        sync.Mutex
	pendingEv [][]byte // event payloads queued for the GET stream
	closed    bool
}

// NewHTTPHandler returns an HTTP handler wrapping Server.
func NewHTTPHandler(s *Server, enc *auth.Encoder, rl *auth.RevocationList) *HTTPHandler {
	return &HTTPHandler{
		Server:      s,
		Encoder:     enc,
		Revocations: rl,
		sessions:    make(map[string]*httpMCPSession),
	}
}

// ServeHTTP dispatches the MCP streamable-HTTP protocol.
//
//	POST /mcp  → JSON-RPC request, returns immediate JSON response
//	GET  /mcp  → SSE stream of server-initiated events / notifications
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Encoder != nil {
		tok := r.Header.Get("Authorization")
		if !verifyBearer(h.Encoder, h.Revocations, tok) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	sessID := sanitizeSessionID(r.Header.Get("Mcp-Session-Id"))
	if sessID == "" {
		sessID = string(ids.NewSessionID())
	}
	// Cache-Control: no-store on every response — these are session-bound
	// JSON-RPC / SSE responses, never cacheable by intermediaries.
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodPost:
		h.handlePOST(w, r, sessID)
	case http.MethodGet:
		h.handleGET(w, r, sessID)
	case http.MethodDelete:
		h.dropSession(sessID)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sanitizeSessionID strips CR, LF, and NUL bytes from an Mcp-Session-Id
// header value to prevent response-header / session-fixation injection
// where an attacker reflects a newline-bearing id into headers.
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

// isAcceptableJSONContentType returns true if ct parses as
// application/json or application/*+json. Anything else (including a
// missing or text/plain body) is rejected with 415 to block
// content-type smuggling into the JSON-RPC transport.
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

// sanitizeSSEData defangs an event payload before emission so a single
// stored event cannot inject additional SSE directives (event:, id:,
// retry:, or a blank-line frame break) on replay. We:
//   - strip CR and NUL bytes outright,
//   - split on LF and re-prefix every line with "data: ",
//   - defang any embedded SSE directive keywords inside each line so
//     that even a downstream string-match for `event:` / `id:` /
//     `retry:` / `data:` will not see a forgeable directive.
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

// defangSSEDirectives rewrites SSE directive keywords inside a single
// already-newline-split data line so an attacker-controlled payload
// cannot smuggle a directive substring into the emitted frame.
func defangSSEDirectives(ln []byte) []byte {
	s := string(ln)
	for _, kw := range []string{"event:", "id:", "retry:", "data:"} {
		s = strings.ReplaceAll(s, kw, kw[:len(kw)-1]+"_:")
	}
	return []byte(s)
}

func (h *HTTPHandler) handlePOST(w http.ResponseWriter, r *http.Request, sessID string) {
	defer r.Body.Close()
	if !isAcceptableJSONContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type: expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		http.Error(w, "body read: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Reuse Server's stdio handler via an in-memory io pair. Build a
	// fresh Server pointer rather than copying the parent by value —
	// the parent embeds a sync.Mutex that must not be copied (go vet:
	// "assignment copies lock value").
	in := bytes.NewReader(append(bytes.TrimSpace(raw), '\n'))
	var out bytes.Buffer
	s := New(h.Server.Mux, h.Server.Catalog)
	s.IdentityClass = h.Server.IdentityClass
	s.RequiredToken = h.Server.RequiredToken
	s.WithIO(in, &out)
	if err := s.Serve(r.Context()); err != nil && !errors.Is(err, context.Canceled) {
		// Serve returns nil on EOF; only log unexpected errors.
		if err != io.EOF {
			http.Error(w, "mcp serve: "+err.Error(), http.StatusInternalServerError)
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

func (h *HTTPHandler) handleGET(w http.ResponseWriter, r *http.Request, sessID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sess := h.acquireSession(sessID)
	defer h.releaseSession(sessID)

	// Replay backlog (anything published while no GET was attached).
	// Defang each stored event so a poisoned payload cannot inject a
	// second SSE frame or a fake event:/id:/retry: directive on replay.
	for _, ev := range sess.drain() {
		_, _ = w.Write(sanitizeSSEData(ev))
		flusher.Flush()
	}
	// Park until ctx done; mcpserver currently doesn't publish
	// notifications to the HTTP GET stream itself — the resource
	// subscriptions land that way in a follow-up. For v0.1 we keep
	// the stream open as a keep-alive heartbeat every 15s.
	ticker := keepaliveTicker()
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (h *HTTPHandler) acquireSession(sessID string) *httpMCPSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[sessID]; ok {
		return s
	}
	s := &httpMCPSession{id: sessID}
	h.sessions[sessID] = s
	return s
}

func (h *HTTPHandler) releaseSession(_ string) {
	// v0.1 keeps the session record so subsequent POSTs see the
	// same backlog; a TTL job would prune dead sessions.
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

// verifyBearer accepts the bearer scheme used by REST + ws.
func verifyBearer(enc *auth.Encoder, rl *auth.RevocationList, header string) bool {
	if len(header) < len("Bearer ") || header[:len("Bearer ")] != "Bearer " {
		return false
	}
	tok := header[len("Bearer "):]
	_, err := auth.Verify(enc, rl, tok, timeNow())
	return err == nil
}

// timeNow is replaced in tests; production uses time.Now().
var timeNow = realTimeNow

// keepaliveTicker is split out to ease testing.
var keepaliveTicker = realKeepaliveTicker
