package middleware

import (
	"context"
	"net/http"
	"uuid"
)

type requestIDKey struct{}

const (
	// HeaderRequestID is the HTTP header used to convey the request ID.
	HeaderRequestID = "X-Request-ID"
)

// GetRequestID returns the request ID in ctx, or "" when there is none.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// MaxRequestIDLen caps a client-supplied X-Request-ID; a longer one is replaced.
const MaxRequestIDLen = 128

func validRequestIDChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z',
		c >= 'A' && c <= 'Z',
		c >= '0' && c <= '9',
		c == '-', c == '_', c == '.':
		return true
	}
	return false
}

func validRequestID(s string) bool {
	if s == "" || len(s) > MaxRequestIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !validRequestIDChar(s[i]) {
			return false
		}
	}
	return true
}

// RequestID returns middleware that reuses a well-formed X-Request-ID (at most MaxRequestIDLen of [A-Za-z0-9._-]) or generates a UUID v4, and echoes it on the response.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if !validRequestID(id) {
				id = uuid.NewV4().String()
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			w.Header().Set(HeaderRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
