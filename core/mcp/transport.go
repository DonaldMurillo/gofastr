package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// maxMCPBodyBytes caps the JSON-RPC request body to 1 MiB. Without
// this cap a single unauthenticated POST could read an arbitrary
// payload into memory.
const maxMCPBodyBytes = 1 << 20

// isMCPJSONContentType allows only "application/json" (with optional
// parameters) and the +json structured-suffix family. The literal
// prefix check that used to live here accepted "application/jsonp"
// and other smuggled types.
func isMCPJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// decodeMCPRequest enforces the content-type and body-size policy for
// the JSON-RPC HTTP transport. It writes an HTTP error response on
// failure and reports whether the caller should continue.
func decodeMCPRequest(w http.ResponseWriter, r *http.Request, req *Request) bool {
	if !isMCPJSONContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return false
	}
	body := http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	defer body.Close()
	if err := json.NewDecoder(body).Decode(req); err != nil {
		var maxErr *http.MaxBytesError
		if errorAsMaxBytes(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &RPCError{
				Code:    ErrInvalidParams,
				Message: "invalid JSON: " + err.Error(),
			},
		})
		return false
	}
	return true
}

// errorAsMaxBytes is a tiny shim so we can use errors.As without
// importing it at the top of the file noisily.
func errorAsMaxBytes(err error, target **http.MaxBytesError) bool {
	for e := err; e != nil; {
		if m, ok := e.(*http.MaxBytesError); ok {
			*target = m
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// originOK reports whether r may reach the JSON-RPC dispatcher.
//
// Two independent checks, because they stop different attacks:
//
//   - Origin: a browser sets it on every cross-origin fetch. If it is
//     present and names a different authority than the request itself,
//     this is a cross-site call and is refused. Absent Origin passes:
//     curl, stdio bridges and native MCP clients never send one, and
//     its absence cannot prove an attack.
//   - Host: the anti-DNS-rebinding control. Origin alone cannot stop
//     rebinding, because after the rebind the attacker's page IS
//     same-origin with the listener. Comparing Host against the
//     authority the server was told to expect breaks the chain, since
//     a rebound request still carries the attacker's own name. Hosts
//     are only pinned when the embedder calls SetAllowedHosts: an
//     unpinned server can't know its own public name, so it stays
//     permissive rather than breaking ordinary deployments.
//
// The content-type gate above already refuses form-shaped CSRF; this
// closes the fetch/rebinding half. MCP's own security guidance makes
// Origin validation a MUST for HTTP transports.
func (s *Server) originOK(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
			if !s.originAllowListed(origin) {
				return false
			}
		}
	}
	s.mu.RLock()
	hosts := s.allowedHosts
	loopbackOnly := s.requireLoopbackHost
	s.mu.RUnlock()
	if loopbackOnly && !isLoopbackAuthority(r.Host) {
		return false
	}
	if len(hosts) == 0 {
		return true // unpinned: embedder never declared an authority
	}
	for _, h := range hosts {
		if strings.EqualFold(r.Host, h) {
			return true
		}
	}
	return false
}

// originAllowListed reports whether origin was explicitly permitted via
// SetAllowedOrigins: the escape hatch for tunnels (ngrok, Codespaces)
// and browser clients served from a different authority.
func (s *Server) originAllowListed(origin string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.allowedOrigins {
		if strings.EqualFold(origin, o) {
			return true
		}
	}
	return false
}

// SetAllowedHosts pins the Host authorities this server answers on.
// Pinning is what makes the transport DNS-rebinding-proof; an empty
// list leaves the server unpinned (Host unchecked). `gofastr dev` pins
// to loopback because dev auto-enables the mutating control tools.
func (s *Server) SetAllowedHosts(hosts []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedHosts = append([]string(nil), hosts...)
}

// SetRequireLoopbackHost restricts this transport to loopback Host
// authorities. Use it wherever the MCP surface is auto-enabled for
// local development: dev implies the mutating control tools, so a
// rebound Host must not reach the dispatcher.
//
// SCOPE: this is a browser control, not a network control. It stops DNS
// rebinding because a browser cannot forge Host. It does nothing against
// a direct TCP client, which sets Host to whatever it likes: a listener
// on a routable interface stays reachable by anyone who can open a
// socket to it. Pair the pin with a loopback BIND. The framework does
// this in guardDevMCPBind, which withholds the dev-implied control tools
// when the listen address is not loopback.
func (s *Server) SetRequireLoopbackHost(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requireLoopbackHost = v
}

// isLoopbackAuthority reports whether authority ("host" or "host:port")
// names the loopback interface.
func isLoopbackAuthority(authority string) bool {
	host := authority
	if h, _, err := net.SplitHostPort(authority); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// SetAllowedOrigins permits browser Origins that are not same-origin
// with the request: tunnels and split-origin dev clients.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedOrigins = append([]string(nil), origins...)
}

// ServeHTTP handles HTTP POST requests for MCP JSON-RPC calls.
// It reads a JSON-RPC request from the body and writes the response.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.originOK(r) {
		http.Error(w, "forbidden: cross-origin or unexpected Host", http.StatusForbidden)
		return
	}

	// Set cache-control before any other writes so it survives
	// regardless of the path we take below.
	w.Header().Set("Cache-Control", "no-store")

	var req Request
	if !decodeMCPRequest(w, r, &req) {
		return
	}

	// Propagate request context with user/tenant info
	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)
	ctx = enrichContext(ctx)

	resp := s.HandleRequest(ctx, req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ServeStdio reads JSON-RPC requests from in line-by-line and writes
// responses to out. It blocks until in returns EOF or ctx is cancelled.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			encoder.Encode(Response{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &RPCError{
					Code:    ErrInvalidParams,
					Message: "invalid JSON: " + err.Error(),
				},
			})
			continue
		}

		ctx := enrichContext(ctx)
		resp := s.HandleRequest(ctx, req)
		encoder.Encode(resp)
	}

	return scanner.Err()
}

