package update

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Semver: major.minor.patch with an optional pre-release, per semver
// 2.0.0 restricted to what a feed can carry. Build metadata (+...) is
// refused rather than ignored: two versions differing only in build
// metadata are EQUAL under semver, and a silent equal would make the
// updater download the same version forever.

// ErrBadVersion is the sentinel for a string that is not semver.
var ErrBadVersion = errors.New("not a semantic version")

// Version is one parsed semver value.
type Version struct {
	Major, Minor, Patch uint64
	// Pre is the dot-separated pre-release identifiers; nil means a
	// release (which sorts ABOVE every pre-release of the same triple).
	Pre []string
}

// ParseVersion parses "1.2.3", "1.2.3-beta.1", "1.2.3-rc". It refuses
// leading zeros in numeric components, empty identifiers, build
// metadata, and anything else outside the grammar.
func ParseVersion(s string) (Version, error) {
	// Split off a pre-release; keep the core strictly numeric.
	core, pre, hasPre := strings.Cut(s, "-")
	if hasPre && pre == "" {
		return Version{}, fmt.Errorf("%w: %q has an empty pre-release", ErrBadVersion, s)
	}
	var v Version
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q is not major.minor.patch", ErrBadVersion, s)
	}
	var err error
	if v.Major, err = parseNumeric(parts[0]); err != nil {
		return Version{}, fmt.Errorf("%w: %q major: %v", ErrBadVersion, s, err)
	}
	if v.Minor, err = parseNumeric(parts[1]); err != nil {
		return Version{}, fmt.Errorf("%w: %q minor: %v", ErrBadVersion, s, err)
	}
	if v.Patch, err = parseNumeric(parts[2]); err != nil {
		return Version{}, fmt.Errorf("%w: %q patch: %v", ErrBadVersion, s, err)
	}
	if !hasPre {
		return v, nil
	}
	for _, id := range strings.Split(pre, ".") {
		if !validPreIdentifier(id) {
			return Version{}, fmt.Errorf("%w: %q pre-release identifier %q", ErrBadVersion, s, id)
		}
		v.Pre = append(v.Pre, id)
	}
	return v, nil
}

// parseNumeric parses one numeric component: digits only, no leading
// zero unless the value is zero.
func parseNumeric(s string) (uint64, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, errors.New("leading zero")
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("not digits")
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errors.New("out of range")
	}
	return n, nil
}

// validPreIdentifier accepts one pre-release identifier: [0-9A-Za-z-]+,
// numeric identifiers without a leading zero.
func validPreIdentifier(id string) bool {
	if id == "" {
		return false
	}
	numeric := true
	for i := range len(id) {
		c := id[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
			numeric = false
		default:
			return false
		}
	}
	if numeric && len(id) > 1 && id[0] == '0' {
		return false
	}
	return true
}

// Compare orders two versions: -1 when a < b, 0 when equal, 1 when
// a > b, per semver 2.0.0 precedence (pre-release < release; numeric
// identifiers numerically; alphanumeric lexically, below numeric; a
// longer identifier list wins when the prefixes are equal).
func Compare(a, b Version) int {
	if c := cmpU64(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpU64(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpU64(a.Patch, b.Patch); c != 0 {
		return c
	}
	// A release sorts above any pre-release.
	if len(a.Pre) == 0 || len(b.Pre) == 0 {
		switch {
		case len(a.Pre) == len(b.Pre):
			return 0
		case len(a.Pre) == 0:
			return 1
		default:
			return -1
		}
	}
	for i := 0; i < len(a.Pre) && i < len(b.Pre); i++ {
		if c := comparePreID(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a.Pre), len(b.Pre))
}

// comparePreID orders two pre-release identifiers.
func comparePreID(a, b string) int {
	an, aIsNum := numericID(a)
	bn, bIsNum := numericID(b)
	switch {
	case aIsNum && bIsNum:
		return cmpU64(an, bn)
	case aIsNum: // numeric identifiers sort below alphanumeric
		return -1
	case bIsNum:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// numericID parses an identifier already validated as numeric.
func numericID(s string) (uint64, bool) {
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// IsNewer reports whether candidate sorts above running. An empty
// running version is never updated: the caller treats it as unbundled.
func IsNewer(candidate, running string) (bool, error) {
	if running == "" {
		return false, nil
	}
	c, err := ParseVersion(candidate)
	if err != nil {
		return false, err
	}
	r, err := ParseVersion(running)
	if err != nil {
		return false, err
	}
	return Compare(c, r) > 0, nil
}

func cmpU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
