package router

import (
	"time"
)

// NoTimeout exempts a route or group from the request timeout, as zero does.
const NoTimeout time.Duration = -1

// SetTimeout sets the request-timeout budget middleware.Timeout applies to every route under r.
func (r *Router) SetTimeout(d time.Duration) {
	r.mu.Lock()
	r.timeout = &d
	r.mu.Unlock()
	r.root.timeoutOverrides.Store(true)
}

// SetRouteTimeout sets the budget for one route, keyed by its full "METHOD /pattern"; a key no route has is inert.
func (r *Router) SetRouteTimeout(method, pattern string, d time.Duration) {
	key := method + " " + r.prefix + pattern
	root := r.root
	root.mu.Lock()
	if root.routeTimeouts == nil {
		root.routeTimeouts = make(map[string]time.Duration)
	}
	root.routeTimeouts[key] = d
	root.mu.Unlock()
	root.timeoutOverrides.Store(true)
}

func (r *Router) effectiveRouteTimeout(method, pattern string) (time.Duration, bool) {
	root := r.root
	root.mu.RLock()
	d, ok := root.routeTimeouts[method+" "+pattern]
	root.mu.RUnlock()
	if ok {
		return d, true
	}
	for cur := r; cur != nil; cur = cur.parent {
		cur.mu.RLock()
		t := cur.timeout
		cur.mu.RUnlock()
		if t != nil {
			return *t, true
		}
	}
	return 0, false
}
