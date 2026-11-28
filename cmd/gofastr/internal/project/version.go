package project

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// ReadBuildInfo is debug.ReadBuildInfo, a variable so tests can stub it.
var ReadBuildInfo = debug.ReadBuildInfo

// CliVersion is the -ldflags version, else the gofastr module version in the build info, else cli.Version.
func CliVersion() string {
	if v := normalizeFrameworkVersion(cli.Version); v != "" {
		return v
	}
	if info, ok := ReadBuildInfo(); ok && isGofastrMainModule(info.Main.Path) {
		if v := normalizeFrameworkVersion(info.Main.Version); v != "" {
			return v
		}
	}
	return cli.Version
}

func isGofastrMainModule(path string) bool {
	return path == GofastrModule || strings.HasPrefix(path, GofastrModule+"/cmd/")
}

// VersionLine is what `gofastr version` and `--version` print.
func VersionLine() string {
	c, d, label := cli.Commit, cli.BuildDate, "built"
	if info, ok := ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "none" && s.Value != "" {
					c = s.Value[:cli.Min(12, len(s.Value))]
				}
			case "vcs.time":
				if d == "unknown" && s.Value != "" {
					d, label = s.Value, "committed"
				}
			}
		}
	}
	return fmt.Sprintf("GoFastr %s (commit: %s, %s: %s)", CliVersion(), c, label, d)
}
