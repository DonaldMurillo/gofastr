package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// Capability is the area of the framework a rule speaks about. It is the
// unit users filter by (`gofastr verify routing`) and the unit config
// relaxes by, so the set is deliberately small and stable — a new
// capability is an API change, a new rule inside one is not.
type Capability string

const (
	// CapMeta covers the contract system talking about itself:
	// unparsable config, suppressions that no longer match anything.
	CapMeta Capability = "meta"
	// CapRouting covers the route table — duplicates, auth, reachability,
	// and registrations that bypass the framework's own helpers.
	CapRouting Capability = "routing"
	// CapTesting covers semantic coverage: which routes, permissions, and
	// entity operations a test run actually exercised.
	CapTesting Capability = "testing"
	// CapAccessibility covers the static WCAG floor the type system can
	// see. The runtime half lives in `gofastr audit a11y --url`.
	CapAccessibility Capability = "accessibility"
	// CapArchitecture covers dependency direction and package layering.
	CapArchitecture Capability = "architecture"
	// CapSecurity covers CSRF, injection, cookie flags, and unscoped data.
	CapSecurity Capability = "security"
	// CapPerformance covers work done per-request that belongs at init.
	CapPerformance Capability = "performance"
	// CapData covers the persistence layer — ignored writes, raw SQL.
	CapData Capability = "data"
	// CapEntities covers entity declarations and their exposure surface.
	CapEntities Capability = "entities"
	// CapPermissions covers who can reach what: owner scoping, RBAC.
	CapPermissions Capability = "permissions"
	// CapRendering covers the UI contract — one styling surface, no hard
	// navigation, no bespoke event streams.
	CapRendering Capability = "rendering"
	// CapAI covers idiomatic-usage guidance: the hand-rolled shape an
	// agent reaches for when a framework primitive already exists.
	CapAI Capability = "ai"
)

// allCapabilities is the canonical order capabilities are reported in —
// roughly "how early does getting this wrong hurt", not alphabetical.
var allCapabilities = []Capability{
	CapMeta,
	CapRouting,
	CapPermissions,
	CapSecurity,
	CapData,
	CapEntities,
	CapArchitecture,
	CapRendering,
	CapAccessibility,
	CapPerformance,
	CapTesting,
	CapAI,
}

// Capabilities returns every capability in report order.
func Capabilities() []Capability {
	out := make([]Capability, len(allCapabilities))
	copy(out, allCapabilities)
	return out
}

// Order is the capability's position in report order. Unknown
// capabilities sort last so a future rule never silently jumps the queue.
func (c Capability) Order() int {
	for i, known := range allCapabilities {
		if known == c {
			return i
		}
	}
	return len(allCapabilities)
}

// Valid reports whether c is a capability the catalog knows.
func (c Capability) Valid() bool { return c.Order() < len(allCapabilities) }

func (c Capability) String() string { return string(c) }

// Title is the capability rendered for a section header.
func (c Capability) Title() string {
	switch c {
	case CapAI:
		return "AI guidance"
	case CapAccessibility:
		return "Accessibility"
	default:
		s := string(c)
		if s == "" {
			return ""
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// ParseCapability resolves a user-typed capability name. It accepts the
// canonical name plus the aliases people actually type — `a11y` for
// accessibility, `sec` for security, `perf` for performance — because a
// CLI that rejects `gofastr verify a11y` after shipping `gofastr audit
// a11y` for a year is just being rude.
func ParseCapability(s string) (Capability, error) {
	norm := strings.ToLower(strings.TrimSpace(s))
	switch norm {
	case "a11y", "accessibility":
		return CapAccessibility, nil
	case "sec", "security":
		return CapSecurity, nil
	case "perf", "performance":
		return CapPerformance, nil
	case "arch", "architecture":
		return CapArchitecture, nil
	case "test", "tests", "testing", "coverage":
		return CapTesting, nil
	case "perms", "permissions", "authz":
		return CapPermissions, nil
	case "entity", "entities":
		return CapEntities, nil
	case "route", "routes", "routing":
		return CapRouting, nil
	case "render", "rendering", "ui":
		return CapRendering, nil
	case "data", "db":
		return CapData, nil
	case "ai", "guidance", "idiomatic":
		return CapAI, nil
	case "meta":
		return CapMeta, nil
	}
	names := make([]string, 0, len(allCapabilities))
	for _, c := range allCapabilities {
		names = append(names, string(c))
	}
	sort.Strings(names)
	return "", fmt.Errorf("unknown capability %q — known capabilities: %s", s, strings.Join(names, ", "))
}
