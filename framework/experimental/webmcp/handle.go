package webmcp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// HandleOption customizes one Handle registration.
type HandleOption func(*handleConfig)

type handleConfig struct {
	middleware []router.Middleware
}

// WithHTTPMiddleware wraps the tool's route handler with mw, outermost first.
func WithHTTPMiddleware(mw ...router.Middleware) HandleOption {
	return func(c *handleConfig) { c.middleware = append(c.middleware, mw...) }
}

// Handle declares t as Register does and binds handler on rt at t's method
// and path; on any error it registers neither.
func (h *Host) Handle(rt *router.Router, t Tool, handler http.Handler, opts ...HandleOption) error {
	var cfg handleConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	declared := t
	t, class, err := validateTool(t)
	if err != nil {
		h.emitRegister(declared, class)
		return err
	}
	pattern := pathPortion(t.Path)
	h.mu.Lock()
	class, err = h.admitLocked("Handle", t)
	if err == nil {
		for _, route := range rt.Routes() {
			if route.Method == t.Method && route.Pattern == full(rt, pattern) {
				class = "route_conflict"
				err = fmt.Errorf("webmcp: Handle(%q): %s %s is already registered on this router; one endpoint cannot serve two tool declarations (and the manifest must not advertise a path the router would panic on)", t.Name, t.Method, full(rt, pattern))
				break
			}
		}
	}
	if err == nil {
		final := handler
		for i := len(cfg.middleware) - 1; i >= 0; i-- {
			final = cfg.middleware[i](final)
		}
		if h.observer != nil {
			final = h.observeHandler(t, final)
		}
		if perr := registerRoute(rt, t.Method, pattern, final); perr != nil {
			class = "route_conflict"
			err = fmt.Errorf("webmcp: Handle(%q): %s %s conflicts with a route already on this router: %v", t.Name, t.Method, full(rt, pattern), perr)
		} else {
			t.Path = full(rt, t.Path)
			h.names[t.Name] = true
			h.tools = append(h.tools, t)
		}
	}
	h.mu.Unlock()
	if err != nil {
		h.emitRegister(t, class)
	}
	return err
}

func pathPortion(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

func full(rt *router.Router, pattern string) string { return rt.Prefix() + pattern }

func registerRoute(rt *router.Router, method, pattern string, h http.Handler) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	rt.Handle(method, pattern, h)
	return nil
}
