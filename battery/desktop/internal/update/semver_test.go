package update

import (
	"errors"
	"testing"
)

func TestParseVersionAccepts(t *testing.T) {
	for _, s := range []string{"0.0.0", "1.2.3", "10.20.30", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-0.3.7", "1.0.0-x.7.z.92", "1.0.0-rc-1"} {
		if _, err := ParseVersion(s); err != nil {
			t.Errorf("ParseVersion(%q) = %v, want ok", s, err)
		}
	}
}

func TestParseVersionRefuses(t *testing.T) {
	for _, s := range []string{
		"", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03",
		"1.2.x", "v1.2.3", "1.2.3-", "1.2.3-.", "1.2.3-01", "1.2.3-a..b",
		"1.2.3+a", "1.2.3-a+b", " 1.2.3", "1.2.3 ", "1.2.-3",
	} {
		if _, err := ParseVersion(s); !errors.Is(err, ErrBadVersion) {
			t.Errorf("ParseVersion(%q) = %v, want ErrBadVersion", s, err)
		}
	}
}

func TestCompareOrdersVersions(t *testing.T) {
	// ordered ascending; every pair must compare a < b.
	ordered := []string{
		"0.0.1", "0.1.0", "1.0.0-1", "1.0.0-2", "1.0.0-10", "1.0.0-alpha",
		"1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.2.0", "1.10.0",
		"2.0.0", "10.0.0",
	}
	vs := make([]Version, len(ordered))
	for i, s := range ordered {
		v, err := ParseVersion(s)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", s, err)
		}
		vs[i] = v
	}
	for i := range vs {
		if Compare(vs[i], vs[i]) != 0 {
			t.Errorf("Compare(%s, %s) != 0", ordered[i], ordered[i])
		}
		for j := i + 1; j < len(vs); j++ {
			if Compare(vs[i], vs[j]) >= 0 {
				t.Errorf("Compare(%s, %s) >= 0, want < (semver precedence)", ordered[i], ordered[j])
			}
			if Compare(vs[j], vs[i]) <= 0 {
				t.Errorf("Compare(%s, %s) <= 0, want >", ordered[j], ordered[i])
			}
		}
	}
}

func TestIsNewerRules(t *testing.T) {
	cases := []struct {
		candidate, running string
		want               bool
	}{
		{"1.2.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"0.9.0", "1.0.0", false},
		{"1.0.0", "1.0.0-beta", true},  // release above pre-release
		{"1.0.0-beta", "1.0.0", false}, // pre-release below release
		{"1.1.0", "", false},           // unbundled never updates
		{"999.0.0", "", false},
	}
	for _, c := range cases {
		got, err := IsNewer(c.candidate, c.running)
		if err != nil || got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, %v; want %v, nil", c.candidate, c.running, got, err, c.want)
		}
	}
	if _, err := IsNewer("not-semver", "1.0.0"); !errors.Is(err, ErrBadVersion) {
		t.Errorf("IsNewer(bad) = %v, want ErrBadVersion", err)
	}
	if _, err := IsNewer("1.0.0", "not-semver"); !errors.Is(err, ErrBadVersion) {
		t.Errorf("IsNewer(running bad) = %v, want ErrBadVersion", err)
	}
}
