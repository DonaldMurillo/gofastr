package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ResponseType is an interface for custom response types that control
// how they are serialized and what Content-Type is used.
type ResponseType interface {
	// ContentType returns the MIME type for the response.
	ContentType() string
	// WriteBody writes the response body to w.
	WriteBody(w http.ResponseWriter) error
}

// Respond writes out the response based on out's type:
//   - nil → 204 No Content
//   - ResponseType → delegates to the custom type
//   - any other value → JSON serialization, 200 OK
func Respond(w http.ResponseWriter, r *http.Request, out any) {
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check for custom ResponseType
	if rt, ok := out.(ResponseType); ok {
		w.Header().Set("Content-Type", rt.ContentType())
		w.WriteHeader(http.StatusOK)
		rt.WriteBody(w)
		return
	}

	// Default: JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(out)
}

// HTML is a response type that writes text/html.
type HTML string

func (h HTML) ContentType() string { return "text/html; charset=utf-8" }
func (h HTML) WriteBody(w http.ResponseWriter) error {
	_, err := w.Write([]byte(h))
	return err
}

// SSE is a Server-Sent Event response.
type SSE struct {
	Event string
	Data  string
	ID    string // optional event ID
}

func (s SSE) ContentType() string { return "text/event-stream" }
func (s SSE) WriteBody(w http.ResponseWriter) error {
	flusher, canFlush := w.(http.Flusher)

	if s.ID != "" {
		fmt.Fprintf(w, "id: %s\n", s.ID)
	}
	if s.Event != "" {
		fmt.Fprintf(w, "event: %s\n", s.Event)
	}
	fmt.Fprintf(w, "data: %s\n\n", s.Data)

	if canFlush {
		flusher.Flush()
	}
	return nil
}

// SSEStream writes a channel of SSE events as a streaming response.
func SSEStream(w http.ResponseWriter, events <-chan SSE) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	for evt := range events {
		if evt.ID != "" {
			fmt.Fprintf(w, "id: %s\n", evt.ID)
		}
		if evt.Event != "" {
			fmt.Fprintf(w, "event: %s\n", evt.Event)
		}
		fmt.Fprintf(w, "data: %s\n\n", evt.Data)
		if canFlush {
			flusher.Flush()
		}
	}
}

// RawBytes is a response type for raw bytes with an explicit content type.
type RawBytes struct {
	Data    []byte
	CT      string // content type, e.g. "image/png"
}

func (r RawBytes) ContentType() string { return r.CT }
func (r RawBytes) WriteBody(w http.ResponseWriter) error {
	_, err := w.Write(r.Data)
	return err
}
