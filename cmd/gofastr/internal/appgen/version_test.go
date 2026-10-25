package appgen

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func stubBuildInfo(t *testing.T, info *debug.BuildInfo) {
	t.Helper()
	old := project.ReadBuildInfo
	project.ReadBuildInfo = func() (*debug.BuildInfo, bool) { return info, info != nil }
	t.Cleanup(func() { project.ReadBuildInfo = old })
}

// #503: a `go install …/cmd/gofastr@v0.87.0` binary printed "GoFastr dev".
func TestVersionReadsGoInstallBuildInfo(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{
		Path: project.GofastrModule + "/cmd/gofastr",
		Main: debug.Module{Path: project.GofastrModule, Version: "v0.87.0"},
	})
	if got := project.VersionLine(); !strings.HasPrefix(got, "GoFastr v0.87.0 ") {
		t.Fatalf("versionLine() = %q, want the installed release v0.87.0", got)
	}
}

func TestVersionLdflagsWinOverBuildInfo(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: project.GofastrModule, Version: "v0.1.0"}})
	old := cli.Version
	cli.Version = "v0.87.2"
	t.Cleanup(func() { cli.Version = old })
	if got := project.CliVersion(); got != "v0.87.2" {
		t.Fatalf("cliVersion() = %q, want the ldflags version v0.87.2", got)
	}
}

func TestVersionCheckoutBuildShowsCommit(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Path: project.GofastrModule, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "bdea4ca6d0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-10-12T15:00:00Z"},
		},
	})
	got := project.VersionLine()
	if !strings.HasPrefix(got, "GoFastr dev ") {
		t.Errorf("versionLine() = %q, want dev for a checkout build", got)
	}
	if !strings.Contains(got, "commit: bdea4ca6d012,") || !strings.Contains(got, "committed: 2026-10-12T15:00:00Z") {
		t.Errorf("versionLine() = %q, want the VCS stamp as commit and commit time", got)
	}
	if strings.Contains(got, "built: 2026") {
		t.Errorf("versionLine() = %q: vcs.time is the commit time, not the build time", got)
	}
}

// Another module's main package must not lend its version to this CLI.
func TestVersionIgnoresForeignModule(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: "example.com/app", Version: "v3.0.0"}})
	if got := project.CliVersion(); got != "dev" {
		t.Fatalf("cliVersion() = %q, want dev", got)
	}
}
