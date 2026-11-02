package upgrade

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/semver"
)

// ReleasesInRange returns the registry entries in (current, target],
// i.e. everything the project crosses when moving current → target.
// An empty/unknown current includes everything up to target.
//
// A prerelease target (v0.96.0-rc.1) already carries the changes of
// the release it precedes, so it crosses that release's notes: the
// target side compares against the release's lowest prerelease,
// v0.96.0-0. The current side keeps the release itself, so moving from
// a prerelease to its release still shows the notes.
func ReleasesInRange(reg *Registry, current, target string) []Release {
	var out []Release
	for _, r := range reg.Releases {
		if current != "" && !semver.Less(current, r.Version) {
			continue
		}
		if semver.Less(target, lowestPrerelease(r.Version)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// lowestPrerelease is the lowest version that precedes v and no release
// before it: v's core with the prerelease "-0". A version that is
// itself a prerelease is returned as is.
func lowestPrerelease(v string) string {
	if strings.ContainsAny(v, "-+") {
		return v
	}
	return v + "-0"
}
