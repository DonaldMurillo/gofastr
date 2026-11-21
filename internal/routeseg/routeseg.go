// Package routeseg holds the app router's segment constraints (":id:int",
// ":id:uuid", ":slug:alpha", ":code:alnum"): what a constrained segment
// admits. core-ui/app's router enforces them, and framework/embed reads them
// to keep a grant's reach to the paths the surface's route matches, without
// importing the UI layer.
package routeseg

import (
	"slices"
	"strings"
)

// constraints is every constraint the router enforces, in the order a
// registration error lists them: its matcher and the constraints no
// value satisfies together with it. There is deliberately no "string"
// constraint: every param is a string, so it would be an unconstrained
// no-op under a misleading name.
var constraints = []struct {
	name     string
	match    func(string) bool
	disjoint []string
}{
	{"int", isAllDigits, []string{"alpha", "uuid"}},
	{"uuid", isUUID, []string{"int", "alpha", "alnum"}}, // uuid needs hyphens
	{"alpha", isAllAlpha, []string{"int", "uuid"}},
	{"alnum", isAllAlnum, []string{"uuid"}},
}

func lookup(name string) (int, bool) {
	for i, c := range constraints {
		if c.name == name {
			return i, true
		}
	}
	return 0, false
}

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
// constraint declared on seg. An absent or unknown constraint passes;
// unknown names are refused at registration (Valid), so resolve never
// sees them.
func ConstraintOK(seg, val string) bool {
	i, ok := lookup(Constraint(seg))
	return !ok || constraints[i].match(val)
}

// Valid reports whether name is a constraint the router enforces: the
// registration-time allowlist.
func Valid(name string) bool {
	_, ok := lookup(name)
	return ok
}

// Names lists the constraints the router enforces.
func Names() []string {
	out := make([]string, len(constraints))
	for i, c := range constraints {
		out[i] = c.name
	}
	return out
}

// Overlap reports whether one value can satisfy both constraints. An
// empty name is unconstrained and overlaps every constraint.
func Overlap(a, b string) bool {
	if a == "" || b == "" || a == b {
		return true
	}
	i, okA := lookup(a)
	j, okB := lookup(b)
	if !okA || !okB {
		return true
	}
	return !slices.Contains(constraints[i].disjoint, b) && !slices.Contains(constraints[j].disjoint, a)
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
