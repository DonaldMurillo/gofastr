package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Router maps paths to screens and layouts.
// Supports both exact paths ("/about") and dynamic patterns
// ("/products/:slug"). The "{slug}" brace syntax — used by the
// blueprint, the REST/entity routers, and the entity docs — is also
// accepted and normalized to ":slug" at registration (see
// normalizeRoutePath). Both forms match identically.
type Router struct {
	screens       map[string]*Screen // path → screen (exact matches)
	dynamic       []dynamicRoute     // pattern-based routes
	exactRedir    map[string]string  // exact from → to (permanent redirect)
	patternRedir  []patternRedirect  // dynamic from → to (param passthrough)
	defaultLayout *Layout
}

// dynamicRoute holds a parsed route pattern with parameter extraction.
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
// If layout is nil, the screen will use the default layout (if set).
//
// Both ":param" (the core-ui historical form) and "{param}" (the
// blueprint / REST / entity-doc form) are accepted; the path is
// normalized to ":param" once here so Resolve, Paths, and llm.md
// generation all see one shape (issue #71). A trailing catch-all
// segment ("{path...}" or ":path*") captures one or more remaining
// path segments into a single joined param; it must be the final
// segment.
func (r *Router) Screen(screen *Screen, layout *Layout) {
	if layout != nil {
		screen.Layout = layout
	}

	screen.Path = normalizeRoutePath(screen.Path)

	if !strings.Contains(screen.Path, ":") {
		if _, ok := r.exactRedir[screen.Path]; ok {
			panic("app: screen " + screen.Path + " collides with a registered redirect")
		}
		r.screens[screen.Path] = screen
		return
	}

	// Dynamic route — parse + validate segments.
	parts := strings.Split(strings.Trim(screen.Path, "/"), "/")
	paramNames := validateDynamicSegments(screen.Path, parts)
	// Fail loud at registration: a dynamic route whose component does
	// not implement ParamSetter would have its params silently dropped
	// at request time (SetParams is never called). Boot-time only —
	// mirrors the render-time panic voice in framework/ui/variants.go.
	if len(paramNames) > 0 {
		if _, ok := screen.Component.(ParamSetter); !ok {
			panic("app: route " + screen.Path + " is dynamic but " +
				fmt.Sprintf("%T", screen.Component) +
				" does not implement SetParams — params would be silently dropped")
		}
	}
	for _, pr := range r.patternRedir {
		if patternsOverlap(pr.segments, parts) {
			panic("app: screen " + screen.Path + " overlaps a registered redirect — " +
				"redirects are consulted before screens, so the screen would be unreachable on shared URLs")
		}
	}
	r.dynamic = append(r.dynamic, dynamicRoute{
		segments:   parts,
		ParamNames: paramNames,
		screen:     screen,
	})
}

// normalizeRoutePath converts the "{param}" brace syntax into the
// router's canonical ":param" form, and "{path...}" into the catch-all
// ":path*" form. Only a path segment that is a complete "{...}" is
// rewritten, so paths that legitimately contain a literal brace are
// left untouched. No-op when the path already uses ":param" or has no
// params.
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
		if strings.HasSuffix(inner, "...") {
			parts[i] = ":" + strings.TrimSuffix(inner, "...") + "*"
		} else {
			parts[i] = ":" + inner
		}
	}
	return strings.Join(parts, "/")
}

// DefaultLayout sets the default layout for screens without one.
func (r *Router) DefaultLayout(layout *Layout) {
	r.defaultLayout = layout
}

// GetDefaultLayout returns the layout currently set as the default for
// screens that don't declare one. May return nil. Read-only accessor —
// uihost's not-found path wraps a synthesized 404 component in this
// layout so the error page shares chrome with the rest of the site.
func (r *Router) GetDefaultLayout() *Layout {
	return r.defaultLayout
}

