package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// timeoutWriter wraps an http.ResponseWriter with mutex protection so that
// concurrent writes from the handler goroutine and the timeout path cannot
// race on the underlying ResponseWriter.
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
