package router

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func (r *Router) fallbackFor(req *http.Request, pick func(*Router) http.Handler, below *Router) http.Handler {
	root := r.root
	root.mu.RLock()
	groups := slices.Clone(root.fallbackGroups)
	root.mu.RUnlock()
	belowAt := slices.Index(groups, below)
	if below != nil && belowAt < 0 {
		groups = nil
	}
	bestAt := -1
	var bestH http.Handler
	for i, g := range groups {
		if below != nil && (g == below || !moreSpecific(groups, belowAt, i)) {
			continue
		}
		if bestAt >= 0 && !moreSpecific(groups, i, bestAt) {
			continue
		}
		if !g.servedBy(r) || !prefixCovers(g.prefix, req) {
			continue
		}
		if h := pick(g); h != nil {
			bestAt, bestH = i, h
		}
	}
	if bestAt >= 0 {
		return bestH
	}
	for cur := r; cur != nil; cur = cur.parent {
		if cur == below {
			continue
		}
		if h := pick(cur); h != nil {
			return h
		}
	}
	return nil
}

func moreSpecific(groups []*Router, a, b int) bool {
	if b < 0 {
		return true
	}
	ha, pa := splitGroupPrefix(groups[a].prefix)
	hb, pb := splitGroupPrefix(groups[b].prefix)
	if (ha != "") != (hb != "") {
		return ha != ""
	}
	if len(pa) != len(pb) {
		return len(pa) > len(pb)
	}
	for i := range pa {
		wa, wb := strings.HasPrefix(pa[i], "{"), strings.HasPrefix(pb[i], "{")
		if wa != wb {
			return !wa
		}
	}
	return a < b
}

func splitGroupPrefix(prefix string) (string, []string) {
	host := ""
	if prefix != "" && prefix[0] != '/' {
		host, prefix, _ = strings.Cut(prefix, "/")
	}
	var segs []string
	for seg := range strings.SplitSeq(prefix, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	return host, segs
}

func (g *Router) servedBy(r *Router) bool {
	for cur := g; cur != nil; cur = cur.parent {
		if cur == r {
			return true
		}
	}
	return false
}

func prefixCovers(prefix string, req *http.Request) bool {
	if prefix == "" {
		return true
	}
	if prefix[0] != '/' {
		host, rest, _ := strings.Cut(prefix, "/")
		reqHost := req.Host
		if h, _, err := net.SplitHostPort(reqHost); err == nil {
			reqHost = h
		}
		if !strings.EqualFold(host, reqHost) {
			return false
		}
		prefix = "/" + rest
	}
	want := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
	got := strings.Split(req.URL.EscapedPath(), "/")
	if len(got) < len(want) {
		return false
	}
	for i, seg := range want {
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}"):
			return true
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			if got[i] == "" {
				return false
			}
		case unescapeSegment(seg) != unescapeSegment(got[i]):
			return false
		}
	}
	return true
}

func unescapeSegment(seg string) string {
	if s, err := url.PathUnescape(seg); err == nil {
		return s
	}
	return seg
}

func ownNotFound(r *Router) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.notFound
}

func ownMethodNotAllowed(r *Router) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.methodNotAllowed
}

func (r *Router) registerFallback() {
	if r.parent == nil {
		return
	}
	root := r.root
	root.mu.Lock()
	if !slices.Contains(root.fallbackGroups, r) {
		root.fallbackGroups = append(root.fallbackGroups, r)
	}
	root.mu.Unlock()
}

// NotFound sets the 404 handler, run through the chain, for misses under r's prefix; replacing one logs a warning.
func (r *Router) NotFound(handler http.Handler) {
	route := newCachedRoute(handler, r, "", "")
	r.mu.Lock()
	replaced := r.notFound != nil
	r.notFound = route
	r.mu.Unlock()
	r.registerFallback()
	if replaced {
		slog.Warn("router: NotFound handler replaced; the previous handler is discarded",
			"hint", "compose with Router.WrapNotFound if both should run")
	}
}

// WrapNotFound replaces the 404 handler with mw(next), next being the previous handler, and the chain runs once.
func (r *Router) WrapNotFound(mw func(next http.Handler) http.Handler) {
	r.mu.Lock()
	prev := r.notFound
	var next http.Handler = http.NotFoundHandler()
	switch {
	case prev != nil:
		if cr, ok := prev.(*cachedRoute); ok {
			next = cr.raw
		} else {
			next = prev
		}
	case r.parent != nil:
		next = r.delegateFallback(ownNotFound, http.NotFoundHandler())
	}
	r.notFound = newCachedRoute(mw(next), r, "", "")
	r.mu.Unlock()
	r.registerFallback()
}

// WrapMethodNotAllowed is WrapNotFound for the 405 handler.
func (r *Router) WrapMethodNotAllowed(mw func(next http.Handler) http.Handler) {
	r.mu.Lock()
	prev := r.methodNotAllowed
	var next http.Handler = http.HandlerFunc(plainMethodNotAllowed)
	switch {
	case prev != nil:
		if cr, ok := prev.(*cachedRoute); ok {
			next = cr.raw
		} else {
			next = prev
		}
	case r.parent != nil:
		next = r.delegateFallback(ownMethodNotAllowed, http.HandlerFunc(plainMethodNotAllowed))
	}
	r.methodNotAllowed = newCachedRoute(mw(next), r, "", "")
	r.mu.Unlock()
	r.registerFallback()
}

