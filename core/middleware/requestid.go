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

// MaxRequestIDLen caps client-supplied X-Request-ID values. Above this
// length the inbound header is rejected and a fresh ID generated.
//
// Without a cap, an attacker can stuff arbitrary multi-KB strings into
// every request — they'd be logged in every access entry, reflected on
// the response, and amplified across webhook sinks.
const MaxRequestIDLen = 128

// validRequestIDChar accepts the conventional UUID alphabet plus a few
// adjacent characters operators commonly use for tagging (period, slash
// is intentionally NOT included to avoid path-injection ambiguity).
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

// RequestID returns middleware that assigns a unique ID to each request.
// If an X-Request-ID header is present, well-formed (≤MaxRequestIDLen,
// alphanumeric / dot / dash / underscore), it is reused. Otherwise a
// new UUID v4 is generated.
//
// Validation defends against: (1) huge headers amplified across every
// log entry, (2) header-reflection into response, (3) control chars
// (already blocked by net/http) — but our charset is stricter.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if !validRequestID(id) {
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
