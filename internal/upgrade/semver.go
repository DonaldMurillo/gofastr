package upgrade

import (
	"fmt"
	"strconv"
	"strings"
)

// Semver is a parsed version: the vX.Y.Z core plus any prerelease
// suffix (which is also how Go pseudo-versions look:
// v0.25.1-0.20260715120000-abcdef123456).
type Semver struct {
	nums [3]int
	pre  string // "" for a release; the "-…" tail (sans dash) otherwise
}

// ParseSemver parses vMAJOR.MINOR.PATCH with an optional -prerelease
// (or +build, ignored) suffix, covering the pseudo-versions Go writes
// into go.mod.
func ParseSemver(v string) (Semver, error) {
	var out Semver
	if !strings.HasPrefix(v, "v") {
		return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
	}
	core := strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(core, '-'); i >= 0 {
		out.pre = core[i+1:]
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("version %q must look like vX.Y.Z", v)
		}
		out.nums[i] = n
	}
	return out, nil
}

// SemverLess reports a < b. Same-core comparisons follow semver: a
// prerelease (or pseudo-version) sorts before its release; two
// prereleases compare lexically (exact enough for pseudo-version
// timestamps). Malformed versions compare as lowest so an unknown
// current version includes every registry entry up to target.
func SemverLess(a, b string) bool {
	av, aerr := ParseSemver(a)
	bv, berr := ParseSemver(b)
	if aerr != nil {
		return berr == nil
	}
	if berr != nil {
		return false
	}
	for i := range 3 {
		if av.nums[i] != bv.nums[i] {
			return av.nums[i] < bv.nums[i]
		}
	}
	if (av.pre == "") != (bv.pre == "") {
		return av.pre != "" // prerelease < release
	}
	return av.pre < bv.pre
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
