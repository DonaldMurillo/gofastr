package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/routeseg"
)

type patternRedirect struct {
	segments []string
	to       string
}

// Redirect registers an exact-path permanent (308) redirect: a hard GET of `from` is redirected to
// `to`, and the client-side router rewrites a navigation to `from` without a round-trip.
func (a *App) Redirect(from, to string) {
	validateRedirectTarget(to)
	from = normalizeRoutePath(from)
	if strings.Contains(from, ":") {
		panic("app: Redirect from " + from + " is dynamic. Use RedirectPattern for param passthrough")
	}
	if _, ok := a.Router.screens[from]; ok {
		panic("app: redirect from " + from + " collides with a registered screen")
	}
	if _, ok := a.Router.exactRedir[from]; ok {
		panic("app: duplicate redirect from " + from)
	}
	if a.Router.exactRedir == nil {
		a.Router.exactRedir = map[string]string{}
	}
	a.Router.exactRedir[from] = to
}

// RedirectPattern registers a dynamic redirect with param passthrough: /old/{id} → /new/{id}.
func (a *App) RedirectPattern(from, to string) {
	validateRedirectTarget(to)
	from = normalizeRoutePath(from)
	to = normalizeRoutePath(to)
	fromSegs := strings.Split(strings.Trim(from, "/"), "/")
	validateDynamicSegments(from, fromSegs)
	fromParams := make(map[string]bool)
	for _, seg := range fromSegs {
		if isParamSeg(seg) {
			fromParams[segParamName(seg)] = true
		}
	}
	for seg := range strings.SplitSeq(strings.Trim(to, "/"), "/") {
		if isParamSeg(seg) {
			name := segParamName(seg)
			if !fromParams[name] {
				panic("app: redirect target " + to + " references param :" + name +
					" not declared in from " + from)
			}
		}
	}
	if _, ok := a.Router.screens[from]; ok {
		panic("app: redirect from " + from + " collides with a registered screen")
	}
	var shadowed []string
	for path := range a.Router.screens {
		if patternsOverlap(fromSegs, strings.Split(strings.Trim(path, "/"), "/")) {
			shadowed = append(shadowed, path)
		}
	}
	if len(shadowed) > 0 {
		sort.Strings(shadowed)
		panic("app: redirect from " + from + " overlaps registered screen " + shadowed[0] +
			". Redirects are consulted before screens, so the overlap would silently shadow it")
	}
	for _, dr := range a.Router.dynamic {
		if patternsOverlap(dr.segments, fromSegs) {
			panic("app: redirect from " + from + " overlaps registered screen " + dr.screen.Path +
				". Redirects are consulted before screens, so the overlap would silently shadow it")
		}
	}
	if patternRedirectExists(a.Router.patternRedir, fromSegs) {
		panic("app: duplicate redirect from " + from)
	}
	a.Router.patternRedir = append(a.Router.patternRedir, patternRedirect{
		segments: fromSegs,
		to:       to,
	})
}

// ResolveRedirect returns the redirect target for `path` (exact or pattern, with param passthrough
// substituted) and whether `path` is a registered redirect.
func (a *App) ResolveRedirect(path string) (string, bool) {
	return a.Router.resolveRedirect(path)
}

func (r *Router) resolveRedirect(path string) (string, bool) {
	target, ok := r.resolveRedirectOnce(path)
	if !ok {
		return "", false
	}
	for range 10 {
		next, ok := r.resolveRedirectOnce(target)
		if !ok {
			return target, true
		}
		target = next
	}
	return "", false
}

func (r *Router) resolveRedirectOnce(path string) (string, bool) {
	if to, ok := r.exactRedir[path]; ok {
		return to, true
	}
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	for _, pr := range r.patternRedir {
		if params, ok := matchDynamicRouting(pr.segments, pathParts); ok {
			t := substituteRedirect(pr.to, params)
			if !safeResolvedTarget(t) {
				return "", false
			}
			return t, true
		}
	}
	return "", false
}

func safeResolvedTarget(t string) bool {
	return strings.HasPrefix(t, "/") &&
		!strings.HasPrefix(t, "//") &&
		!strings.Contains(t, "\\") &&
		!strings.Contains(t, "://") &&
		!hasControlByte(t)
}

func hasControlByte(s string) bool {
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

var redirectQueryEscaper = strings.NewReplacer("?", "%3F", "#", "%23")

func substituteRedirect(toPattern string, params map[string]string) string {
	parts := strings.Split(toPattern, "/")
	for i, seg := range parts {
		if !isParamSeg(seg) {
			continue
		}
		if v, ok := params[segParamName(seg)]; ok {
			if isCatchAllSeg(seg) {
				v = redirectQueryEscaper.Replace(v)
			} else {
				v = redirectQueryEscaper.Replace(encodeRouteSeg(decodeRouteSeg(v)))
			}
			parts[i] = v
		}
	}
	return strings.Join(parts, "/")
}

func validateRedirectTarget(to string) {
	if !strings.HasPrefix(to, "/") {
		panic("app: redirect target must be a relative same-app path (start with /): " + to)
	}
	if strings.HasPrefix(to, "//") || strings.Contains(to, "://") {
		panic("app: redirect target must not be an absolute URL (open-redirect guard): " + to)
	}
	if strings.Contains(to, "\\") {
		panic("app: redirect target must not contain a backslash (open-redirect guard): " + to)
	}
	if hasControlByte(to) {
		panic("app: redirect target must not contain a control byte (open-redirect guard): " + strconv.Quote(to))
	}
}

func patternRedirectExists(redirs []patternRedirect, segs []string) bool {
	for _, pr := range redirs {
		if sameShape(pr.segments, segs) {
			return true
		}
	}
	return false
}

func sameShape(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		pa, pb := isParamSeg(a[i]), isParamSeg(b[i])
		if pa != pb {
			return false
		}
		if !pa {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		if isCatchAllSeg(a[i]) != isCatchAllSeg(b[i]) {
			return false
		}
		if routeseg.Constraint(a[i]) != routeseg.Constraint(b[i]) {
			return false
		}
	}
	return true
}

func patternsOverlap(a, b []string) bool {
	catchA := len(a) > 0 && isCatchAllSeg(a[len(a)-1])
	catchB := len(b) > 0 && isCatchAllSeg(b[len(b)-1])

	prefixA, prefixB := len(a), len(b)
	if catchA {
		prefixA--
	}
	if catchB {
		prefixB--
	}
	switch {
	case !catchA && !catchB:
		if len(a) != len(b) {
			return false
		}
	case catchA && !catchB:
		if len(b) < prefixA+1 { // catch-all needs >=1 remainder segment
			return false
		}
	case !catchA && catchB:
		if len(a) < prefixB+1 {
			return false
		}
	default: // both catch-alls: always length-compatible
	}

	n := min(prefixB, prefixA)
	for i := range n {
		if !segsOverlap(a[i], b[i]) {
			return false
		}
	}
	if !catchA && !catchB {
		return true
	}
	return true
}

func segsOverlap(x, y string) bool {
	px, py := isParamSeg(x), isParamSeg(y)
	switch {
	case !px && !py:
		return x == y
	case px && !py:
		return routeseg.ConstraintOK(x, y)
	case !px && py:
		return routeseg.ConstraintOK(y, x)
	}
	return routeseg.Overlap(routeseg.Constraint(x), routeseg.Constraint(y))
}
