package app

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/routeseg"
)

// Router maps paths to screens and layouts.
type Router struct {
	screens       map[string]*Screen // path → screen (exact matches)
	dynamic       []dynamicRoute     // pattern-based routes
	exactRedir    map[string]string  // exact from → to (permanent redirect)
	patternRedir  []patternRedirect  // dynamic from → to (param passthrough)
	defaultLayout *Layout
	layoutsGen    atomic.Uint64
}

type dynamicRoute struct {
	segments   []string // e.g. ["products", ":slug"]
	ParamNames []string // e.g. ["slug"]
	screen     *Screen
}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{
		screens: make(map[string]*Screen),
	}
}

// Screen registers a screen with an optional layout.
func (r *Router) Screen(screen *Screen, layout *Layout) {
	r.layoutsGen.Add(1)
	if layout != nil {
		screen.Layout = layout
	}

	screen.Path = canonicalRoutePattern(screen.Path)
	if screen.Intercept != nil {
		screen.Intercept = normalizeIntercept(screen.Intercept)
	}
	RefuseRetiredScreenMethods("screen "+screen.Path, screen.Component)

	if !strings.Contains(screen.Path, ":") {
		if _, ok := r.exactRedir[screen.Path]; ok {
			panic("app: screen " + screen.Path + " collides with a registered redirect")
		}
		exactSegs := strings.Split(strings.Trim(screen.Path, "/"), "/")
		for _, pr := range r.patternRedir {
			if patternsOverlap(pr.segments, exactSegs) {
				panic("app: screen " + screen.Path + " overlaps a registered redirect. " +
					"Redirects are consulted before screens, so the screen would be unreachable on shared URLs")
			}
		}
		r.screens[screen.Path] = screen
		return
	}

	parts := strings.Split(strings.Trim(screen.Path, "/"), "/")
	paramNames := validateDynamicSegments(screen.Path, parts)
	if len(paramNames) > 0 {
		if _, ok := screen.Component.(ParamSetter); !ok {
			panic("app: route " + screen.Path + " is dynamic but " +
				fmt.Sprintf("%T", screen.Component) +
				" does not implement SetParams. Params would be silently dropped")
		}
	}
	for _, pr := range r.patternRedir {
		if patternsOverlap(pr.segments, parts) {
			panic("app: screen " + screen.Path + " overlaps a registered redirect. " +
				"Redirects are consulted before screens, so the screen would be unreachable on shared URLs")
		}
	}
	r.dynamic = append(r.dynamic, dynamicRoute{
		segments:   parts,
		ParamNames: paramNames,
		screen:     screen,
	})
}

func normalizeRoutePath(path string) string {
	if !strings.Contains(path, "{") {
		return path
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if len(p) < 3 || p[0] != '{' || p[len(p)-1] != '}' {
			continue
		}
		inner := p[1 : len(p)-1] // strip braces
		if before, ok := strings.CutSuffix(inner, "..."); ok {
			parts[i] = ":" + before + "*"
		} else {
			parts[i] = ":" + inner
		}
	}
	return strings.Join(parts, "/")
}

func canonicalRoutePattern(path string) string {
	path = normalizeRoutePath(path)
	if strings.Contains(path, ":") {
		return "/" + strings.Trim(path, "/")
	}
	return path
}

// DefaultLayout sets the default layout for screens without one.
func (r *Router) DefaultLayout(layout *Layout) {
	r.layoutsGen.Add(1)
	r.defaultLayout = layout
}

// GetDefaultLayout returns the layout currently set as the default for screens that don't declare one.
func (r *Router) GetDefaultLayout() *Layout {
	return r.defaultLayout
}

// Resolve finds the screen for a given path.
func (r *Router) Resolve(path string) (*Screen, map[string]string, bool) {
	if !strings.Contains(path, "%2F") {
		if s, ok := r.screens[decodeRouteSeg(path)]; ok {
			return s, nil, true
		}
	}

	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	for _, dr := range r.dynamic {
		if params, ok := matchDynamic(dr.segments, pathParts); ok {
			return dr.screen, params, true
		}
	}

	return nil, nil, false
}

func matchDynamic(segments, pathParts []string) (map[string]string, bool) {
	params, ok := matchDynamicRouting(segments, pathParts)
	for name, value := range params {
		params[name] = decodeRouteSeg(value)
	}
	return params, ok
}

func matchDynamicRouting(segments, pathParts []string) (map[string]string, bool) {
	lastIdx := len(segments) - 1
	catchAll := lastIdx >= 0 && isCatchAllSeg(segments[lastIdx])
	parts := make([]string, len(pathParts))
	for i, p := range pathParts {
		parts[i] = decodeRouteSeg(p)
	}

	if !catchAll {
		if len(segments) != len(pathParts) {
			return nil, false
		}
		params := make(map[string]string)
		for i, seg := range segments {
			if isParamSeg(seg) {
				if parts[i] == "" {
					return nil, false
				}
				if !routeseg.ConstraintOK(seg, parts[i]) {
					return nil, false
				}
				params[segParamName(seg)] = pathParts[i]
			} else if seg != parts[i] {
				return nil, false
			}
		}
		return params, true
	}

	if len(pathParts) < len(segments) {
		return nil, false
	}
	params := make(map[string]string)
	for i := range lastIdx {
		seg := segments[i]
		if isParamSeg(seg) {
			if parts[i] == "" {
				return nil, false
			}
			if !routeseg.ConstraintOK(seg, parts[i]) {
				return nil, false
			}
			params[segParamName(seg)] = pathParts[i]
		} else if seg != parts[i] {
			return nil, false
		}
	}
	remainder := strings.Join(pathParts[lastIdx:], "/")
	if remainder == "" {
		return nil, false
	}
	params[segParamName(segments[lastIdx])] = remainder
	return params, true
}

