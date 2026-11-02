package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/netguard"
)

// maxMCPBodyBytes caps the JSON-RPC request body to 1 MiB. Without
// this cap a single unauthenticated POST could read an arbitrary
// payload into memory.
const maxMCPBodyBytes = 1 << 20

// sseWriteTimeout bounds each individual write on the held-open SSE
// notification stream. Servers that serve that stream run
// http.Server.WriteTimeout zero (a global deadline would kill
// long-lived streams), so without a per-write deadline a client that
// stops reading pins the handler goroutine forever: closing sub.ch
// cannot interrupt an in-flight write. Each write arms a fresh one.
const sseWriteTimeout = 10 * time.Second

// sseKeepAliveEvery paces the comment line the held notification stream
// writes on a fixed interval, whether or not notifications went out in
// between. Each tick also re-checks the caller, so a stream whose session was revoked, or whose connection
// went half-open, ends within one interval instead of whenever the next
// notification happens to arrive. Server.sseKeepAlive overrides it
// (tests).
const sseKeepAliveEvery = 15 * time.Second

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
// failure and reports whether the caller should continue; notification
// reports a message with no id member, which the caller must not answer.
func decodeMCPRequest(w http.ResponseWriter, r *http.Request, req *Request) (ok, notification bool) {
	if !isMCPJSONContentType(r.Header.Get("Content-Type")) {
		i18n.HTTPError(w, r, http.StatusUnsupportedMediaType, i18n.StatusPhrase(http.StatusUnsupportedMediaType))
		return false, false
	}
	body := http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errorAsMaxBytes(err, &maxErr) {
			i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return false, false
		}
		writeEnvelopeError(w, newErrorResponse(nil, ErrParse, "invalid JSON: "+handler.BodyReadDetail(err).Error()))
		return false, false
	}
	notification, errResp := decodeEnvelope(raw, req)
	if errResp != nil {
		writeEnvelopeError(w, *errResp)
		return false, false
	}
	return true, notification
}

// writeEnvelopeError answers a message the transport could not decode.
func writeEnvelopeError(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(resp)
}

// encodeResponse is the wire form of a dispatched response, one JSON
// line. A response JSON cannot encode (a NaN in a content block's or a
// prompt message's _meta, values the dispatch hands through unmarshaled)
// becomes an internal error carrying the request's id: the transports
// used to drop the encode error, which answered HTTP with an empty body
// and stdio with no line at all, so the client waited on a reply that
// never came. The encode error names Go types and values; it is logged,
// never sent.
func encodeResponse(resp Response) []byte {
	b, err := json.Marshal(resp)
	if err == nil {
		return append(b, '\n')
	}
	slog.Error("mcp: response encode failed", slog.String("err", err.Error()))
	b, err = json.Marshal(newErrorResponse(resp.ID, ErrInternalError, "internal error"))
	if err != nil {
		// Only an in-process id could fail here (a NaN float): answer
		// with a null id rather than with nothing.
		b, _ = json.Marshal(newErrorResponse(nil, ErrInternalError, "internal error"))
	}
	return append(b, '\n')
}

