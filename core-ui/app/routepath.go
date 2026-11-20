package app

import (
	"net/url"
	"strings"
)

// RoutePath decodes an escaped request path into the form Router.Resolve matches, keeping %2F and %25.
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

// EscapeRoutePath is the inverse of [RoutePath]: the escaped URL path for a routing-form path, as
// url.URL.EscapedPath writes it, with a "/" inside a segment written %2F.
func EscapeRoutePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		e := (&url.URL{Path: decodeRouteSeg(s)}).EscapedPath()
		segs[i] = strings.ReplaceAll(e, "/", "%2F")
	}
	return strings.Join(segs, "/")
}

// PatternPath substitutes params into a route pattern and returns the routing-form path: each
// ":name" segment takes params[name] (see [ParamName]), a param params lacks stays as written.
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

func encodeRouteSeg(s string) string { return routeSegEncoder.Replace(s) }

func decodeRouteSeg(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	return routeSegDecoder.Replace(s)
}
