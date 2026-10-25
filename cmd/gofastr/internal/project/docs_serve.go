package project

import (
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// IsReleaseTag reports whether v names a tag that can carry a release
// archive: a pseudo-version (an untagged commit, `go install ...@main`), a
// version with build metadata (a `+dirty` local build) or a word such as
// the `dev` marker version cannot.
func IsReleaseTag(v string) bool {
	return semver.IsValid(v) && !strings.Contains(v, "+") && !module.IsPseudoVersion(v)
}
