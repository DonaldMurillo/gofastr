package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// readBuildInfo is debug.ReadBuildInfo behind a seam so tests can hand
// versionLine the build info a `go install …@vX.Y.Z` binary carries.
var readBuildInfo = debug.ReadBuildInfo

// cliVersion is the release this binary was built from. Release builds
// set version through -ldflags; `go install module/cmd/gofastr@vX.Y.Z`
// sets no ldflags but records the module version in the build info, and
// printing "dev" there left the CLI unable to say which release it is
// (#503). A local build from a checkout has neither and stays "dev".
func cliVersion() string {
	if v := normalizeFrameworkVersion(version); v != "" {
		return v
	}
	if info, ok := readBuildInfo(); ok && isGofastrMainModule(info.Main.Path) {
		if v := normalizeFrameworkVersion(info.Main.Version); v != "" {
			return v
		}
	}
	return version
}

func isGofastrMainModule(path string) bool {
	return path == gofastrModule || strings.HasPrefix(path, gofastrModule+"/cmd/")
}

// versionLine is what `gofastr version` and `--version` print. The commit
// and build time fall back to the VCS stamp the go command records when
// it builds from a checkout; a module-mode `go install` records neither,
// so those two stay at their defaults there.
func versionLine() string {
	c, d := commit, buildDate
	if info, ok := readBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "none" && s.Value != "" {
					c = s.Value[:min(12, len(s.Value))]
				}
			case "vcs.time":
				if d == "unknown" && s.Value != "" {
					d = s.Value
				}
			}
		}
	}
	return fmt.Sprintf("GoFastr %s (commit: %s, built: %s)", cliVersion(), c, d)
}
