package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

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
//   - nil, or a nil pointer → 204 No Content (a nil slice or map is
//     still JSON "null", 200: a list handler's empty result is content)
//   - ResponseType → delegates to the custom type
//   - any other value → JSON serialization, 200 OK
func Respond(w http.ResponseWriter, r *http.Request, out any) {
	respond(w, r, out, nil)
}

// respond is Respond that sets *started (when non-nil) just before the
// status goes out, so HandlerAdapter's recover knows a later panic (in a
// ResponseType's WriteBody) came after the response began.
func respond(w http.ResponseWriter, r *http.Request, out any, started *bool) {
	begin := func(code int) {
		if started != nil {
			*started = true
		}
		w.WriteHeader(code)
	}
	// A Handler[I, *Out] returning (nil, nil) hands over a typed nil,
	// which is not == nil inside an any: it is still no content, not a
	// 200 "null".
	if isNilValue(out) {
		begin(http.StatusNoContent)
		return
	}
	if rt, ok := out.(ResponseType); ok {
		// Caller-supplied Content-Type is sanitized (C0/DEL removed)
		// before it reaches the ResponseWriter so it cannot smuggle a
		// new header line or terminal-control mischief.
		ct := textsafe.SanitizeControlBytes(rt.ContentType())
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		begin(http.StatusOK)
		rt.WriteBody(w)
		return
	}

	// Default: JSON, encoded before the status goes out: a value JSON
	// cannot encode (NaN, a channel) is a 500, not an empty 200.
	body, err := json.Marshal(out)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	begin(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}

// isNilValue reports whether out is nil or a nil pointer or interface
// held in an any. A nil slice or map is deliberately not "no content":
// a list handler that found no rows returns one, and turning its 200 into
// a bodyless 204 would break every client that parses the body.
func isNilValue(out any) bool {
	if out == nil {
		return true
	}
	switch rv := reflect.ValueOf(out); rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
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
	if err := writeSSEFrame(w, s); err != nil {
		return err
	}
	if canFlush {
		flusher.Flush()
	}
	return nil
}

// writeSSEFrame emits a single SSE frame with id/event/data fields,
// stripping control bytes from id/event (so they can't terminate the
// field and forge a new directive) and re-prefixing every newline-split
// line of data with "data: " (so an embedded blank line can't end the
// frame and inject a second event).
// writeSSEFrame writes one event in a single Write, so a failed write
// is reported once and never leaves half a frame behind a good one.
func writeSSEFrame(w http.ResponseWriter, evt SSE) error {
	var b strings.Builder
	if id := stripSSEField(evt.ID); id != "" {
		fmt.Fprintf(&b, "id: %s\n", id)
	}
	if event := stripSSEField(evt.Event); event != "" {
		fmt.Fprintf(&b, "event: %s\n", event)
	}
	data := sanitizeSSEData(evt.Data)
	for line := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(&b, "data: %s\n", line)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
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
