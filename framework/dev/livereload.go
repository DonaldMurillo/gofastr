// Package dev holds the dev-mode-only helpers: livereload, the HTML script
// injector, the layout layer and the dev gates. Nothing here runs in production.
package dev

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// LiveReloadStreamURL is the SSE endpoint the dev client connects to.
// LiveReloadScriptURL is the tiny EventSource client script.
const (
	LiveReloadStreamURL = "/__livereload"
	LiveReloadScriptURL = "/__livereload.js"
)

// LiveReloadExposeEnv keeps the livereload stream on a non-loopback listener.
const LiveReloadExposeEnv = "GOFASTR_DEV_LIVERELOAD_EXPOSE"

var heartbeatInterval = 25 * time.Second

// LiveReloadEnabled reports whether livereload should auto-wire: GOFASTR_DEV
// is truthy, GOFASTR_ENV is not production-like, and GOFASTR_DEV_LIVERELOAD
// is not false.
func LiveReloadEnabled() bool {
	return devFeatureEnabled("GOFASTR_DEV_LIVERELOAD")
}

func devFeatureEnabled(optOutEnv string) bool {
	if isNonDevEnv(os.Getenv("GOFASTR_ENV")) {
		return false
	}
	if !config.EnvBool("GOFASTR_DEV") {
		return false
	}
	if v := os.Getenv(optOutEnv); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && !b {
			return false
		}
	}
	return true
}

// Enabled reports whether the process runs under `gofastr dev`: GOFASTR_DEV
// is truthy and GOFASTR_ENV is not production-like.
func Enabled() bool {
	return !isNonDevEnv(os.Getenv("GOFASTR_ENV")) && config.EnvBool("GOFASTR_DEV")
}

func isNonDevEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod", "live", "staging":
		return true
	}
	return false
}

var (
	registeredMu sync.Mutex
	registered   = map[*router.Router]*liveReloadRoute{}
)

type liveReloadRoute struct {
	installed bool
	withdrawn atomic.Bool
}

// Withdrawal is what WithdrawLiveReload did.
type Withdrawal int

const (
	// NotRegistered: r has no livereload.
	NotRegistered Withdrawal = iota
	// Withdrawn: the stream on r now answers 404.
	Withdrawn
	// HostOwned: r serves its own livereload, left untouched.
	HostOwned
)

// WithdrawLiveReload makes the livereload stream on r answer 404 from now on.
func WithdrawLiveReload(r *router.Router) Withdrawal {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	route, ok := registered[r]
	switch {
	case !ok:
		return NotRegistered
	case !route.installed:
		return HostOwned
	}
	route.withdrawn.Store(true)
	return Withdrawn
}

// RegisterLiveReload installs the SSE stream and client script on r and
// adds the client to the HTML injector. It is idempotent and installs
// nothing when r already has either route.
func RegisterLiveReload(r *router.Router) {
	if r == nil {
		return
	}
	registeredMu.Lock()
	if registered[r] != nil {
		registeredMu.Unlock()
		return
	}
	route := &liveReloadRoute{}
	registered[r] = route
	registeredMu.Unlock()

	for _, rt := range r.Routes() {
		if rt.Pattern == LiveReloadStreamURL || rt.Pattern == LiveReloadScriptURL {
			return
		}
	}
	registeredMu.Lock()
	route.installed = true
	registeredMu.Unlock()
	withdrawn := &route.withdrawn

	buildID := strconv.FormatInt(time.Now().UnixNano(), 10)
	r.Get(LiveReloadStreamURL, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if withdrawn.Load() {
			i18n.HTTPError(w, req, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream") //gofastr:allow(unseated) dev-mode livereload with no principal to seat; App.Start withdraws it on a non-loopback bind, so only this machine holds it
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		if _, ok := w.(http.Flusher); !ok {
			i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		sse := stream.NewSSEWriter(w)
		if err := sse.WriteEvent("ready", buildID); err != nil {
			return
		}
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case <-ticker.C:
				if err := sse.WriteComment("ping"); err != nil {
					return
				}
			}
		}
	}))
	r.Get(LiveReloadScriptURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(liveReloadJS))
	}))

	addDevScript(r, LiveReloadScriptURL)
}

// MaybeRegisterLiveReload calls RegisterLiveReload when LiveReloadEnabled.
func MaybeRegisterLiveReload(r *router.Router) {
	if LiveReloadEnabled() {
		RegisterLiveReload(r)
	}
}

// SetHeartbeatIntervalForTest swaps the SSE heartbeat interval for a test
// and returns a restore function.
func SetHeartbeatIntervalForTest(_ interface{ Helper() }, d time.Duration) func() {
	prev := heartbeatInterval
	heartbeatInterval = d
	return func() { heartbeatInterval = prev }
}

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
