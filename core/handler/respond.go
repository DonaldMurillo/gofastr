package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// sanitizeHeaderValue strips bytes that would otherwise smuggle a new
// header line (CR/LF/NUL) or terminal-control mischief (other C0, DEL)
// into a response header value. Used to scrub caller-supplied
// Content-Type strings before writing them to the ResponseWriter.
func sanitizeHeaderValue(s string) string {
	if !needsHeaderSanitize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func needsHeaderSanitize(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// stripSSEField truncates a single-line SSE field value at the first
// CR/LF/NUL. Those bytes terminate an SSE field and would otherwise let
// a caller-supplied event name or id inject forged directives below it.
func stripSSEField(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}

// sanitizeSSEData removes CR and NUL bytes from a multi-line SSE data
// payload and ensures each '\n'-separated line is re-prefixed with
// "data: " by the caller. Collapsing consecutive newlines prevents a
// blank line ("\n\n") inside the payload from terminating the event
// frame and letting following bytes appear as a forged second frame.
func sanitizeSSEData(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

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
		ct := sanitizeHeaderValue(rt.ContentType())
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		rt.WriteBody(w)
		return
	}

	// Default: JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	writeSSEFrame(w, s)
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	for evt := range events {
		writeSSEFrame(w, evt)
		if canFlush {
			flusher.Flush()
		}
	}
}

// writeSSEFrame emits a single SSE frame with id/event/data fields,
// stripping control bytes from id/event (so they can't terminate the
// field and forge a new directive) and re-prefixing every newline-split
// line of data with "data: " (so an embedded blank line can't end the
// frame and inject a second event).
func writeSSEFrame(w http.ResponseWriter, evt SSE) {
	if id := stripSSEField(evt.ID); id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	if event := stripSSEField(evt.Event); event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	data := sanitizeSSEData(evt.Data)
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// RawBytes is a response type for raw bytes with an explicit content type.
type RawBytes struct {
	Data []byte
	CT   string // content type, e.g. "image/png"
}

func (r RawBytes) ContentType() string { return r.CT }
func (r RawBytes) WriteBody(w http.ResponseWriter) error {
	_, err := w.Write(r.Data)
	return err
}
