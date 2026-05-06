package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout returns middleware that enforces a deadline on request processing.
// If the downstream handler does not complete within the given duration,
// a 504 Gateway Timeout response is returned.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				// Handler completed normally.
			case <-ctx.Done():
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			}
		})
	}
}
