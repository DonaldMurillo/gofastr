package upgrade

import "testing"

func TestReleasesInRangePseudoVersionCurrent(t *testing.T) {
	// A pseudo-version current skips already-crossed releases.
	reg := &Registry{Releases: []Release{{Version: "v0.23.0"}, {Version: "v0.25.0"}}}
	got := ReleasesInRange(reg, "v0.24.1-0.20260701000000-aaaaaaaaaaaa", "v0.25.0")
	if len(got) != 1 || got[0].Version != "v0.25.0" {
		t.Errorf("pseudo-version current must not re-include older notes, got %+v", got)
	}
}

// A release candidate of v0.25.0 carries v0.25.0's changes, so a
// prerelease target crosses that release's notes; a pseudo-version
// built after v0.23.0 (v0.23.1-0.…) does not reach v0.25.0.
func TestReleasesInRangePrereleaseTarget(t *testing.T) {
	reg := &Registry{Releases: []Release{{Version: "v0.23.0"}, {Version: "v0.25.0"}}}
	got := ReleasesInRange(reg, "v0.23.0", "v0.25.0-rc.1")
	if len(got) != 1 || got[0].Version != "v0.25.0" {
		t.Errorf("an rc target must cross its release's notes, got %+v", got)
	}
	if got := ReleasesInRange(reg, "v0.23.0", "v0.23.1-0.20260701000000-aaaaaaaaaaaa"); len(got) != 0 {
		t.Errorf("a pseudo-version past v0.23.0 must not reach v0.25.0, got %+v", got)
	}
	// rc → final and rc → rc both still show the release's notes.
	for _, c := range [][2]string{{"v0.25.0-rc.1", "v0.25.0"}, {"v0.25.0-rc.1", "v0.25.0-rc.2"}} {
		if got := ReleasesInRange(reg, c[0], c[1]); len(got) != 1 || got[0].Version != "v0.25.0" {
			t.Errorf("%s → %s must show v0.25.0, got %+v", c[0], c[1], got)
		}
	}
}

func TestReleasesInRange(t *testing.T) {
	reg := &Registry{Releases: []Release{
		{Version: "v0.3.0"}, {Version: "v0.5.0"}, {Version: "v0.21.0"}, {Version: "v0.23.0"},
	}}
	got := ReleasesInRange(reg, "v0.5.0", "v0.23.0")
	if len(got) != 2 || got[0].Version != "v0.21.0" || got[1].Version != "v0.23.0" {
		t.Errorf("expected (v0.5.0, v0.23.0] = [v0.21.0 v0.23.0], got %+v", got)
	}
	// current == target → empty.
	if got := ReleasesInRange(reg, "v0.23.0", "v0.23.0"); len(got) != 0 {
		t.Errorf("same-version range must be empty, got %+v", got)
	}
	// Unknown current (older than everything) includes all up to target.
	if got := ReleasesInRange(reg, "", "v0.5.0"); len(got) != 2 {
		t.Errorf("empty current means from-the-beginning, got %+v", got)
	}
}
