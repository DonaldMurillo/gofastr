// Package stability classifies every package in the module into a support
// tier and enforces, via [TestEveryPackageIsClassified], that no package
// ships without an explicit classification.
//
// The tiers below are the machine-readable half of the policy documented in
// framework/docs/content/stability.md and docs/public-api.md. The human docs
// explain what each tier promises; this file is the source of truth for which
// package is in which tier, and the test package is the gate that keeps the
// two in sync.
//
// Before v1.0.0 the supported library surface is Provisional, not Stable:
// it is documented and follows the deprecation window in stability.md, but it
// is not frozen. Promoting a package to Stable is the deliberate v1 act — it
// is not done implicitly by omission.
package stability

import "strings"

// ModulePath is the module's import path. Package paths in the manifest are
// written relative to it (leading slash dropped), e.g. "framework/crud".
const ModulePath = "github.com/DonaldMurillo/gofastr"

// Tier is a package's support classification.
type Tier string

const (
	// Stable: frozen public API. After v1.0.0 a breaking change requires a
	// new major version. No package is Stable before v1.0.0 — the freeze is
	// an explicit release act, never a default.
	Stable Tier = "stable"

	// Provisional: supported and documented, but may change before v1.0.0
	// following the deprecation window in stability.md. This is the honest
	// pre-v1 tier for the framework, core, core-ui, and battery surface.
	Provisional Tier = "provisional"

	// Experimental: may change or be removed without a deprecation window.
	// Consumers should pin a version. Covers framework/experimental/*, the
	// Kiln build-mode runtime, and the codegen extension exec surface.
	Experimental Tier = "experimental"

	// Internal: not a public contract even though the symbols are exported.
	// Import at your own risk; these move freely.
	Internal Tier = "internal"

	// Excluded: not part of the shipped library surface at all — examples,
	// benchmarks, evals, and build output. Present so the coverage gate can
	// account for every package rather than silently skipping some.
	Excluded Tier = "excluded"
)

// rule maps a package-path prefix (relative to ModulePath) to a tier. The
// longest matching prefix wins, so a specific subtree can override a broad
// default. The empty prefix "" is the module root default and MUST be last-
// resort only; every real package should match a more specific rule so that a
// newly added top-level tree fails the gate until it is classified on purpose.
type rule struct {
	prefix string
	tier   Tier
}

// manifest is ordered by specificity for readability only; Classify sorts by
// prefix length at match time, so order here is not load-bearing.
var manifest = []rule{
	// --- Not part of the library surface ---
	{"examples/", Excluded},
	{"benchmarks/", Excluded},
	{"evals/", Excluded},
	{"dist/", Excluded},

	// --- Internal ---
	{"internal/", Internal},
	{"stability", Internal}, // this gate package is not a public contract

	// --- Experimental: pin a version ---
	{"framework/experimental/", Experimental},
	{"kiln", Experimental},
	{"codegen", Experimental},

	// --- Provisional: supported, not yet frozen ---
	{"framework", Provisional},
	{"core-ui", Provisional},
	{"core", Provisional},
	{"battery", Provisional},
	{"sqlite", Provisional},
	{"cmd", Provisional},
}

// Classify returns the tier for a full import path (e.g.
// "github.com/DonaldMurillo/gofastr/framework/crud"). ok is false when no rule
// matches — that is the gate's signal that the package must be added to the
// manifest before it can ship.
func Classify(importPath string) (Tier, bool) {
	rel := strings.TrimPrefix(importPath, ModulePath+"/")
	rel = strings.TrimPrefix(rel, ModulePath) // module root itself

	// A nested /internal/ anywhere in the path is always Internal, regardless
	// of which tree it lives under — Go's own visibility rule.
	if rel == "internal" || strings.HasPrefix(rel, "internal/") ||
		strings.Contains(rel, "/internal/") || strings.HasSuffix(rel, "/internal") {
		return Internal, true
	}

	best := ""
	var bestTier Tier
	found := false
	for _, r := range manifest {
		if r.prefix == "" {
			continue
		}
		if rel == strings.TrimSuffix(r.prefix, "/") || strings.HasPrefix(rel, r.prefix) {
			if !found || len(r.prefix) > len(best) {
				best, bestTier, found = r.prefix, r.tier, true
			}
		}
	}
	return bestTier, found
}
