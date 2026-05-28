package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
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

// ServeHTTP handles HTTP POST requests for MCP JSON-RPC calls.
// It reads a JSON-RPC request from the body and writes the response.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
func (s *Server) sseGetHandler(w http.ResponseWriter, _ *http.Request) {
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

// StreamSSE writes a single SSE event to the writer. It is the
// hardened entry point for tool-result streaming and treats both
// arguments as untrusted:
//
//   - the event name is truncated at the first CR/LF/NUL so the
//     caller can't terminate the "event:" field and inject a forged
//     directive below it.
//   - data is collapsed to a single line (CR/LF/NUL stripped) and any
//     occurrence of a literal SSE directive marker (e.g. "event:",
//     "id:", "retry:", "data:") inside the payload has its colon
//     replaced with " -" so the bytes can no longer be mistaken
//     for — or substring-matched as — an injected directive.
func StreamSSE(w io.Writer, event, data string) {
	event = stripSSEField(event)
	data = neutralizeSSEDataPayload(data)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// neutralizeSSEDataPayload collapses a multi-line payload to a single
// line and disarms any inline SSE-directive lookalike. SSE data
// legitimately MAY contain newlines (the spec splits on '\n' and
// re-prefixes each line with "data: "), but for the MCP streaming
// entry point we deliberately go stricter: we don't want the
// serialized output to *substring-match* a forged directive at all,
// because downstream consumers may scan rather than parse.
func neutralizeSSEDataPayload(s string) string {
	// Strip frame-terminating / line-terminating bytes outright.
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\x00", "")
	// Defang the four reserved SSE field markers anywhere in the
	// payload. Replacing the trailing colon is enough to break the
	// "<name>:" pattern that defines an SSE directive.
	for _, marker := range []string{"event:", "id:", "retry:", "data:"} {
		s = strings.ReplaceAll(s, marker, strings.TrimSuffix(marker, ":")+" ")
	}
	return s
}

// stripSSEField truncates at the first CR/LF/NUL — those bytes
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
