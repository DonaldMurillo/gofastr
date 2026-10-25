package main

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func stubBuildInfo(t *testing.T, info *debug.BuildInfo) {
	t.Helper()
	old := project.ReadBuildInfo
	project.ReadBuildInfo = func() (*debug.BuildInfo, bool) { return info, info != nil }
	t.Cleanup(func() { project.ReadBuildInfo = old })
}

func TestVersionCommandPrintsRelease(t *testing.T) {
	stubBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: project.GofastrModule, Version: "v0.87.0"}})
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out := clitest.CaptureStdout(t, func() { dispatch(args) })
		if !strings.HasPrefix(out, "GoFastr v0.87.0 ") {
			t.Errorf("gofastr %s printed %q, want the installed release", args[0], out)
		}
	}
}
