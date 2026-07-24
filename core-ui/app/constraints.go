package app

import "strings"

// segConstraint returns the constraint name declared on a dynamic segment
// ("" when none). ":id:int" → "int"; ":id" and ":path*" → "".
func segConstraint(seg string) string {
	if !strings.HasPrefix(seg, ":") {
		return ""
	}
	rest := seg[1:] // drop leading ":"
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[i+1:]
	}
	return ""
}

// constraintOK reports whether val satisfies the constraint declared on seg.
// Unknown / absent constraints pass (the value matches anything); only the
// names in validConstraints are enforced. Unknown constraint names are
// rejected at registration, so resolve never sees them.
func constraintOK(seg, val string) bool {
	switch segConstraint(seg) {
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

// validConstraints is the registration-time allowlist. There is
// deliberately no "string" constraint — every param is a string, so it
// would be an unconstrained no-op under a misleading name.
var validConstraints = map[string]bool{"int": true, "uuid": true, "alpha": true, "alnum": true}

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
// constraint). An empty value never matches — a dynamic segment must bind.
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
	for i := 0; i < 36; i++ {
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
