// Package routeseg holds the app router's segment constraints (":id:int",
// ":id:uuid", ":slug:alpha", ":code:alnum"): what a constrained segment
// admits. core-ui/app's router enforces them, and framework/embed reads them
// to keep a grant's reach to the paths the surface's route matches, without
// importing the UI layer.
package routeseg

import "strings"

// Constraint returns the constraint name declared on a dynamic segment
// ("" when none). ":id:int" → "int"; ":id" and ":path*" → "".
func Constraint(seg string) string {
	if !strings.HasPrefix(seg, ":") {
		return ""
	}
	rest := seg[1:] // drop leading ":"
	if _, after, ok := strings.Cut(rest, ":"); ok {
		return after
	}
	return ""
}

// ConstraintOK reports whether val, a decoded path segment, satisfies the
// constraint declared on seg.
// Unknown / absent constraints pass (the value matches anything); only the
// names in validConstraints are enforced. Unknown constraint names are
// rejected at registration, so resolve never sees them.
func ConstraintOK(seg, val string) bool {
	switch Constraint(seg) {
	case "int":
		return isAllDigits(val)
	case "uuid":
		return isUUID(val)
	case "alpha":
		return isAllAlpha(val)
	case "alnum":
		return isAllAlnum(val)
	default:
		return true
	}
}

// Valid reports whether name is a constraint the router enforces: the
// registration-time allowlist. There is deliberately no "string"
// constraint, every param is a string, so it would be an unconstrained
// no-op under a misleading name.
func Valid(name string) bool {
	switch name {
	case "int", "uuid", "alpha", "alnum":
		return true
	}
	return false
}

// isAllAlpha reports whether s is one or more ASCII letters (the "alpha"
// constraint). Empty never matches.
func isAllAlpha(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// isAllAlnum reports whether s is one or more ASCII letters/digits (the
// "alnum" constraint). Empty never matches.
func isAllAlnum(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// isAllDigits reports whether s is one or more ASCII digits (the "int"
// constraint). An empty value never matches, a dynamic segment must bind.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isUUID reports whether s is a canonical UUID: 8-4-4-4-12 hex digits,
// case-insensitive, hyphen-separated (36 bytes total).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range 36 {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexDigit(c) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
