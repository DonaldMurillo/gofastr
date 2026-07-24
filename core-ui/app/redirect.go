package app

import "strings"

// patternRedirect is a dynamic redirect (RedirectPattern): a from-pattern
// whose params are passed through to a to-pattern. segments are the
// normalized from-pattern segments (with ":param" / ":param*"); to is the
// normalized target pattern.
type patternRedirect struct {
	segments []string
	to       string
}

// Redirect registers an exact-path permanent (308) redirect: a hard GET
// of `from` is redirected to `to`, and the client-side router rewrites a
// navigation to `from` without a round-trip. `to` must be a relative
// same-app path (no scheme/host) — an absolute target panics at
// registration (open-redirect guard). Registering a redirect whose
// `from` collides with an existing screen (or redirect) panics.
func (a *App) Redirect(from, to string) {
	validateRedirectTarget(to)
	from = normalizeRoutePath(from)
	if strings.Contains(from, ":") {
		panic("app: Redirect from " + from + " is dynamic — use RedirectPattern for param passthrough")
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

// RedirectPattern registers a dynamic redirect with param passthrough:
// /old/{id} → /new/{id}. Every param referenced in `to` must be declared
// in `from` — otherwise registration panics. Catch-all passthrough is
// allowed when the catch-all segment appears in both. As with Redirect,
// `to` must be relative and `from` must not collide with a screen or
// another redirect.
func (a *App) RedirectPattern(from, to string) {
	validateRedirectTarget(to)
	from = normalizeRoutePath(from)
	to = normalizeRoutePath(to)
	fromSegs := strings.Split(strings.Trim(from, "/"), "/")
	// The from-pattern obeys the exact same grammar as a screen route:
	// allowlisted constraints, catch-all final, no constrained catch-all.
	// Without this, a typo'd constraint (":string", ":integer") would be
	// silently treated as unconstrained at resolve time.
	validateDynamicSegments(from, fromSegs)
	fromParams := make(map[string]bool)
	for _, seg := range fromSegs {
		if isParamSeg(seg) {
			fromParams[segParamName(seg)] = true
		}
	}
	// Every param in `to` must be declared in `from`.
	for _, seg := range strings.Split(strings.Trim(to, "/"), "/") {
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
	for _, dr := range a.Router.dynamic {
		if patternsOverlap(dr.segments, fromSegs) {
			panic("app: redirect from " + from + " overlaps registered screen " + dr.screen.Path +
				" — redirects are consulted before screens, so the overlap would silently shadow it")
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

// ResolveRedirect returns the redirect target for `path` (exact or
// pattern, with param passthrough substituted) and whether `path` is a
// registered redirect. Chained redirects (A→B where B is itself a
// redirect) are followed to the final target so callers emit a single
// hop; a chain that hasn't terminated after 10 hops (a cycle, since
// registration is finite) fails closed and reports no redirect.
//
// Collision checks at registration keep same-shape duplicates out
// (exact-vs-exact, pattern-vs-pattern, and redirect-vs-screen of the
// same shape). Cross-shape shadowing follows the router's exact-first
// contract: an exact redirect may shadow one concrete path of a dynamic
// screen, exactly as an exact screen registration would.
func (a *App) ResolveRedirect(path string) (string, bool) {
	return a.Router.resolveRedirect(path)
}

func (r *Router) resolveRedirect(path string) (string, bool) {
	target, ok := r.resolveRedirectOnce(path)
	if !ok {
		return "", false
	}
	// Follow chains to the final target so the response is one hop.
	// Registration cannot rule out cycles (A→B and B→A are individually
	// valid), so cap the walk and fail closed when it doesn't terminate.
	// The initial resolve above consumed hop 1; ten more iterations let a
	// chain of up to 10 edges reach AND verify its terminal target.
	for i := 0; i < 10; i++ {
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
		if params, ok := matchDynamic(pr.segments, pathParts); ok {
			t := substituteRedirect(pr.to, params)
			// The registered target pattern was validated, but the
			// substituted VALUES come from the request path: a catch-all
			// passthrough can join an empty segment into a "//host" or
			// backslash-bearing target that browsers normalize into an
			// absolute URL. Fail closed — no redirect beats an open one.
			if !safeResolvedTarget(t) {
				return "", false
			}
			return t, true
		}
	}
	return "", false
}

// safeResolvedTarget re-checks a substituted redirect target with the
// same rules validateRedirectTarget applies to the registered pattern.
// Backslashes are rejected outright: browsers normalize "/\host" (and
// "\/host") to the protocol-relative "//host".
func safeResolvedTarget(t string) bool {
	return strings.HasPrefix(t, "/") &&
		!strings.HasPrefix(t, "//") &&
		!strings.Contains(t, "\\") &&
		!strings.Contains(t, "://")
}

// substituteRedirect fills the target pattern's params from the matched
// route params. Params present in the pattern but absent from the map are
// left as-is (the from/to validation already guarantees every target
// param was declared in from, so this only happens for malformed input).
func substituteRedirect(toPattern string, params map[string]string) string {
	parts := strings.Split(toPattern, "/")
	for i, seg := range parts {
		if !isParamSeg(seg) {
			continue
		}
		if v, ok := params[segParamName(seg)]; ok {
			parts[i] = v
		}
	}
	return strings.Join(parts, "/")
}

// validateRedirectTarget rejects absolute redirect targets (scheme or
// host), which would turn a same-app redirect into an open redirect.
func validateRedirectTarget(to string) {
	if !strings.HasPrefix(to, "/") {
		panic("app: redirect target must be a relative same-app path (start with /): " + to)
	}
	if strings.HasPrefix(to, "//") || strings.Contains(to, "://") {
		panic("app: redirect target must not be an absolute URL (open-redirect guard): " + to)
	}
	// Browsers normalize backslashes to forward slashes when resolving a
	// Location, so "/\evil.com" is "//evil.com" in disguise.
	if strings.Contains(to, "\\") {
		panic("app: redirect target must not contain a backslash (open-redirect guard): " + to)
	}
}

// patternRedirectExists reports whether a redirect with the given from-
// segments is already registered (shape-equality, not specificity —
// consistent with the no-specificity-ranking routing contract).
func patternRedirectExists(redirs []patternRedirect, segs []string) bool {
	for _, pr := range redirs {
		if sameShape(pr.segments, segs) {
			return true
		}
	}
	return false
}

// sameShape reports whether two normalized segment lists match the same
// set of URLs. Param NAMES are irrelevant to what a pattern matches —
// "/users/:id" and "/users/:slug" are the same shape — so duplicate
// checks compare positions: literal segments byte-equal, param segments
// equivalent when both are plain, both are the same constraint, or both
// are catch-alls. Used for redirect-vs-redirect duplicates only:
// overlapping-but-unequal redirects follow registration order, exactly
// like screens do.
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
		if segConstraint(a[i]) != segConstraint(b[i]) {
			return false
		}
	}
	return true
}

// patternsOverlap reports whether any URL matches BOTH normalized
// patterns. Redirect-vs-screen collisions use overlap (not shape
// equality): redirects are consulted before screen resolution, so ANY
// shared URL would let the redirect silently shadow the screen with no
// registration-order recourse — "/users/{n:int}" over "/users/{id}"
// steals every numeric id. Catch-alls absorb one-or-more trailing
// segments, so a trailing catch-all overlaps anything that agrees on
// the prefix and has at least one segment beyond it.
func patternsOverlap(a, b []string) bool {
	catchA := len(a) > 0 && isCatchAllSeg(a[len(a)-1])
	catchB := len(b) > 0 && isCatchAllSeg(b[len(b)-1])

	// Fixed-length portions that must pairwise-overlap.
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

	n := prefixA
	if prefixB < n {
		n = prefixB
	}
	for i := 0; i < n; i++ {
		if !segsOverlap(a[i], b[i]) {
			return false
		}
	}
	// Positions beyond the shorter prefix are consumed by the other
	// pattern's catch-all (which matches anything) — no further checks.
	// For the no-catch-all case n == len(a) == len(b), so every position
	// was compared.
	if !catchA && !catchB {
		return true
	}
	// Compare the remaining fixed segments of the longer pattern against
	// "anything" (the catch-all) — always overlapping.
	return true
}

// segsOverlap reports whether two single segments can match the same
// value: literal-vs-literal by equality, literal-vs-param by the param's
// constraint accepting the literal, param-vs-param by constraint-set
// intersection (unconstrained overlaps everything; int⊂alnum, alpha⊂alnum;
// uuid is disjoint from int/alpha/alnum because it requires hyphens).
func segsOverlap(x, y string) bool {
	px, py := isParamSeg(x), isParamSeg(y)
	switch {
	case !px && !py:
		return x == y
	case px && !py:
		return constraintOK(x, y)
	case !px && py:
		return constraintOK(y, x)
	}
	cx, cy := segConstraint(x), segConstraint(y)
	if cx == "" || cy == "" || cx == cy {
		return true
	}
	disjoint := map[[2]string]bool{
		{"int", "alpha"}: true, {"alpha", "int"}: true,
		{"uuid", "int"}: true, {"int", "uuid"}: true,
		{"uuid", "alpha"}: true, {"alpha", "uuid"}: true,
		{"uuid", "alnum"}: true, {"alnum", "uuid"}: true,
	}
	return !disjoint[[2]string{cx, cy}]
}
