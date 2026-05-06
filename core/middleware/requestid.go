package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

// requestIDKey is the context key for the request ID.
type requestIDKey struct{}

const (
	// HeaderRequestID is the HTTP header used to convey the request ID.
	HeaderRequestID = "X-Request-ID"
)

// GetRequestID retrieves the request ID from the given context.
// Returns an empty string if no request ID is present.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestID returns middleware that assigns a unique ID to each request.
// If an X-Request-ID header is already present on the incoming request,
// it is reused. Otherwise a new UUID v4 is generated.
// The ID is stored in the request context and set on the response header.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = newUUIDv4()
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newUUIDv4 generates a random UUID v4 string using crypto/rand.
func newUUIDv4() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	// Set version 4
	buf[6] = (buf[6] & 0x0f) | 0x40
	// Set variant RFC 4122
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
