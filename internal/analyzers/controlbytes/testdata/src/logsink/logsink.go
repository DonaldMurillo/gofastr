// Package logsink mirrors battery/log's accessMiddleware shape and
// core/middleware's Idempotency Finish log: request snapshots flowing
// into slog attrs and key-value logging. The scrubbed spellings are
// the negatives.
package logsink

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxLen = 2 << 10

// truncate is battery/log's truncateString: caps length, does NOT
// remove control bytes. Passing through it keeps taint.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// scrubCtl is the repo's scrubControlBytes: the name says what it does.
func scrubCtl(s string) string {
	if !strings.ContainsAny(s, "\x00\x01\x1b\x7f") {
		return s
	}
	var b strings.Builder
	for i := range s {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func remote(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return truncate(strings.TrimSpace(xff), maxLen)
	}
	return r.RemoteAddr
}

type countingWriter struct {
	http.ResponseWriter
	status int
}

// badAccess is the pre-fix accessMiddleware reduced to the shape: the
// raw snapshot reaches slog.String values.
func badAccess(logger *slog.Logger, w http.ResponseWriter, r *http.Request, start time.Time) {
	method := r.Method
	path := truncate(r.URL.Path, maxLen)
	fwd := truncate(r.Header.Get("X-Forwarded-For"), maxLen)
	rw := &countingWriter{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		logger.LogAttrs(r.Context(), slog.LevelInfo, "http.access",
			slog.String("method", method), // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.String("path", path),     // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.Int("status", rw.status),
			slog.String("forwarded_for", fwd),             // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.String("host", r.Host),                   // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.String("query", r.URL.RawQuery),          // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.Any("agent", r.Header.Get("User-Agent")), // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
			slog.String("form", r.FormValue("x")),         // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
		)
	}()
}

// goodAccess is the fixed spelling: every request-derived value passes
// through the scrub before the sink. The remote helper receives the
// whole *http.Request, which is not a source expression (only its
// string selectors are), so it needs no scrub HERE to stay quiet — but
// every string it returns that did derive inside must be scrubbed by
// the caller that logged it; remote itself scrubs below.
func goodAccess(logger *slog.Logger, w http.ResponseWriter, r *http.Request, start time.Time) {
	method := scrubCtl(r.Method)
	path := truncate(scrubCtl(r.URL.Path), maxLen)
	fwd := truncate(scrubCtl(r.Header.Get("X-Forwarded-For")), maxLen)
	rw := &countingWriter{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		logger.LogAttrs(r.Context(), slog.LevelInfo, "http.access",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", rw.status),
			slog.String("forwarded_for", fwd),
			slog.String("remote", scrubCtl(remote(r))),
		)
	}()
}

// badKv is core/middleware's Idempotency Finish-failure log: the key
// came straight from a header.
func badKv(logger *slog.Logger, r *http.Request) error {
	key := r.Header.Get("Idempotency-Key")
	if err := storeFinish(key); err != nil {
		logger.Error("idempotency: Finish failed", "key", key, "error", err) // want `controlbytes: request-derived value reaches logger.Info/Warn/Error key-value unscrubbed`
		return err
	}
	return nil
}

// goodKv is the fixed spelling.
func goodKv(logger *slog.Logger, r *http.Request) error {
	key := scrubCtl(r.Header.Get("Idempotency-Key"))
	if err := storeFinish(key); err != nil {
		logger.Error("idempotency: Finish failed", "key", key, "error", err)
		return err
	}
	return nil
}

func mixedKv(logger *slog.Logger, r *http.Request) {
	// The diagnostic lands on the Warn call: r.Method taints the
	// entry, while EscapedPath re-encodes and does not clear what is
	// already reported once per call.
	logger.Warn("relay: upstream unreachable", // want `controlbytes: request-derived value reaches logger.Info/Warn/Error key-value unscrubbed`
		"method", r.Method,
		"path", r.URL.EscapedPath(),
		"remote", net.JoinHostPort(r.Host, "1"),
	)
}

func storeFinish(key string) error { return nil }
