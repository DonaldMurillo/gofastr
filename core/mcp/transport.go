package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/mediatype"
	"github.com/DonaldMurillo/gofastr/core/netguard"
)

const maxMCPBodyBytes = 1 << 20

const sseWriteTimeout = 10 * time.Second

const sseKeepAliveEvery = 15 * time.Second

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

func writeEnvelopeError(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(encodeResponse(resp))
}

func encodeResponse(resp Response) []byte {
	b, err := json.Marshal(resp)
	if err == nil {
		return append(b, '\n')
	}
	slog.Error("mcp: response encode failed", slog.String("err", err.Error()))
	b, err = json.Marshal(newErrorResponse(resp.ID, ErrInternalError, "internal error"))
	if err != nil {
		b, _ = json.Marshal(newErrorResponse(nil, ErrInternalError, "internal error")) //gofastr:allow(discardeddecode) the last-resort reply: a null id and a fixed message hold nothing that can fail to encode
	}
	return append(b, '\n')
}

func decodeEnvelope(raw []byte, req *Request) (notification bool, errResp *Response) {
	if !json.Valid(raw) {
		resp := newErrorResponse(nil, ErrParse, "invalid JSON")
		return false, &resp
	}
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
		if req.JSONRPC != "2.0" || req.Method == "" {
			resp := newErrorResponse(nil, ErrInvalidRequest, "invalid request: a notification needs jsonrpc 2.0 and a method")
			return false, &resp
		}
		return true, nil
	}
	return false, nil
}

// ProtocolVersionHeader is the HTTP header a client names its negotiated MCP revision in on every request after initialize.
const ProtocolVersionHeader = "MCP-Protocol-Version"

func refuseVersionHeader(w http.ResponseWriter, r *http.Request, req Request) bool {
	values := r.Header.Values(ProtocolVersionHeader)
	if len(values) == 0 || req.Method == "initialize" {
		return false
	}
	requested := strings.TrimSpace(values[0])
	if len(values) == 1 && slices.Contains(handshakeProtocolVersions, requested) {
		return false
	}
	if len(requested) > maxEchoedVersionBytes {
		requested = requested[:maxEchoedVersionBytes]
	}
	msg := "unsupported " + ProtocolVersionHeader
	if len(values) > 1 {
		msg = "repeated " + ProtocolVersionHeader
	}
	resp := newErrorResponse(replyID(req.ID), ErrInvalidRequest, msg)
	resp.Error.Data = map[string]any{"supported": SupportedProtocolVersions(), "requested": requested}
	writeEnvelopeError(w, resp)
	return true
}

// Standard request headers the 2026-07-28 Streamable HTTP transport mirrors from the body.
const (
	MethodHeader = "Mcp-Method"
	NameHeader   = "Mcp-Name"
)

var nameHeaderMethods = map[string]bool{"tools/call": true, "resources/read": true, "prompts/get": true}

func refuseHTTPHeaders(w http.ResponseWriter, r *http.Request, req Request, notification bool) bool {
	if notification {
		if v := r.Header.Values(ProtocolVersionHeader); len(v) == 1 && slices.Contains(modernProtocolVersions, strings.TrimSpace(v[0])) {
			return false
		}
		return refuseVersionHeader(w, r, req)
	}
	facts, rpcErr := readRequestFacts(req)
	if rpcErr != nil {
		return false
	}
	if !facts.modern {
		values := r.Header.Values(ProtocolVersionHeader)
		if len(values) == 1 && slices.Contains(modernProtocolVersions, strings.TrimSpace(values[0])) {
			if req.Method == "server/discover" {
				return false
			}
			writeEnvelopeError(w, headerMismatch(replyID(req.ID), ProtocolVersionHeader+" names a per-request revision but params._meta carries no "+MetaProtocolVersion))
			return true
		}
		return refuseVersionHeader(w, r, req)
	}
	if msg := modernHeaderMismatch(r, req, facts); msg != "" {
		writeEnvelopeError(w, headerMismatch(replyID(req.ID), msg))
		return true
	}
	return false
}

