package pluginhost

import (
	"testing"
)

// Property: the boot-time Permissions-Policy check warns ONLY when every
// directive naming a feature carries the empty allowlist "()" — the one
// header shape that provably denies the feature to every context. A single
// granting directive among denials means the feature is granted, and a
// warning there would train developers to ignore the check (the documented
// contract in CheckHostRequirements). Asserted at the pure function AND
// through CheckHostRequirements' warning output, so the parser and the
// contract cannot drift apart.
func TestDenyRuleRequiresAllDirectivesEmpty(t *testing.T) {
	denied := []string{
		"camera=()",                           // the plain denial
		"camera=(), geolocation=()",           // denied feature first among others
		"geolocation=(), camera=()",           // denied feature last
		"CAMERA=()",                           // header grammar is case-insensitive
		"camera = ()",                         // spaces around the separator
		" geolocation=() , camera=() ",        // padded directives
		"microphone=(), camera=(), camera=()", // denied twice over
	}
	for _, policy := range denied {
		if !policyDeniesFeature(policy, "camera") {
			t.Errorf("policyDeniesFeature(%q, camera) = false, want denied", policy)
		}
	}

	granted := []string{
		"camera=(self)",                         // granted to the page
		"camera=(self), camera=()",              // ANY granting directive means not provably denied
		"camera=*, camera=()",                   // wildcard grant
		"camera=(https://a.example), camera=()", // origin-list grant (undecidable, but never a boot warning)
		"fullscreen=(camera)",                   // the name appears only INSIDE another allowlist
		"camera=(())",                           // nested parens are not the empty allowlist
		"screen-orientation=()",                 // a different feature entirely
		"",                                      // names nothing
	}
	for _, policy := range granted {
		if policyDeniesFeature(policy, "camera") {
			t.Errorf("policyDeniesFeature(%q, camera) = true, want granted/undecidable/silent", policy)
		}
	}

	// The same rule end to end: exactly one warning for a pure denial, and
	// silence when a sibling directive grants — through CheckHostRequirements,
	// which is the surface an app actually runs at boot.
	for _, tc := range []struct {
		policy string
		warns  int
	}{
		{"camera=()", 1},
		{"camera=(self), camera=()", 0},
		{"camera=(), camera=*", 0},
	} {
		log, buf := captureWarnings()
		CheckHostRequirements(log, tc.policy, scannerModule(t, "camera"))
		if got := warnCount(buf); got != tc.warns {
			t.Errorf("CheckHostRequirements(%q) warned %d times, want %d", tc.policy, got, tc.warns)
		}
	}
}
