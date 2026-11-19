package project

import (
	"strings"

	"golang.org/x/mod/module"

	relver "github.com/DonaldMurillo/gofastr/internal/semver"
)

// IsReleaseTag reports whether v names a tag that can carry a release
// archive: the three-number semver the shared validator demands, with
// no pseudo-version (an untagged commit, `go install ...@main`) and no
// build metadata (a `+dirty` local build); a word such as the `dev`
// marker version or an abbreviated `v1.2` cannot.
func IsReleaseTag(v string) bool {
	return relver.Validate(v) == nil && !strings.Contains(v, "+") && !module.IsPseudoVersion(v)
}