func modernHeaderMismatch(r *http.Request, req Request, facts requestFacts) string {
	version, msg := singleHeader(r, ProtocolVersionHeader)
	if msg != "" {
		return msg
	}
	if version != facts.version {
		return ProtocolVersionHeader + " header does not match params._meta " + MetaProtocolVersion
	}
	method, msg := singleHeader(r, MethodHeader)
	if msg != "" {
		return msg
	}
	if method != req.Method {
		return MethodHeader + " header does not match the request method"
	}
	if !nameHeaderMethods[req.Method] {
		return ""
	}
	name, msg := singleHeader(r, NameHeader)
	if msg != "" {
		return msg
	}
	decoded, ok := decodeHeaderValue(name)
	if !ok {
		return NameHeader + " header is not valid base64 inside its =?base64?...?= sentinel"
	}
	if !facts.hasName || decoded != facts.name {
		return NameHeader + " header does not match the request body"
	}
	return ""
}

func singleHeader(r *http.Request, name string) (string, string) {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return "", "missing " + name + " header"
	}
	if len(values) > 1 {
		return "", "repeated " + name + " header"
	}
	for _, c := range []byte(values[0]) {
		if (c < 0x20 || c > 0x7e) && c != '\t' {
			return "", name + " header holds a character outside visible ASCII, space and tab"
		}
	}
	return values[0], ""
}

func decodeHeaderValue(v string) (string, bool) {
	const prefix, suffix = "=?base64?", "?="
	if !strings.HasPrefix(v, prefix) || !strings.HasSuffix(v, suffix) || len(v) < len(prefix)+len(suffix) {
		return v, true
	}
	b, err := base64.StdEncoding.DecodeString(v[len(prefix) : len(v)-len(suffix)])
	if err != nil || !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}

func headerMismatch(id any, msg string) Response {
	return newErrorResponse(id, ErrHeaderMismatch, "Header mismatch: "+msg)
}

func httpStatusFor(resp Response) int {
	if resp.Error == nil {
		return http.StatusOK
	}
	switch resp.Error.Code {
	case ErrUnsupportedProtocolVersion, ErrHeaderMismatch, ErrMissingRequiredClientCapability:
		return http.StatusBadRequest
	case ErrMethodNotFound:
		if resp.modern {
			return http.StatusNotFound
		}
	}
	return http.StatusOK
}

// replyID is the id a transport-level refusal answers under: the request's own when it is a valid scalar, else null.
func replyID(id any) any {
	if !validRequestID(id) {
		return nil
	}
	return id
}

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

func (s *Server) originOK(r *http.Request) bool {
	s.mu.RLock()
	origins := s.allowedOrigins
	hosts := s.allowedHosts
	loopbackOnly := s.requireLoopbackHost
	s.mu.RUnlock()
	if !netguard.OriginAllowed(r, origins) {
		return false
	}
	if loopbackOnly && !netguard.IsLoopbackAuthority(r.Host) {
		return false
	}
	if len(hosts) == 0 {
		return true
	}
	for _, h := range hosts {
		if strings.EqualFold(r.Host, h) {
			return true
		}
	}
	return false
}

// SetAllowedHosts pins the Host authorities the HTTP transports answer on; an empty list leaves Host unchecked.
func (s *Server) SetAllowedHosts(hosts []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedHosts = append([]string(nil), hosts...)
}

// SetRequireLoopbackHost restricts the HTTP transports to loopback Host authorities; it stops DNS rebinding, not a direct TCP client, so pair it with a loopback bind.
func (s *Server) SetRequireLoopbackHost(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requireLoopbackHost = v
}

// SetAllowedOrigins permits cross-origin browser Origins, such as tunnels.
func (s *Server) SetAllowedOrigins(origins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedOrigins = append([]string(nil), origins...)
}

// ServeHTTP answers one JSON-RPC request per POST.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
		return
	}
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
	if refuseHTTPHeaders(w, r, req, notification) {
		return
	}
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)

	resp := s.HandleRequest(ctx, req)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatusFor(resp))
	_, _ = w.Write(encodeResponse(resp))
}

