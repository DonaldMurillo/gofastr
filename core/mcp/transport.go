package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gofastr/gofastr/core/handler"
)

// ServeHTTP handles HTTP POST requests for MCP JSON-RPC calls.
// It reads a JSON-RPC request from the body and writes the response.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, contextKey{}, r)
	ctx = enrichContext(ctx)

	// Check if client wants SSE streaming via Accept header
	if wantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		resp := s.HandleRequest(ctx, req)
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Send initial connection event
	fmt.Fprintf(w, "event: endpoint\ndata: /sse\n\n")
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

// StreamSSE writes a single SSE event to the writer.
// Useful for streaming tool results.
func StreamSSE(w io.Writer, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

// ensure handler package is imported for context propagation
var _ = handler.SetUser
