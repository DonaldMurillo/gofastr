package router

import (
	"fmt"
	"net/http"
	"strings"
)

// Param returns path parameter name, truncated as SanitizePathParam does; it panics when the matched route declares no such wildcard.
func Param(r *http.Request, name string) string {
	if r.Pattern != "" && !declaresWildcard(r.Pattern, name) {
		panic(fmt.Sprintf("router.Param: route %q has no {%s} wildcard", r.Pattern, name))
	}
	return SanitizePathParam(r.PathValue(name), IsCatchAllParam(r.Pattern, name))
}

func declaresWildcard(pattern, name string) bool {
	_, declared := wildcards(pattern)[name]
	return declared
}

// IsCatchAllParam reports whether pattern declares name as a {name...} wildcard.
func IsCatchAllParam(pattern, name string) bool {
	return wildcards(pattern)[name]
}

func wildcards(pattern string) map[string]bool {
	out := map[string]bool{}
	for {
		start := strings.IndexByte(pattern, '{')
		if start == -1 {
			return out
		}
		end := strings.IndexByte(pattern[start:], '}')
		if end == -1 {
			return out
		}
		name := pattern[start+1 : start+end]
		pattern = pattern[start+end+1:]
		catchAll := strings.HasSuffix(name, "...")
		if name = strings.TrimSuffix(name, "..."); name != "$" {
			out[name] = catchAll
		}
	}
}

// Params returns every wildcard the matched pattern declares, each sanitized as Param does, even when empty.
func Params(r *http.Request) map[string]string {
	pattern := r.Pattern
	if pattern == "" {
		return nil
	}
	params := make(map[string]string)
	for name, catchAll := range wildcards(pattern) {
		params[name] = SanitizePathParam(r.PathValue(name), catchAll)
	}
	return params
}

// SanitizePathParam truncates s at the first CR, LF, NUL, ".." segment, or "/" (catch-all: an empty segment) a routed path cannot carry.
func SanitizePathParam(s string, catchAll bool) string {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\n' || c == '\r' || c == 0:
			return s[:i]
		case c == '/' && !catchAll:
			return s[:i]
		case c == '/' && (i == 0 || s[i-1] == '/'):
			return s[:i]
		case c == '.' && isDotDotSegment(s, i):
			return s[:i]
		}
	}
	return s
}

func isDotDotSegment(s string, i int) bool {
	if i+2 > len(s) || s[i+1] != '.' {
		return false
	}
	if i > 0 && s[i-1] != '/' {
		return false
	}
	return i+2 == len(s) || s[i+2] == '/'
}
