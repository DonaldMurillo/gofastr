package log

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ErrorReport is the payload handed to an ErrorReporter: the error/panic
// value, a stack trace, and the request context surrounding it.
type ErrorReport struct {
	Time      time.Time `json:"time"`
	Message   string    `json:"msg"`              // "http.panic" for recovered panics
	Error     string    `json:"error"`            // the panic value / error message
	Stack     string    `json:"stack,omitempty"`  // captured stack trace
	Method    string    `json:"method,omitempty"` // request method
	Path      string    `json:"path,omitempty"`   // request URL path
	Route     string    `json:"route,omitempty"`  // matched route pattern (r.Pattern)
	RequestID string    `json:"request_id,omitempty"`
	Remote    string    `json:"remote,omitempty"`

	ctx context.Context `json:"-"`
}

// ErrorReporter receives application errors.
type ErrorReporter interface {
	Report(r ErrorReport)
}

type noopReporter struct{}

func (noopReporter) Report(ErrorReport) {}

// SlogErrorReporter is the default ErrorReporter: it writes each report
// as a slog ERROR record through the plugin's logger, preserving the
// http.panic log shape operators and existing tests rely on (msg
// "http.panic", attr "panic" carrying the value).
type SlogErrorReporter struct {
	Logger *slog.Logger
}

// Report logs the report at ERROR.
func (s SlogErrorReporter) Report(r ErrorReport) {
	if s.Logger == nil {
		return
	}
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := []slog.Attr{
		slog.String("panic", textsafe.ScrubControlBytes(r.Error)),
		slog.String("method", textsafe.ScrubControlBytes(r.Method)),
		slog.String("path", textsafe.ScrubControlBytes(r.Path)),
		slog.String("request_id", textsafe.ScrubControlBytes(r.RequestID)),
	}
	if r.Route != "" {
		attrs = append(attrs, slog.String("route", textsafe.ScrubControlBytes(r.Route)))
	}
	if r.Stack != "" {
		attrs = append(attrs, slog.String("stack", r.Stack))
	}
	msg := r.Message
	if msg == "" {
		msg = "http.panic"
	}
	s.Logger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
}

// HTTPErrorReporter forwards each report as a JSON POST to a URL, reusing
// the battery's webhook-sink machinery (bounded async queue, exponential
// backoff on 5xx/transport errors, drop-oldest under backpressure).
type HTTPErrorReporter struct {
	sink Sink
}

// NewHTTPErrorReporter builds a reporter that POSTs reports to url.
func NewHTTPErrorReporter(url string, opts WebhookOpts) *HTTPErrorReporter {
	return &HTTPErrorReporter{sink: WebhookSink(url, opts)}
}

// Report marshals r and enqueues it for delivery.
func (h *HTTPErrorReporter) Report(r ErrorReport) {
	if h == nil || h.sink == nil {
		return
	}
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = h.sink.Write(b)
}

// Close flushes pending reports and releases the sink.
func (h *HTTPErrorReporter) Close() error {
	if h == nil || h.sink == nil {
		return nil
	}
	return h.sink.Close()
}

// CaptureStack returns the current goroutine stack trace, capped to keep
// reports bounded.
func CaptureStack() string {
	return textsafe.Truncate(string(debug.Stack()), maxStackLen)
}
