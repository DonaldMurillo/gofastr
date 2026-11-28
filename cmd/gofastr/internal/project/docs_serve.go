package project

import (
	"strings"

	"golang.org/x/mod/module"

	relver "github.com/DonaldMurillo/gofastr/internal/semver"
)

// IsReleaseTag reports whether v is a release tag: three-number semver, no pseudo-version, no build metadata.
func IsReleaseTag(v string) bool {
	return relver.Validate(v) == nil && !strings.Contains(v, "+") && !module.IsPseudoVersion(v)
}
