package access

import "strings"

// ScopeMatch reports whether a granted scope satisfies required under the
// resource:verb grammar ("posts:*", "*:read", "*:*"). It does not expand
// against the capability registry. Empty grants or a malformed required deny.
func ScopeMatch(granted []Permission, required Permission) bool {
	wantRes, wantVerb, ok := splitScope(string(required))
	if !ok {
		return false
	}
	for _, g := range granted {
		if g == required {
			return true
		}
		res, verb, ok := splitScope(string(g))
		if !ok {
			continue
		}
		if (res == "*" || res == wantRes) && (verb == "*" || verb == wantVerb) {
			return true
		}
	}
	return false
}

// ValidScope reports whether s is "resource:verb" with each half one or more
// of [a-z0-9_*-] and exactly one ':'.
func ValidScope(s string) bool {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return false
	}
	for i := range s {
		c := s[i]
		if c == ':' {
			if i != idx {
				return false
			}
			continue
		}
		if !isScopeChar(c) {
			return false
		}
	}
	return true
}

func isScopeChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '*' || c == '-'
}

func splitScope(s string) (resource, verb string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}
