package project

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/semver"
)

// InstalledFrameworkVersion is CliVersion as a release tag, or "" for a local build.
func InstalledFrameworkVersion() string {
	return normalizeFrameworkVersion(CliVersion())
}

func normalizeFrameworkVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" || v == "(devel)" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if err := semver.Validate(v); err != nil {
		return ""
	}
	return v
}