// Resolve finds the screen for a given path.
// Returns the screen, the extracted route params (for dynamic routes), and whether it was found.
// Params are returned separately to avoid mutating the shared screen instance.
//
// Exact-map matches win; among dynamic routes, registration order
// decides precedence (first-match-wins). There is no specificity
// ranking — register more specific patterns first.
func (r *Router) Resolve(path string) (*Screen, map[string]string, bool) {
	// Exact match first
	if s, ok := r.screens[path]; ok {
		return s, nil, true
	}

	// Try dynamic routes in registration order.
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	for _, dr := range r.dynamic {
		if params, ok := matchDynamic(dr.segments, pathParts); ok {
			return dr.screen, params, true
		}
	}

	return nil, nil, false
}

// matchDynamic matches pathParts against a route's normalized segments.
// Fixed-length routes require equal segment counts; a trailing catch-all
// (":name*") consumes one or more remainder segments joined with "/".
// Catch-all routes require at least one remainder segment (so "/docs"
// does not match "/docs/{p...}"). Param values are the raw matched
// text — no URL decoding — consistent with single-segment params.
func matchDynamic(segments, pathParts []string) (map[string]string, bool) {
	lastIdx := len(segments) - 1
	catchAll := lastIdx >= 0 && isCatchAllSeg(segments[lastIdx])

	if !catchAll {
		if len(segments) != len(pathParts) {
			return nil, false
		}
		params := make(map[string]string)
		for i, seg := range segments {
			if isParamSeg(seg) {
				if !constraintOK(seg, pathParts[i]) {
					return nil, false
				}
				params[segParamName(seg)] = pathParts[i]
			} else if seg != pathParts[i] {
				return nil, false
			}
		}
		return params, true
	}

	// Catch-all: prefix segments must align; the remainder (>=1 segment)
	// is joined with "/" into the catch-all param. len(pathParts) ==
	// len(segments) means exactly one remainder segment; fewer means none.
	if len(pathParts) < len(segments) {
		return nil, false
	}
	params := make(map[string]string)
	for i := 0; i < lastIdx; i++ {
		seg := segments[i]
		if isParamSeg(seg) {
			if !constraintOK(seg, pathParts[i]) {
				return nil, false
			}
			params[segParamName(seg)] = pathParts[i]
		} else if seg != pathParts[i] {
			return nil, false
		}
	}
	params[segParamName(segments[lastIdx])] = strings.Join(pathParts[lastIdx:], "/")
	return params, true
}

// validateDynamicSegments applies the dynamic-route registration rules —
// catch-all must be final, no constraint on a catch-all, constraint names
// must be in the allowlist — panicking with pathLabel in the message on
// any violation, and returns the declared param names. Shared by screen
// registration and RedirectPattern so a redirect's from-pattern obeys the
// exact same grammar as a route.
func validateDynamicSegments(pathLabel string, parts []string) []string {
	var paramNames []string
	for i, p := range parts {
		if !isParamSeg(p) {
			continue
		}
		if isCatchAllSeg(p) && i != len(parts)-1 {
			panic("app: route " + pathLabel +
				" — catch-all segment \"" + p + "\" must be the final segment")
		}
		if c := segConstraint(p); c != "" {
			if strings.Contains(p, "*") {
				panic("app: route " + pathLabel +
					" — a constraint is not allowed on a catch-all segment (\"" + p + "\")")
			}
			if !validConstraints[c] {
				panic("app: route " + pathLabel +
					" — unknown constraint :" + c + " (supported: int, uuid, alpha, alnum)")
			}
		}
		name := segParamName(p)
		// A dot in the extracted name means malformed syntax slipped
		// through normalization — e.g. "{p...:int}", where the ellipsis
		// must come LAST ("{p:int...}", itself rejected above). Fail with
		// the real problem instead of registering a garbage param name.
		if strings.Contains(name, ".") {
			panic("app: route " + pathLabel +
				" — malformed dynamic segment \"" + p + "\" (catch-all ellipsis goes last: \"{name...}\")")
		}
		paramNames = append(paramNames, name)
	}
	return paramNames
}

