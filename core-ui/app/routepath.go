package app

import (
	"net/url"
	"strings"
)

// RoutePath turns an escaped request path (url.URL.EscapedPath, or a
// client's location.pathname) into the routing form [Router.Resolve]
// matches: every segment percent-decoded, except that a "/" or "%"
// inside a segment stays escaped as %2F or %25. A record whose key holds
// "/" is linked as "/orders/a%2Fb"; the decoded path, "/orders/a/b",
// has lost the segment boundary, and the routing form keeps it.
//
// A path with neither an escaped "/" nor a "%" in its decoded text comes
// back as its decoded path, so a host that hands Resolve r.URL.Path
// routes it the same way. A segment that is not valid percent-encoding
// is taken literally.
func RoutePath(escaped string) string {
	if !strings.Contains(escaped, "%") {
		return escaped
	}
	segs := strings.Split(escaped, "/")
	for i, s := range segs {
		if !strings.Contains(s, "%") {
			continue
		}
		if d, err := url.PathUnescape(s); err == nil {
			segs[i] = encodeRouteSeg(d)
		} else {
			segs[i] = encodeRouteSeg(s)
		}
	}
	return strings.Join(segs, "/")
}

// EscapeRoutePath is the inverse of [RoutePath]: the escaped URL path
// for a routing-form path, as url.URL.EscapedPath writes it, with a "/"
// inside a segment written %2F. A decoded path without "%" escapes as
// EscapedPath escapes it.
func EscapeRoutePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		e := (&url.URL{Path: decodeRouteSeg(s)}).EscapedPath()
		segs[i] = strings.ReplaceAll(e, "/", "%2F")
	}
	return strings.Join(segs, "/")
}

// PatternPath substitutes params into a route pattern and returns the
// routing-form path: each ":name" segment takes params[name] (see
// [ParamName]), a param params lacks stays as written. A value fills
// one segment, its "/" and "%" kept as %2F and %25, so "a%2Fb" stays
// one key rather than reaching the router as "a/b". A catch-all
// (":name*") value's "/" separates segments. The static export and the
// sitemap both expand StaticPaths through it, so a page and its <loc>
// name the same URL.
func PatternPath(pattern string, params map[string]string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if !strings.HasPrefix(part, ":") || len(part) <= 1 {
			continue
		}
		v, ok := params[ParamName(part)]
		if !ok {
			continue
		}
		if !strings.HasSuffix(part, "*") {
			parts[i] = RoutePath(url.PathEscape(v))
			continue
		}
		comps := strings.Split(v, "/")
		for j, c := range comps {
			comps[j] = RoutePath(url.PathEscape(c))
		}
		parts[i] = strings.Join(comps, "/")
	}
	return strings.Join(parts, "/")
}

var (
	routeSegEncoder = strings.NewReplacer("%", "%25", "/", "%2F")
	routeSegDecoder = strings.NewReplacer("%25", "%", "%2F", "/")
)

// encodeRouteSeg writes one decoded segment in routing form.
func encodeRouteSeg(s string) string { return routeSegEncoder.Replace(s) }

// decodeRouteSeg reads a routing-form segment, or a whole routing-form
// path, back to its decoded text.
func decodeRouteSeg(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	return routeSegDecoder.Replace(s)
}
