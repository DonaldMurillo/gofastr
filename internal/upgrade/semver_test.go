package upgrade

import "testing"

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.4.0", true},
		{"v0.4.0", "v0.3.0", false},
		{"v0.9.0", "v0.10.0", true},
		{"v0.23.0", "v0.23.0", false},
		{"v0.23.0", "v0.23.1", true},
		{"v1.0.0", "v0.25.0", false},
		// Numeric prerelease identifiers compare as numbers, not text.
		{"v0.86.0-rc.2", "v0.86.0-rc.10", true},
		{"v0.86.0-rc.10", "v0.86.0-rc.2", false},
		{"v0.86.0-rc.1", "v0.86.0-rc.1.1", true},
		{"v0.86.0-alpha", "v0.86.0-alpha.1", true},
		{"v0.86.0-alpha.9", "v0.86.0-beta", true},
	}
	for _, c := range cases {
		if got := SemverLess(c.a, c.b); got != c.want {
			t.Errorf("SemverLess(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSemverPrereleaseAndPseudoVersions(t *testing.T) {
	// A pseudo-version sits between its base's predecessor and the base.
	if !SemverLess("v0.25.0", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("pseudo-version of v0.25.1 must be newer than v0.25.0")
	}
	if !SemverLess("v0.25.1-0.20260715120000-abcdef123456", "v0.25.1") {
		t.Errorf("prerelease must sort before its release")
	}
	if SemverLess("v0.25.1", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("release must not sort before its own prerelease")
	}
	// Prerelease targets parse.
	if err := ValidateSemver("v0.26.0-rc.1"); err != nil {
		t.Errorf("prerelease target must parse: %v", err)
	}
	for _, bad := range []string{"0.26.0", "v0.26", "v1", "v0.26.0.1", "v0.026.0", "v0.26.0-", "v0.26.0-rc.01"} {
		if ValidateSemver(bad) == nil {
			t.Errorf("ValidateSemver(%q) accepted a malformed version", bad)
		}
	}
	// A pseudo-version current skips already-crossed releases.
	reg := &Registry{Releases: []Release{{Version: "v0.23.0"}, {Version: "v0.25.0"}}}
	got := ReleasesInRange(reg, "v0.24.1-0.20260701000000-aaaaaaaaaaaa", "v0.25.0")
	if len(got) != 1 || got[0].Version != "v0.25.0" {
		t.Errorf("pseudo-version current must not re-include older notes, got %+v", got)
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
