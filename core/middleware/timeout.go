package middleware

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// timeoutWriter wraps an http.ResponseWriter with mutex protection so that
// concurrent writes from the handler goroutine and the timeout path cannot
// race on the underlying ResponseWriter.
//
// It transparently passes through Flush and Hijack when the underlying
// writer supports them so SSE handlers and WebSocket upgrades continue to
// work behind the timeout middleware.
type timeoutWriter struct {
	http.ResponseWriter
	mu       sync.Mutex
	timedOut bool
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	return tw.ResponseWriter.Write(p)
}

// Flush passes through to the underlying ResponseWriter when it supports it,
// so SSE handlers (which type-assert to http.Flusher) work behind this
// middleware. Returns silently if the underlying writer does not support
// flushing — same contract as a plain http.Flusher.Flush() call.
func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack passes through when supported so WebSocket upgrades and other
// long-lived connections work behind the timeout middleware. After a
// successful hijack the timeout no longer governs the connection.
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := tw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("timeout middleware: underlying ResponseWriter does not support hijacking")
}

func (tw *timeoutWriter) setTimedOut() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.timedOut = true
}

// Timeout returns middleware that enforces a deadline on request processing.
// If the downstream handler does not complete within the given duration,
// a 504 Gateway Timeout response is returned.
//
// The handler runs in a goroutine; a synchronized response writer prevents
// concurrent writes when the timeout fires.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			tw := &timeoutWriter{ResponseWriter: w}
			done := make(chan struct{})
			go func() {
				next.ServeHTTP(tw, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				// Handler completed normally.
			case <-ctx.Done():
				tw.setTimedOut()
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			}
		})
	}
}
