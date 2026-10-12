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
// falls back to the VCS stamp the go command records when it builds from
// a checkout; a module-mode `go install` records none, so it stays at its
// default there. vcs.time is the commit's time, not the build's, so it is
// printed as "committed" and only when no build date was stamped.
func versionLine() string {
	c, d, label := commit, buildDate, "built"
	if info, ok := readBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "none" && s.Value != "" {
					c = s.Value[:min(12, len(s.Value))]
				}
			case "vcs.time":
				if d == "unknown" && s.Value != "" {
					d, label = s.Value, "committed"
				}
			}
		}
	}
	return fmt.Sprintf("GoFastr %s (commit: %s, %s: %s)", cliVersion(), c, label, d)
}
