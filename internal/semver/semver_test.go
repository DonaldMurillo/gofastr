package semver

import "testing"

func TestLess(t *testing.T) {
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
		if got := Less(c.a, c.b); got != c.want {
			t.Errorf("Less(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPrereleaseAndPseudoVersions(t *testing.T) {
	// A pseudo-version sits between its base's predecessor and the base.
	if !Less("v0.25.0", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("pseudo-version of v0.25.1 must be newer than v0.25.0")
	}
	if !Less("v0.25.1-0.20260715120000-abcdef123456", "v0.25.1") {
		t.Errorf("prerelease must sort before its release")
	}
	if Less("v0.25.1", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("release must not sort before its own prerelease")
	}
	// Prerelease targets parse.
	if err := Validate("v0.26.0-rc.1"); err != nil {
		t.Errorf("prerelease target must parse: %v", err)
	}
	for _, bad := range []string{"0.26.0", "v0.26", "v1", "v0.26.0.1", "v0.026.0", "v0.26.0-", "v0.26.0-rc.01"} {
		if Validate(bad) == nil {
			t.Errorf("Validate(%q) accepted a malformed version", bad)
		}
	}
}

// Malformed versions sort lowest, so an unknown current version reads
// as older than every real release.
func TestLessMalformedSortsLowest(t *testing.T) {
	if !Less("garbage", "v0.1.0") {
		t.Error("a malformed version must sort before a valid one")
	}
	if Less("v0.1.0", "garbage") {
		t.Error("a valid version must not sort before a malformed one")
	}
	if Less("garbage", "also-garbage") {
		t.Error("two malformed versions must not order")
	}
}
