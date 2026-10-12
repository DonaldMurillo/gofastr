package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func stubBuildInfo(t *testing.T, info *debug.BuildInfo) {
	t.Helper()
	old := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, info != nil }
	t.Cleanup(func() { readBuildInfo = old })
}

// #503: a `go install …/cmd/gofastr@v0.87.0` binary printed "GoFastr dev".
func TestVersionReadsGoInstallBuildInfo(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{
		Path: gofastrModule + "/cmd/gofastr",
		Main: debug.Module{Path: gofastrModule, Version: "v0.87.0"},
	})
	if got := versionLine(); !strings.HasPrefix(got, "GoFastr v0.87.0 ") {
		t.Fatalf("versionLine() = %q, want the installed release v0.87.0", got)
	}
}

func TestVersionLdflagsWinOverBuildInfo(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: gofastrModule, Version: "v0.1.0"}})
	old := version
	version = "v0.87.2"
	t.Cleanup(func() { version = old })
	if got := cliVersion(); got != "v0.87.2" {
		t.Fatalf("cliVersion() = %q, want the ldflags version v0.87.2", got)
	}
}

func TestVersionCheckoutBuildShowsCommit(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Path: gofastrModule, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "bdea4ca6d0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-10-12T15:00:00Z"},
		},
	})
	got := versionLine()
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
	if got := cliVersion(); got != "dev" {
		t.Fatalf("cliVersion() = %q, want dev", got)
	}
}

func TestVersionCommandPrintsRelease(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: gofastrModule, Version: "v0.87.0"}})
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out := captureStdout(t, func() { dispatch(args) })
		if !strings.HasPrefix(out, "GoFastr v0.87.0 ") {
			t.Errorf("gofastr %s printed %q, want the installed release", args[0], out)
		}
	}
}
