package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Caps on log-entry pieces so a handler that panics with a 100 MB
// string doesn't write a 100 MB log line through slog.Default for
// apps that don't load a structured-logging plugin.
const (
	maxRecoveryPanicLen = 4 << 10  // 4 KiB
	maxRecoveryStackLen = 64 << 10 // 64 KiB
	maxRecoveryPathLen  = 2 << 10  // 2 KiB
)

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = " … (truncated)"
	if max <= len(marker) {
		return s[:max]
	}
	return s[:max-len(marker)] + marker
}

// RecoveryFn returns recovery middleware that logs panics via the
// *slog.Logger returned by getLogger. The accessor is called per
// request so a downstream `app.SetLogger` swap takes effect without
// rewiring the middleware chain.
//
// If getLogger is nil or returns nil, slog.Default() is used.
//
// The panic value, stack trace, and URL.Path are truncated to
// reasonable caps (4 KiB / 64 KiB / 2 KiB) so an attacker (or a buggy
// handler) can't drive multi-MB log entries through this middleware.
func RecoveryFn(getLogger func() *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger := slog.Default()
					if getLogger != nil {
						if l := getLogger(); l != nil {
							logger = l
						}
					}
					logger.Error("panic recovered",
						"error", truncate(fmt.Sprint(err), maxRecoveryPanicLen),
						"path", truncate(r.URL.Path, maxRecoveryPathLen),
						"method", r.Method,
						"stack", truncate(string(debug.Stack()), maxRecoveryStackLen),
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Recovery returns middleware that catches panics in downstream handlers
// using slog.Default at request time. Kept as a convenience for code
// without a logger to inject; new framework code should prefer
// RecoveryFn with an explicit logger source.
func Recovery() Middleware {
	return RecoveryFn(nil)
}