// ScreenByPattern returns the screen registered under the given route
// pattern (either brace or colon syntax; normalized before lookup).
// Enumeration consumers (static export, sitemap, llm.md index) MUST use
// this instead of Resolve(pattern): Resolve treats its argument as a
// REQUEST path, and a constrained pattern's own text does not satisfy
// its constraint ("/admin/:id:int" is not a numeric id), so the
// resolve-the-pattern idiom silently drops constrained routes.
func (r *Router) ScreenByPattern(pattern string) (*Screen, bool) {
	pattern = normalizeRoutePath(pattern)
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

// isParamSeg reports whether a normalized route segment is dynamic
// (":name", ":name*", or a constrained ":name:int").
func isParamSeg(seg string) bool { return strings.HasPrefix(seg, ":") }

// isCatchAllSeg reports whether a segment is the trailing catch-all
// form ":name*".
func isCatchAllSeg(seg string) bool {
	return strings.HasPrefix(seg, ":") && strings.HasSuffix(seg, "*")
}

// segParamName extracts the bare parameter name from a dynamic segment,
// stripping the leading ":", any trailing catch-all "*", and a
// ":constraint" suffix. ":id" → "id", ":path*" → "path", ":id:int" → "id".
func segParamName(seg string) string {
	name := seg[1:] // strip leading ":"
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i] // strip constraint suffix
	}
	return strings.TrimSuffix(name, "*")
}

// ParamName extracts the bare parameter name from a normalized dynamic
// route segment (":id", ":path*", ":id:int"). It is the single source
// of truth for what counts as a param name, shared by the router, the
// static builder, and the sitemap expander so catch-all ("*") and
// constraint (":type") suffixes are stripped consistently everywhere a
// StaticPaths param map is substituted into a pattern.
func ParamName(seg string) string { return segParamName(seg) }

// RenderRaw renders a screen by path with no policy resolution and no
// request context. INTENDED FOR INTERNAL/SSG USE ONLY — callers in
// HTTP-serving code should use App.RenderPageResult, which evaluates
// the Policy chain before invoking Load and Render.
func (r *Router) RenderRaw(path string) (render.HTML, error) {
	screen, params, ok := r.Resolve(path)
	if !ok {
		return "", fmt.Errorf("app: no screen registered for path %q", path)
	}

	// Per-request fresh instance — SetParams/Render mutations stay
	// isolated to this call. See Screen.newInstance for the rationale.
	comp := screen.newInstance()
	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}

	content := renderComponentInScreen(context.Background(), screen, comp)

	// Group-registered screens compose ALL parent group layouts so
	// nested screen groups produce nested layout shells, each wrapped
	// in its data-fui-screen-group marker. This is what makes
	// sibling-screen nav DOM-stable at the innermost matching group.
	// When screen.Layout was explicitly set via group.Screen(s, custom),
	// it replaces the innermost group's layout in the chain — the
	// marker is still emitted so sibling-nav keeps working.
	if screen.group != nil {
		return composeLayoutsWithOverride(screen.group, screen.Layout, content, false), nil
	}

	layout := screen.Layout
	if layout == nil {
		layout = r.defaultLayout
	}
	return layout.Wrap(content), nil
}

// Paths returns all registered paths (exact + dynamic patterns).
func (r *Router) Paths() []string {
	paths := make([]string, 0, len(r.screens)+len(r.dynamic))
	for p := range r.screens {
		paths = append(paths, p)
	}
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
// Each screen in the group (and its sub-groups) is registered with the
// router, and the group's layout is applied.
//
// When the runtime navigates between siblings in the same group, only
// the inner content is swapped — the layout shell is DOM-stable.
func (r *Router) ScreenGroup(sg *ScreenGroup) {
	for _, screen := range sg.AllScreens() {
		r.Screen(screen, screen.Layout)
	}
}
