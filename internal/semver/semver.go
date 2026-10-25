// Package semver holds the release-version rules the gofastr CLI and the
// migration registry share: which spellings are a release version, and
// how two of them order. It is a leaf on purpose, so a command that only
// checks a version string does not link the migration registry.
package semver

import (
	"fmt"
	"strings"

	modsemver "golang.org/x/mod/semver"
)

// Validate accepts vMAJOR.MINOR.PATCH with an optional -prerelease (or
// +build) suffix, covering the pseudo-versions Go writes into go.mod.
// The short forms semver allows (v1, v1.2) are refused: a registry
// release and a go.mod require always spell all three numbers.
func Validate(v string) error {
	if !modsemver.IsValid(v) || !hasThreeNumbers(v) {
		return fmt.Errorf("version %q must look like vX.Y.Z", v)
	}
	return nil
}

// hasThreeNumbers reports whether the core, cut at the first "-" or
// "+", spells all three numbers.
func hasThreeNumbers(v string) bool {
	core := v
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	return strings.Count(core, ".") == 2
}

// Less reports a < b by semver precedence: a prerelease (or
// pseudo-version) sorts before its release, and numeric prerelease
// identifiers compare as numbers (rc.2 < rc.10). Malformed versions
// compare as lowest so an unknown current version includes every
// registry entry up to target.
func Less(a, b string) bool {
	if Validate(a) != nil {
		return Validate(b) == nil
	}
	if Validate(b) != nil {
		return false
	}
	return modsemver.Compare(a, b) < 0
}
