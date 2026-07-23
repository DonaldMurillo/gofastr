// Package dev provides dev-mode-only helpers (livereload, debug surfaces).
// Everything in this package no-ops in production by design.
package dev

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// LiveReloadStreamURL is the SSE endpoint the dev client connects to.
// LiveReloadScriptURL is the tiny EventSource client script.
const (
	LiveReloadStreamURL = "/__livereload"
	LiveReloadScriptURL = "/__livereload.js"
)

// heartbeatInterval governs how often the SSE handler emits a comment
// to keep intermediaries from timing the connection out. Exported as a
// package var so tests can swap it for a short value and exercise the
// Write-error bail path without waiting 25 seconds.
var heartbeatInterval = 25 * time.Second

// LiveReloadEnabled reports whether dev-mode livereload should auto-wire.
//
// Rules:
//   - GOFASTR_ENV looking like a non-dev environment → always off
//     (case-insensitive: production, prod, live, staging).
//   - GOFASTR_DEV must be set to a strconv.ParseBool truthy value
//     (1, t, T, TRUE, true, True) — typically by `gofastr dev`.
//     Unparseable / falsy values keep livereload off so a stray
//     GOFASTR_DEV="false" or ="no" never trips it on.
//   - GOFASTR_DEV_LIVERELOAD set to a falsy ParseBool value → opt-out
//     even when GOFASTR_DEV is set.
//   - Otherwise on.
func LiveReloadEnabled() bool {
	if isNonDevEnv(os.Getenv("GOFASTR_ENV")) {
		return false
	}
	if !envBool("GOFASTR_DEV") {
		return false
	}
	if v := os.Getenv("GOFASTR_DEV_LIVERELOAD"); v != "" {
		// Explicit opt-out wins; explicit opt-in is the default.
		b, err := strconv.ParseBool(v)
		if err == nil && !b {
			return false
		}
	}
	return true
}

// Enabled reports whether this process is running under `gofastr dev`:
// GOFASTR_DEV is ParseBool-truthy and GOFASTR_ENV does not name a
// production-like environment. This is the base predicate the per-feature
// gates (LiveReloadEnabled, DevMCPEnabled) refine with their own opt-outs;
// use it for behavior that should exist in dev and simply not exist in
// production (e.g. uihost strict mode's axe-coverage check, whose input
// is a local test artifact that never ships).
func Enabled() bool {
	return !isNonDevEnv(os.Getenv("GOFASTR_ENV")) && envBool("GOFASTR_DEV")
}

// isNonDevEnv returns true for any env value that names a production
// or production-like environment. Compared case-insensitively against
// a small allow-list to defeat "GOFASTR_ENV=prod" / "PRODUCTION" /
// "Live" slipping through an exact-string check.
func isNonDevEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod", "live", "staging":
		return true
	}
	return false
}

// envBool reports whether the named env var is set to a ParseBool-true
// value. Anything else (unset, empty, "false", "no", garbage) is false.
// Deliberately strict — dev-mode features must not turn on by accident
// in production-leaning environments.
func envBool(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// registered tracks routers that already have livereload wired so calls from
// both framework.NewApp and an explicit user RegisterLiveReload don't race
// or double-register the same handler.
var (
	registeredMu sync.Mutex
	registered   = map[*router.Router]bool{}
)

// RegisterLiveReload installs the SSE endpoint and client script on r.
// Idempotent — safe to call multiple times on the same router. If the
// router already has either pattern registered (e.g. the host wired a
// hand-rolled livereload before upgrading), the call is a no-op so the
// upgrade path doesn't panic on http.ServeMux duplicate-pattern.
func RegisterLiveReload(r *router.Router) {
	if r == nil {
		return
	}
	registeredMu.Lock()
	if registered[r] {
		registeredMu.Unlock()
		return
	}
	registered[r] = true
	registeredMu.Unlock()

	// Pre-existing routes win — leave the host's own implementation
	// alone. http.ServeMux panics on duplicate pattern registration,
	// so this guard converts the panic into a quiet skip.
	for _, rt := range r.Routes() {
		if rt.Pattern == LiveReloadStreamURL || rt.Pattern == LiveReloadScriptURL {
			return
		}
	}

	buildID := strconv.FormatInt(time.Now().UnixNano(), 10)
	r.Get(LiveReloadStreamURL, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		if _, err := fmt.Fprintf(w, "event: ready\ndata: %s\n\n", buildID); err != nil {
			return
		}
		fl.Flush()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case <-ticker.C:
				// Bail when the underlying conn is gone so the goroutine
				// doesn't leak past a stalled or closed client. The error
				// surfaces once the kernel send buffer fails to drain.
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				fl.Flush()
			}
		}
	}))
	r.Get(LiveReloadScriptURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(liveReloadJS))
	}))

	// HTML responses that don't already carry the client get it injected, so
	// non-uihost surfaces (static file serving, widget pages, hand-rolled
	// handlers) refresh in the browser too. Guarded by the same `registered`
	// map above, so the middleware mounts at most once per router.
	r.Use(LiveReloadHTMLInjector())
}

// MaybeRegisterLiveReload calls RegisterLiveReload only if LiveReloadEnabled()
// returns true. Used by framework.NewApp to auto-wire without forcing the
// route on every test that constructs a router.
func MaybeRegisterLiveReload(r *router.Router) {
	if LiveReloadEnabled() {
		RegisterLiveReload(r)
	}
}

// SetHeartbeatIntervalForTest swaps the SSE heartbeat interval and
// returns a restore function. For tests only — production callers must
// use the default. Pass *testing.T to make the intent unambiguous.
func SetHeartbeatIntervalForTest(_ interface{ Helper() }, d time.Duration) func() {
	prev := heartbeatInterval
	heartbeatInterval = d
	return func() { heartbeatInterval = prev }
}

// liveReloadJS — SSE-based change detection.
//
// The browser opens a single EventSource. The server sends a "ready" event
// with a build ID (unique per server process). On reconnection (server
// restarted after rebuild), the client compares the new build ID against
// the stored one. Only a changed build ID triggers location.reload().
// Transient reconnects (network blip, proxy timeout) produce the same
// build ID → no reload → no lost page state.
const liveReloadJS = `(() => {
  let lastBuildId = null;
  const connect = () => {
    const es = new EventSource('/__livereload');
    es.addEventListener('ready', (e) => {
      if (e.data && e.data !== lastBuildId) {
        if (lastBuildId !== null) {
          location.reload();
          return;
        }
        lastBuildId = e.data;
      }
    });
    es.addEventListener('error', () => {});
  };
  connect();
})();`