// decodeEnvelope is the one envelope decode every transport runs.
//
// Malformed JSON is a parse error (-32700). A well-formed message that
// handler.UnmarshalStrict refuses is an invalid request (-32600): it
// rejects duplicate and case-folded top-level keys, because stdlib json
// keeps the last duplicate and folds key case, and no first-occurrence
// parser (proxy, WAF, audit logger) may disagree with the dispatcher's
// decode.
//
// notification reports a valid request with no "id" member. JSON-RPC
// defines a notification by that absence, whatever the method, and
// forbids a reply; an id-less message with no method or a version other
// than 2.0 is an invalid request instead;
// "id": null is present, so it is a request and is answered. The id the
// decode keeps is the literal the client sent when it is a number: a
// float64 rounds an id above 2^53, and the client could not match the
// reply to its request.
func decodeEnvelope(raw []byte, req *Request) (notification bool, errResp *Response) {
	if !json.Valid(raw) {
		resp := newErrorResponse(nil, ErrParse, "invalid JSON")
		return false, &resp
	}
	// UseNumber keeps a numeric id as the literal the client sent: a
	// float64 rounds one above 2^53 and cannot hold one past its range.
	if err := handler.UnmarshalStrict(raw, req, handler.UseNumber()); err != nil {
		resp := newErrorResponse(nil, ErrInvalidRequest, "invalid request: "+err.Error())
		return false, &resp
	}
	var env struct {
		ID json.RawMessage `json:"id"`
	}
	//gofastr:allow(GOFASTR1407) raw already passed handler.UnmarshalStrict above, so both decodes read the one "id" key; this pass only reads whether it is present
	if err := json.Unmarshal(raw, &env); err != nil {
		resp := newErrorResponse(nil, ErrInvalidRequest, "invalid request: "+handler.JSONErrorDetail(err).Error())
		return false, &resp
	}
	if env.ID == nil {
		// Only a valid request object is a notification. {} or a wrong
		// version is an invalid request, which JSON-RPC answers with a
		// null id even though no id was sent.
		if req.JSONRPC != "2.0" || req.Method == "" {
			resp := newErrorResponse(nil, ErrInvalidRequest, "invalid request: a notification needs jsonrpc 2.0 and a method")
			return false, &resp
		}
		return true, nil
	}
	return false, nil
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
	if loopbackOnly && !netguard.IsLoopbackAuthority(r.Host) {
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
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
		return
	}
	if !s.originOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonCrossOriginHost)
		return
	}

	// Set cache-control before any other writes so it survives
	// regardless of the path we take below.
	w.Header().Set("Cache-Control", "no-store")

	var req Request
	ok, notification := decodeMCPRequest(w, r, &req)
	if !ok {
		return
	}
	// A notification gets 202 and no body, as stdio sends no reply.
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Propagate request context with user/tenant info
	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)

	resp := s.HandleRequest(ctx, req)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encodeResponse(resp))
}

// ServeStdio reads JSON-RPC requests from in line-by-line and writes
// responses to out. It returns at EOF, or with ctx's error once ctx
// ends, silent input included: the line read runs on its own goroutine,
// which stays parked in in.Read until in yields a line or closes. A
// process that exits on the signal loses nothing to it; a caller that
// goes on using in closes it.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	// A line may be as long as an HTTP body (maxMCPBodyBytes); the
	// scanner's 64 KiB default ended the session on any larger tool
	// call with no reply at all. The scanner's limit counts the line
	// ending too, so it gets room for "\r\n" and the length check
	// below enforces the limit on the line itself.
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxMCPBodyBytes+2)
	encoder := json.NewEncoder(out)
	tooLong := func() {
		// The session ends, but the client learns why first.
		encoder.Encode(Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("request line exceeds %d bytes", maxMCPBodyBytes),
			},
		})
	}

	// The scan sends a copy of each line, so it can read ahead while the
	// loop answers one. stop releases it when the loop returns first.
	lines := make(chan []byte)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			select {
			case lines <- bytes.Clone(scanner.Bytes()):
			case <-stop:
				return
			}
		}
	}()

	for {
		var line []byte
		var open bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, open = <-lines:
		}
		if !open {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(line) == 0 {
			continue
		}
		if len(line) > maxMCPBodyBytes {
			tooLong()
			return bufio.ErrTooLong
		}

		// Same envelope rule as the HTTP transports (decodeEnvelope):
		// the executor can never resolve an envelope a
		// first-occurrence parser (audit logger, WAF, stdio wrapper)
		// read differently.
		var req Request
		notification, errResp := decodeEnvelope(line, &req)
		if errResp != nil {
			encoder.Encode(*errResp)
			continue
		}

		// A notification (no id member) gets no reply: JSON-RPC
		// forbids one, and a stdio client reads an id-less error line
		// as a protocol fault. The client sends
		// notifications/initialized right after the handshake.
		if notification {
			continue
		}

		resp := s.HandleRequest(ctx, req)
		_, _ = out.Write(encodeResponse(resp))
	}

	// lines closed: the scan has finished, so its error is settled.
	err := scanner.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		// The scanner cannot resume past the oversized line.
		tooLong()
	}
	return err
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
			s.sseStream(w, r, path)
			return
		}

		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
	})

	return mux
}

