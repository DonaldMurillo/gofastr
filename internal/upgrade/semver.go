package upgrade

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// ValidateSemver accepts vMAJOR.MINOR.PATCH with an optional
// -prerelease (or +build) suffix, covering the pseudo-versions Go
// writes into go.mod. The short forms semver allows (v1, v1.2) are
// refused: a registry release and a go.mod require always spell all
// three numbers.
func ValidateSemver(v string) error {
	if !semver.IsValid(v) || !hasThreeNumbers(v) {
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

// SemverLess reports a < b by semver precedence: a prerelease (or
// pseudo-version) sorts before its release, and numeric prerelease
// identifiers compare as numbers (rc.2 < rc.10). Malformed versions
// compare as lowest so an unknown current version includes every
// registry entry up to target.
func SemverLess(a, b string) bool {
	if ValidateSemver(a) != nil {
		return ValidateSemver(b) == nil
	}
	if ValidateSemver(b) != nil {
		return false
	}
	return semver.Compare(a, b) < 0
}

// ReleasesInRange returns the registry entries in (current, target],
// i.e. everything the project crosses when moving current → target.
// An empty/unknown current includes everything up to target.
func ReleasesInRange(reg *Registry, current, target string) []Release {
	var out []Release
	for _, r := range reg.Releases {
		if current != "" && !SemverLess(current, r.Version) {
			continue
		}
		if SemverLess(target, r.Version) {
			continue
		}
		out = append(out, r)
	}
	return out
}
