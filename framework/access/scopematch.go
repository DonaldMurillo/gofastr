package access

import "strings"

// ScopeMatch reports whether any granted permission satisfies the required
// permission using the resource:verb wildcard grammar shared by token
// scopes and module capability grants:
//
//   - exact match: "posts:read" grants "posts:read"
//   - resource wildcard: "posts:*" grants any "posts:<verb>"
//   - verb wildcard: "*:read" grants "<any-resource>:read"
//   - grant-all: "*:*" grants everything
//
// Empty grants deny everything (secure-by-default), and a required value
// that does not parse as "resource:verb" is never satisfied.
//
// ScopeMatch is PURE: it is a function of its two arguments only. It does
// NOT consult the capability registry and does NOT expand resource
// wildcards the way RolePolicy.Grant does at grant time — "teams:*" matches
// literally here, against whatever the caller passes as required. Grant-time
// expansion (teams:* → teams:read, teams:write, …) is a separate concern
// that lives in RolePolicy; matching and expanding are deliberately not
// entangled.
//
// This is the matcher the module-grant side and battery/auth's token scopes
// delegate to, so the resource:verb algebra has exactly one home. access.Can
// (the RBAC hot path) is untouched: it performs exact-string-or-global-"*"
// matching and must stay that way — widening it would silently change live
// RBAC for every caller.
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

// ValidScope reports whether s is a well-formed "resource:verb" scope under
// the same closed vocabulary ScopeMatch and the token issuer use: each half
// is one or more of [a-z0-9_*-], separated by exactly one ':'. Both halves
// may be "*" so the wildcard scopes ScopeMatch documents ("posts:*",
// "*:read", "*:*") are mintable. Use this at install/mint time to reject
// malformed scope strings before they reach the matcher.
func ValidScope(s string) bool {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return false // no colon, or empty resource/verb half
	}
	for i := range s {
		c := s[i]
		if c == ':' {
			if i != idx {
				return false // a second colon
			}
			continue
		}
		if !isScopeChar(c) {
			return false
		}
	}
	return true
}

// isScopeChar reports whether c is in the scope-vocabulary class [a-z0-9_*-].
func isScopeChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '*' || c == '-'
}

// splitScope parses "resource:verb" at the first colon. ok is false when
// there is no colon or either half is empty. A value with multiple colons
// parses on the first colon (resource = head, verb = the rest) — that is
// fine for matching, but ValidScope rejects multi-colon strings at mint
// time so they never reach a granted set in practice.
func splitScope(s string) (resource, verb string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}
