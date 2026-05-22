package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Finding 8a: Flusher must be available through the logging wrapper.
func TestLoggingPreservesFlusher(t *testing.T) {
	var sawFlusher bool
	h := Logging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); ok {
			sawFlusher = true
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rr, req)

	if !sawFlusher {
		t.Fatal("Flusher not preserved through LoggingFn wrapper")
	}
}

// Finding 8b: Hijacker must be available through the logging wrapper.
func TestLoggingPreservesHijacker(t *testing.T) {
	var sawHijacker bool
	h := Logging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			sawHijacker = true
		}
	}))

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)
	if !sawHijacker {
		t.Fatal("Hijacker not preserved through LoggingFn wrapper")
	}
}

// Finding 16: SampledLogging must respect an injected logger source.
func TestSampledLoggingUsesInjectedLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := SampledLoggingFn(1, time.Hour, func() *slog.Logger { return logger })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	}

	if !strings.Contains(buf.String(), "request") {
		t.Fatalf("injected logger did not receive any output; got %q", buf.String())
	}
}

// hijackableRecorder is an httptest.ResponseRecorder that also implements
// http.Hijacker (the standard recorder does not).
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("test hijack: not implemented")
}