type dispatcherKey struct{}

func (g *Router) delegateFallback(pick func(*Router) http.Handler, def http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := g.root
		if d, ok := req.Context().Value(dispatcherKey{}).(*Router); ok {
			origin = d
		}
		h := origin.fallbackFor(req, pick, g)
		if cr, ok := h.(*cachedRoute); ok {
			h = cr.raw
		}
		if h == nil {
			h = def
		}
		h.ServeHTTP(w, req)
	})
}

func plainMethodNotAllowed(w http.ResponseWriter, req *http.Request) {
	i18n.HTTPError(w, req, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
}

// MethodNotAllowed sets the 405 handler; the Allow header is set before it runs inside the chain the path's routes share.
func (r *Router) MethodNotAllowed(handler http.Handler) {
	route := newCachedRoute(handler, r, "", "")
	r.mu.Lock()
	r.methodNotAllowed = route
	r.mu.Unlock()
	r.registerFallback()
}

// ServeHTTP dispatches req to its route, or to the 405 or 404 handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if fn := r.root.preRoute.Load(); fn != nil {
		next, target, completed := preRouteSafely(req, *fn)
		if !completed {
			i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if target != "" {
			r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, target, http.StatusPermanentRedirect)
			})).ServeHTTP(w, req)
			return
		}
		if next != nil {
			req = next
		}
	}
	h, pattern := r.mux.Handler(req)

	if pattern != "" {
		if _, matched := h.(*cachedRoute); matched {
			r.mux.ServeHTTP(w, req)
			return
		}
		var allowed bool
		if strings.ContainsAny(pattern, " \t") {
			allowed = r.gateAllows(req.Context(), pattern)
		} else {
			allowed = r.gateAllows(req.Context(), " "+pattern) && r.gateAllows(req.Context(), req.Method+" "+pattern)
		}
		if allowed {
			r.wrap(h).ServeHTTP(w, req)
			return
		}
	}

	if r != r.root || req.Context().Value(dispatcherKey{}) != nil {
		req = req.WithContext(context.WithValue(req.Context(), dispatcherKey{}, r))
	}
	allowed := r.allowedMethods(req)
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		owner := r.sharedOwner(req, allowed)
		if mna := r.fallbackFor(req, ownMethodNotAllowed, nil); mna != nil {
			if cr, ok := mna.(*cachedRoute); ok && cr.router != owner && !cr.router.servedBy(owner) {
				shared := newCachedRoute(cr.raw, owner, "", "")
				shared.branch = cr.router
				mna = shared
			}
			mna.ServeHTTP(w, req)
			return
		}
		owner.wrap(http.HandlerFunc(plainMethodNotAllowed)).ServeHTTP(w, req)
		return
	}
	if nf := r.fallbackFor(req, ownNotFound, nil); nf != nil {
		nf.ServeHTTP(w, req)
		return
	}
	r.wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	})).ServeHTTP(w, req)
}

func (r *Router) sharedOwner(req *http.Request, methods []string) *Router {
	probe := req.Clone(req.Context())
	var owner *Router
	for _, m := range methods {
		probe.Method = m
		h, _ := r.mux.Handler(probe)
		cr, ok := h.(*cachedRoute)
		if !ok {
			return r
		}
		if owner == nil {
			owner = cr.router
			continue
		}
		for owner != nil && !cr.router.servedBy(owner) {
			owner = owner.parent
		}
	}
	if owner == nil || !owner.servedBy(r) {
		return r
	}
	return owner
}

func (r *Router) allowedMethods(req *http.Request) []string {
	probeMux, methods := r.probe()
	if probeMux == nil {
		return nil
	}

	probe := req.Clone(req.Context())
	var allowed []string
	for _, m := range methods {
		probe.Method = m
		h, _ := probeMux.Handler(probe)
		if pr, ok := h.(probeRoute); ok && r.gateAllows(req.Context(), string(pr)) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}

type probeRoute string

func (probeRoute) ServeHTTP(http.ResponseWriter, *http.Request) {}

func (r *Router) probe() (*http.ServeMux, []string) {
	root := r.root
	root.probeMu.Lock()
	defer root.probeMu.Unlock()

	root.mu.RLock()
	n := len(root.patterns)
	r.root.mu.RUnlock()
	if n == 0 {
		return nil, nil
	}
	if root.probeMux != nil && root.probeN == n {
		return root.probeMux, root.probeMethods
	}

	root.mu.RLock()
	allPatterns := make([]RegisteredRoute, n)
	copy(allPatterns, root.patterns[:n])
	root.mu.RUnlock()

	probeMux := http.NewServeMux()
	seen := make(map[string]bool)
	var methods []string
	for _, rt := range allPatterns {
		if !seen[rt.Method] {
			seen[rt.Method] = true
			methods = append(methods, rt.Method)
		}
		key := rt.Method + " " + rt.Pattern
		func() {
			defer func() { _ = recover() }()
			probeMux.Handle(key, probeRoute(key))
		}()
	}
	if !seen[http.MethodHead] {
		methods = append(methods, http.MethodHead)
	}
	sort.Strings(methods)

	root.probeMux = probeMux
	root.probeMethods = methods
	root.probeN = n
	return probeMux, methods
}
