package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/sseframe"
)

// ResponseType is an interface for custom response types that control
// how they are serialized and what Content-Type is used.
type ResponseType interface {
	// ContentType returns the MIME type for the response.
	ContentType() string
	// WriteBody writes the response body to w.
	WriteBody(w http.ResponseWriter) error
}

// Respond answers nil or a nil pointer 204, a ResponseType with its own
// type and body, and any other value, a nil slice or map included, as
// JSON with 200.
func Respond(w http.ResponseWriter, r *http.Request, out any) {
	respond(w, r, out, nil)
}

func respond(w http.ResponseWriter, r *http.Request, out any, started *bool) {
	begin := func(code int) {
		if started != nil {
			*started = true
		}
		w.WriteHeader(code)
	}
	if isNilValue(out) {
		begin(http.StatusNoContent)
		return
	}
	if rt, ok := out.(ResponseType); ok {
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

// writeSSEFrame writes one event in a single Write, so a failed write
// leaves no half frame behind.
func writeSSEFrame(w http.ResponseWriter, evt SSE) error {
	var b strings.Builder
	sseframe.Append(&b, evt.ID, evt.Event, false, evt.Data)
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
