package log

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"time"
)

// ErrorReport is the payload handed to an ErrorReporter: the error/panic
// value, a stack trace, and the request context surrounding it.
//
// Field names are stable JSON keys so the HTTP reporter's payload is a
// predictable schema for generic collectors. The unexported ctx carries the
// request context for slog span extraction on the default reporter; it is not
// serialized.
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

// ErrorReporter receives application errors. The log plugin invokes it from
// its panic-recovery middleware; app code reaches the same seam via
// Plugin.Reporter. A nil-safe reporter (the default) is always installed, so
// callers may forward errors without a nil check. Implementations must be
// safe for concurrent use.
type ErrorReporter interface {
	Report(r ErrorReport)
}

// noopReporter discards every report. Used when no reporter is configured so
// the recovery middleware never has to nil-check.
type noopReporter struct{}

func (noopReporter) Report(ErrorReport) {}

// SlogErrorReporter is the default ErrorReporter: it writes each report as a
// slog ERROR record through the plugin's logger, preserving the http.panic
// log shape operators and existing tests rely on (msg "http.panic", attr
// "panic" carrying the value). No behaviour beyond the pre-existing log line.
type SlogErrorReporter struct {
	Logger *slog.Logger
}

// Report logs the report at ERROR. The panic value is emitted under the
// "panic" attr key (kept for backward compatibility); route and stack are
// included when present.
func (s SlogErrorReporter) Report(r ErrorReport) {
	if s.Logger == nil {
		return
	}
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := []slog.Attr{
		slog.String("panic", r.Error),
		slog.String("method", r.Method),
		slog.String("path", r.Path),
		slog.String("request_id", r.RequestID),
	}
	if r.Route != "" {
		attrs = append(attrs, slog.String("route", r.Route))
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

// HTTPErrorReporter forwards each report as a JSON POST to a URL, reusing the
// battery's webhook-sink machinery (bounded async queue, exponential backoff
// on 5xx/transport errors, drop-oldest under backpressure). Suitable for
// Slack incoming webhooks (via a small payload adapter at the receiver) and
// generic JSON collectors. No vendor SDK.
//
// Reports are JSON-marshalled and handed to the sink; the sink batches them
// into the standard {"entries":[...]} envelope and POSTs them. Close flushes
// pending reports and must be called on shutdown.
type HTTPErrorReporter struct {
	sink Sink
}

// NewHTTPErrorReporter builds a reporter that POSTs reports to url. opts tune
// batching/retry/headers exactly like WebhookSink.
func NewHTTPErrorReporter(url string, opts WebhookOpts) *HTTPErrorReporter {
	return &HTTPErrorReporter{sink: WebhookSink(url, opts)}
}

// Report marshals r and enqueues it for delivery. A marshal failure (the only
// error path) is silently dropped — the reporter must never block or panic
// the request path.
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

// Close flushes pending reports and releases the sink. Idempotent.
func (h *HTTPErrorReporter) Close() error {
	if h == nil || h.sink == nil {
		return nil
	}
	return h.sink.Close()
}

// CaptureStack returns the current goroutine stack trace, capped to keep
// reports bounded. App code reporting a non-panic error can attach a stack so
// the receiver gets a traceback without a panic having occurred.
func CaptureStack() string {
	return truncateString(string(debug.Stack()), maxStackLen)
}
