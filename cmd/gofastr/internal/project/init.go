package project

import (
	"runtime/debug"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/semver"
)

// InstalledFrameworkVersion returns the exact module version represented by
// this CLI. Release binaries may inject version via ldflags; `go install
// module/cmd/gofastr@vX.Y.Z` records it in build info without ldflags. Local
// development builds deliberately return empty rather than pretending
// `@latest` is reproducible.
func InstalledFrameworkVersion() string {
	if v := normalizeFrameworkVersion(cli.Version); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || (info.Main.Path != GofastrModule && !strings.HasPrefix(info.Main.Path, GofastrModule+"/cmd/")) {
		return ""
	}
	return normalizeFrameworkVersion(info.Main.Version)
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