// ssePostHandler handles POST requests with optional SSE streaming.
func (s *Server) ssePostHandler(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonCrossOriginHost)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	var req Request
	ok, notification := decodeMCPRequest(w, r, &req)
	if !ok {
		return
	}
	// A notification gets 202 and no body, as stdio sends no reply.
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)

	// Check if client wants SSE streaming via Accept header
	if wantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		resp := s.HandleRequest(ctx, req)
		data := bytes.TrimSuffix(encodeResponse(resp), []byte("\n"))
		StreamSSE(w, "message", string(data))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	// Standard JSON response
	resp := s.HandleRequest(ctx, req)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(encodeResponse(resp))
}

// sseGetHandler serves the held stream for a handler not built by
// ServeSSE, which knows no mount path: its endpoint event keeps the
// historical "/sse".
func (s *Server) sseGetHandler(w http.ResponseWriter, r *http.Request) {
	s.sseStream(w, r, "/sse")
}

// sseStream sets up an SSE connection for streaming and then HOLDS
// it: the connection stays open for the life of the client, carrying
// the server-initiated MCP notifications (notifications/*) fanned out
// to the per-subscriber registry in notifications.go. Its opening
// endpoint event names path, where ServeSSE mounted the POST half.
func (s *Server) sseStream(w http.ResponseWriter, r *http.Request, path string) {
	// Same gate as ssePostHandler. This handler used to discard the
	// request entirely, so the origin/Host check simply did not run on
	// the GET half of the pair, a guard hole rather than a disclosure
	// today (the event below is static), but the next thing streamed
	// from here would be a cross-origin read.
	if !s.originOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonCrossOriginHost)
		return
	}

	// The caller identity the gate, the delivery-time filters, and the
	// seat cap all evaluate against: the request context, with the
	// inbound *http.Request stashed via WithRequest.
	ctx := context.WithValue(r.Context(), contextKey{}, r)

	// Server-wide gate at CONNECT time too. The per-notification
	// evaluation in the write loop below stays (a gate can depend on
	// state that changes mid-connection, and a revoked stream must stop
	// receiving immediately); this front check closes the other half: a
	// caller the gate refuses outright never takes a seat, instead of
	// holding one goroutine and one buffered channel per connection for
	// the stream's whole life — the DoS shape the seat cap exists for.
	if err := s.checkServerGate(ctx); err != nil {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return
	}

	// Seat admission: the per-caller cap on concurrent streams
	// (SetSSESeatCap / SetSSESeatOverflow). Refusal happens before any
	// byte of the stream is written, so a refused caller is answered,
	// not left holding an empty one.
	key := sseSeatKey(ctx, r)
	sub, ok := s.addSSESubscriber(ctx, key)
	if !ok {
		i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
		return
	}
	defer s.removeSSESubscriber(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// rc carries per-write deadlines down to the connection; ew
	// surfaces the first write error, which StreamSSE's io.Writer API
	// cannot return. writeSSEEvent arms a fresh deadline, writes one
	// event, flushes, and reports whether the stream is still alive.
	// On failure the handler returns and the deferred
	// removeSSESubscriber unregisters this stream, so fan-out stops
	// targeting it.
	rc := http.NewResponseController(w)
	ew := &errWriter{w: w}
	writeSSEEvent := func(event, data string) bool {
		if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
			// http.ErrNotSupported means the ResponseWriter chain has
			// no deadline support (a wrapped writer with no connection
			// underneath — middleware, tests): that is "no deadline
			// available", not a dead stream, so the write is still
			// attempted. Any other error means arming the deadline
			// itself failed; fail the stream rather than risk an
			// unbounded write.
			if !errors.Is(err, http.ErrNotSupported) {
				return false
			}
		}
		StreamSSE(ew, event, data)
		if ew.err != nil {
			return false
		}
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			// Flush has the same wrapped-writer escape as the
			// deadline: a chain without a Flusher was always skipped,
			// never fatal.
			return false
		}
		// Clear the deadline once the event is flushed: on HTTP/2 it is
		// a stream timer that resets the stream when it fires, write
		// pending or not, so one left armed kills a stream idle between
		// notifications.
		_ = rc.SetWriteDeadline(time.Time{})
		return true
	}

	// Send initial connection event
	if !writeSSEEvent("endpoint", path) {
		return
	}

	// callerHeld re-establishes the caller: the context was built at
	// connect, so on its own it keeps answering for a session since
	// revoked or a token since expired. A caller a fresh request would no
	// longer carry has the stream closed, as framework/crud's event
	// stream does. So does one the re-check placed in another tenant
	// (tenant.ResolveMiddleware re-resolves it): the stream was admitted
	// for the connect-time tenant, and a gate that does not read the
	// tenant would keep delivering its URIs.
	callerHeld := func() (context.Context, bool) {
		live, held := handler.RecheckPrincipal(sub.ctx)
		if !held || handler.GetTenantID(live) != handler.GetTenantID(sub.ctx) {
			return nil, false
		}
		return live, true
	}

	// The keep-alive tick re-checks the caller and writes an SSE comment
	// on a fixed interval; a delivery does not reset it. Without it the
	// re-check ran only before
	// a delivery, and a revoked caller held its seat for as long as no
	// notification was raised; a half-open connection was never written
	// to, so it was never found dead either.
	every := s.sseKeepAlive
	if every <= 0 {
		every = sseKeepAliveEvery
	}
	keepAlive := time.NewTicker(every)
	defer keepAlive.Stop()

	// Hold the connection until the client goes away. Each notification
	// is filtered per subscriber (server-wide gate + the item's gate,
	// both evaluated HERE at send time — see subscriberMayReceive) and
	// flushed as one SSE event.
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected. The deferred removeSSESubscriber
			// unregisters; nothing else is needed.
			return
		case <-keepAlive.C:
			if _, held := callerHeld(); !held {
				return
			}
			if !writeSSEComment(rc, ew) {
				return
			}
		case n, ok := <-sub.ch:
			if !ok {
				// The publisher dropped this subscriber for
				// backpressure (buffer full: a stalled stream loop),
				// or the seat policy evicted it for a newer stream.
				// Returning closes the stream — the client's recovery
				// is to reconnect and re-list. See notifySubscribers.
				return
			}
			// Re-establish the caller before every delivery too (see
			// callerHeld).
			live, held := callerHeld()
			if !held {
				return
			}
			if !s.subscriberMayReceive(live, n) {
				continue
			}
			data, err := json.Marshal(jsonrpcNotification{
				JSONRPC: "2.0",
				Method:  n.method,
				Params:  n.params,
			})
			if err != nil {
				// params are server-built strings; unreachable, but a
				// marshalling failure must never kill the stream.
				continue
			}
			if !writeSSEEvent("message", string(data)) {
				// The write failed — deadline exceeded on a client
				// that stopped reading, or the connection is gone.
				// Return: the deferred removeSSESubscriber unregisters
				// the stream. Closing the connection is the client's
				// signal to reconnect and re-list, which restores it.
				return
			}
		}
	}
}

// writeSSEComment writes one SSE comment line (": keep-alive") under a
// fresh write deadline, flushes it, and clears the deadline again, the
// same discipline writeSSEEvent keeps. A consumer ignores comment lines,
// so the stream's events are unchanged; the write is what finds a dead
// connection. It reports whether the stream is still alive.
func writeSSEComment(rc *http.ResponseController, ew *errWriter) bool {
	if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	if _, err := io.WriteString(ew, ": keep-alive\n\n"); err != nil {
		return false
	}
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	_ = rc.SetWriteDeadline(time.Time{})
	return true
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

// errWriter records the first write error, so the SSE stream loop can
// detect a failed write through StreamSSE — an io.Writer API that
// cannot return one itself. Once a write fails, every later write
// short-circuits to the same error without touching w.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		ew.err = err
	}
	return n, err
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
	for line := range strings.SplitSeq(nd, "\n") {
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
