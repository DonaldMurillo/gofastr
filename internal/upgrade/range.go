package upgrade

import "github.com/DonaldMurillo/gofastr/internal/semver"

// ReleasesInRange returns the registry entries in (current, target],
// i.e. everything the project crosses when moving current → target.
// An empty/unknown current includes everything up to target.
func ReleasesInRange(reg *Registry, current, target string) []Release {
	var out []Release
	for _, r := range reg.Releases {
		if current != "" && !semver.Less(current, r.Version) {
			continue
		}
		if semver.Less(target, r.Version) {
			continue
		}
		out = append(out, r)
	}
	return out
}