func validateDynamicSegments(pathLabel string, parts []string) []string {
	var paramNames []string
	for i, p := range parts {
		if !isParamSeg(p) {
			continue
		}
		if isCatchAllSeg(p) && i != len(parts)-1 {
			panic("app: route " + pathLabel +
				": catch-all segment \"" + p + "\" must be the final segment")
		}
		if c := routeseg.Constraint(p); c != "" {
			if strings.Contains(p, "*") {
				panic("app: route " + pathLabel +
					": a constraint is not allowed on a catch-all segment (\"" + p + "\")")
			}
			if !routeseg.Valid(c) {
				panic("app: route " + pathLabel +
					" has unknown constraint :" + c + " (supported: int, uuid, alpha, alnum)")
			}
		}
		name := segParamName(p)
		if strings.Contains(name, ".") {
			panic("app: route " + pathLabel +
				": malformed dynamic segment \"" + p + "\" (catch-all ellipsis goes last: \"{name...}\")")
		}
		paramNames = append(paramNames, name)
	}
	return paramNames
}

// ScreenByPattern returns the screen registered under the given route pattern (either brace or
// colon syntax; normalized before lookup).
func (r *Router) ScreenByPattern(pattern string) (*Screen, bool) {
	pattern = canonicalRoutePattern(pattern)
	if s, ok := r.screens[pattern]; ok {
		return s, true
	}
	for _, dr := range r.dynamic {
		if dr.screen.Path == pattern {
			return dr.screen, true
		}
	}
	return nil, false
}

func isParamSeg(seg string) bool { return strings.HasPrefix(seg, ":") }

func isCatchAllSeg(seg string) bool {
	return strings.HasPrefix(seg, ":") && strings.HasSuffix(seg, "*")
}

func segParamName(seg string) string {
	name := seg[1:] // strip leading ":"
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i] // strip constraint suffix
	}
	return strings.TrimSuffix(name, "*")
}

// ParamName extracts the bare parameter name from a normalized dynamic route segment (":id",
// ":path*", ":id:int").
func ParamName(seg string) string { return segParamName(seg) }

// RenderRaw renders a screen by path with no policy resolution and no request context.
func (r *Router) RenderRaw(path string) (render.HTML, error) {
	screen, params, ok := r.Resolve(path)
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	comp := screen.newInstance()
	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}

	ctx := context.Background()
	ctx = WithMatch(ctx, newMatch(screen.Path, path, params))

	if screen.Type == ScreenPage {
		chain := resolveChainKeys(r.layoutChainFor(screen), params)
		validateFills(screen, chain)
		validateRequires(screen)
		if len(chain) > 0 {
			content, renderErr := renderScreen(ctx, comp)
			if renderErr != nil {
				return "", screenRenderPanicError(path, renderErr)
			}
			wrapped, err := renderLayoutChain(ctx, chain, wrapArticle(screen, comp, content), nil, nil)
			if err != nil {
				return "", err
			}
			return wrapped, nil
		}
	}
	out, err := renderComponentInScreen(ctx, screen, comp, screen.Title)
	if err != nil {
		return "", screenRenderPanicError(path, err)
	}
	return out, nil
}

// ValidateFills panics for a fill whose outlet is not in its screen's layout chain.
func (r *Router) ValidateFills() {
	for _, screen := range r.screens {
		validateFills(screen, resolveChainKeys(r.layoutChainFor(screen), nil))
	}
	for _, dr := range r.dynamic {
		validateFills(dr.screen, resolveChainKeys(r.layoutChainFor(dr.screen), nil))
	}
}

// ValidateRequires panics for a group Requires name no resolver declaration matches.
func (r *Router) ValidateRequires() {
	for _, screen := range r.screens {
		validateRequires(screen)
	}
	for _, dr := range r.dynamic {
		validateRequires(dr.screen)
	}
}

// Paths returns all registered paths: the exact paths sorted, then the dynamic patterns in
// registration order.
func (r *Router) Paths() []string {
	paths := slices.Sorted(maps.Keys(r.screens))
	for _, dr := range r.dynamic {
		paths = append(paths, "/"+strings.Join(dr.segments, "/"))
	}
	return paths
}

// Screens returns the internal screens map for direct access.
func (r *Router) Screens() map[string]*Screen {
	return r.screens
}

// ScreenGroup registers all screens from a ScreenGroup into the router.
func (r *Router) ScreenGroup(sg *ScreenGroup) {
	for _, screen := range sg.AllScreens() {
		r.Screen(screen, screen.Layout)
	}
}