// ServeStdio answers newline-delimited JSON-RPC from in to out; it returns at EOF, with ctx's error once ctx ends,
// or with the error of a reply it could not write.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxMCPBodyBytes+2)
	encoder := json.NewEncoder(out)
	tooLong := func() error {
		return errors.Join(bufio.ErrTooLong, encoder.Encode(Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrInvalidParams,
				Message: fmt.Sprintf("request line exceeds %d bytes", maxMCPBodyBytes),
			},
		}))
	}

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
			return tooLong()
		}

		var req Request
		notification, errResp := decodeEnvelope(line, &req)
		if errResp != nil {
			if _, err := out.Write(encodeResponse(*errResp)); err != nil {
				return err
			}
			continue
		}

		if notification {
			continue
		}

		resp := s.HandleRequest(ctx, req)
		if _, err := out.Write(encodeResponse(resp)); err != nil {
			return err
		}
	}

	err := scanner.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		return tooLong()
	}
	return err
}

// ServeSSE returns a handler answering JSON-RPC POSTs at path and holding a GET stream of server notifications.
func (s *Server) ServeSSE(path string) http.Handler {
	mux := http.NewServeMux()

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
	if refuseHTTPHeaders(w, r, req, notification) {
		return
	}
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)

	if mediatype.Fold(r.Header.Get("Accept")) == "text/event-stream" {
		resp := s.HandleRequest(ctx, req)
		if status := httpStatusFor(resp); status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(encodeResponse(resp))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(httpStatusFor(resp))

		data := bytes.TrimSuffix(encodeResponse(resp), []byte("\n"))
		StreamSSE(w, "message", string(data))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	resp := s.HandleRequest(ctx, req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatusFor(resp))
	_, _ = w.Write(encodeResponse(resp))
}

func (s *Server) sseGetHandler(w http.ResponseWriter, r *http.Request) {
	s.sseStream(w, r, "/sse")
}

func (s *Server) sseStream(w http.ResponseWriter, r *http.Request, path string) {
	if !s.originOK(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.ReasonCrossOriginHost)
		return
	}

	if v := r.Header.Values(ProtocolVersionHeader); len(v) == 1 && slices.Contains(modernProtocolVersions, strings.TrimSpace(v[0])) {
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
		return
	}
	if refuseVersionHeader(w, r, Request{}) {
		return
	}

	ctx := context.WithValue(r.Context(), contextKey{}, r)

	if err := s.checkServerGate(ctx); err != nil {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return
	}

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
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	ew := &errWriter{w: w}
	writeSSEEvent := func(event, data string) bool {
		if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
			if !errors.Is(err, http.ErrNotSupported) {
				return false
			}
		}
		StreamSSE(ew, event, data)
		if ew.err != nil {
			return false
		}
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return false
		}
		_ = rc.SetWriteDeadline(time.Time{})
		return true
	}

	if !writeSSEEvent("endpoint", path) {
		return
	}

	callerHeld := func() (context.Context, bool) {
		live, held := handler.RecheckPrincipal(sub.ctx)
		if !held || handler.GetTenantID(live) != handler.GetTenantID(sub.ctx) {
			return nil, false
		}
		return live, true
	}

	every := s.sseKeepAlive
	if every <= 0 {
		every = sseKeepAliveEvery
	}
	keepAlive := time.NewTicker(every)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
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
				return
			}
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
				continue
			}
			if !writeSSEEvent("message", string(data)) {
				return
			}
		}
	}
}

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

type contextKey struct{}

// WithRequest stores the inbound request in ctx for tool handlers and gates.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// RequestFromContext returns the inbound request the transport stored, if any.
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(contextKey{}).(*http.Request)
	return r, ok
}

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

// StreamSSE writes one SSE event, cutting event at its first CR, LF or NUL and writing each line of data as its own data line.
func StreamSSE(w io.Writer, event, data string) {
	event = stripSSEField(event)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	nd := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(data)
	for line := range strings.SplitSeq(nd, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

func stripSSEField(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}