// ServeSSE sets up an HTTP handler that supports Server-Sent Events for
// streaming responses. The POST endpoint at path handles JSON-RPC calls,
// and the GET endpoint streams responses via SSE.
func (s *Server) ServeSSE(path string) http.Handler {
	mux := http.NewServeMux()

	// POST endpoint for JSON-RPC calls that may stream responses
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.ssePostHandler(w, r)
			return
		}

		if r.Method == http.MethodGet {
			s.sseGetHandler(w, r)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	return mux
}

// ssePostHandler handles POST requests with optional SSE streaming.
func (s *Server) ssePostHandler(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) {
		http.Error(w, "forbidden: cross-origin or unexpected Host", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	var req Request
	if !decodeMCPRequest(w, r, &req) {
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)
	ctx = enrichContext(ctx)

	// Check if client wants SSE streaming via Accept header
	if wantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		resp := s.HandleRequest(ctx, req)
		data, _ := json.Marshal(resp)
		StreamSSE(w, "message", string(data))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Standard JSON response
	resp := s.HandleRequest(ctx, req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// sseGetHandler sets up an SSE connection for streaming.
func (s *Server) sseGetHandler(w http.ResponseWriter, r *http.Request) {
	// Same gate as ssePostHandler. This handler used to discard the
	// request entirely, so the origin/Host check simply did not run on
	// the GET half of the pair, a guard hole rather than a disclosure
	// today (the event below is static), but the next thing streamed
	// from here would be a cross-origin read.
	if !s.originOK(r) {
		http.Error(w, "forbidden: cross-origin or unexpected Host", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Send initial connection event
	StreamSSE(w, "endpoint", "/sse")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// wantsSSE checks if the request Accept header indicates SSE.
func wantsSSE(r *http.Request) bool {
	return r.Header.Get("Accept") == "text/event-stream"
}

// contextKey is a private key for storing *http.Request in context.
type contextKey struct{}

// WithRequest stashes the original inbound *http.Request in ctx so tool
// handlers can recover the caller's transport-level auth (Cookie /
// Authorization headers). The HTTP transports call this automatically;
// it is exported so non-HTTP callers (and tests) can populate the same
// slot.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// RequestFromContext returns the original inbound *http.Request stashed by
// the transport, if any. Tool handlers that re-dispatch through an HTTP
// router use it to copy the caller's auth onto the internal request so
// session/JWT middleware re-resolves the same user instead of demoting to
// anonymous.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(contextKey{}).(*http.Request)
	return r, ok
}

// StreamSSE writes a single SSE event to the writer. It is the hardened
// entry point for tool-result streaming and treats both arguments as
// untrusted:
//
//   - the event name is truncated at the first CR/LF/NUL so the caller
//     can't terminate the "event:" field and inject a forged directive
//     below it.
//   - the data is delivered with spec-correct multi-line `data:` framing:
//     every '\n'-delimited line of the payload becomes its own `data:`
//     line, and a single trailing blank line dispatches the event. A
//     spec consumer re-joins the `data:` lines with '\n', so the payload
//     round-trips byte-for-byte, including newlines and any
//     "event:"/"id:"/"retry:"/"data:" substrings, which are DATA here
//     (they appear only after a `data: ` prefix) and therefore cannot
//     start a new field line or a second event frame. The previous
//     implementation collapsed the payload to one line and rewrote those
//     substrings, corrupting legitimate JSON-RPC content.
func StreamSSE(w io.Writer, event, data string) {
	event = stripSSEField(event)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	// Normalise CR/CRLF to LF, then emit one `data:` line per payload
	// line. A single blank line at the end dispatches the event.
	nd := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(data)
	for _, line := range strings.Split(nd, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// stripSSEField truncates at the first CR/LF/NUL: those bytes
// terminate an SSE field line and would let a caller-supplied
// value inject forged directives below it.
func stripSSEField(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}

// ensure handler package is imported for context propagation
var _ = handler.SetUser
