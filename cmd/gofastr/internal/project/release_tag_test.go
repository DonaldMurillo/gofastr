package project

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestIsReleaseTag(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3":                               true,
		"v0.95.0":                              true,
		"v1.2":                                 false,
		"dev":                                  false,
		"v1.2.3+dirty":                         false,
		"v0.0.0-20260101120000-abcdefabcdef":   false,
		"v1.2.4-0.20260101120000-abcdefabcdef": false,
		"":                                     false,
	} {
		if got := IsReleaseTag(v); got != want {
			t.Errorf("IsReleaseTag(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCanonicalDBDriver(t *testing.T) {
	for in, want := range map[string]string{
		"sqlite":     "sqlite3",
		"SQLite3":    "sqlite3",
		" postgres ": "postgres",
		"PostgreSQL": "postgres",
		"mysql":      "mysql",
		"MySQL":      "mysql",
		" mssql ":    "mssql",
		" MSSQL ":    "MSSQL",
		"":           "",
	} {
		if got := CanonicalDBDriver(in); got != want {
			t.Errorf("CanonicalDBDriver(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnmountedBehindNeedsTwoReleases(t *testing.T) {
	for _, tc := range []struct {
		version, against string
		want             bool
	}{
		{"v0.90.0", "v0.95.0", true},
		{"v0.95.0", "v0.95.0", false},
		{"v0.96.0", "v0.95.0", false},
		{"dev", "v0.95.0", false},
		{"v0.90.0", "dev", false},
		{"v0.0.0-20260101120000-abcdefabcdef", "v0.95.0", false},
	} {
		if got := UnmountedBehind(tc.version, tc.against); got != tc.want {
			t.Errorf("UnmountedBehind(%q, %q) = %v, want %v", tc.version, tc.against, got, tc.want)
		}
	}
}

func TestUnmountedSettingsWithNoFileNamesTheRootFile(t *testing.T) {
	root := t.TempDir()
	path, entries, err := UnmountedSettings(root)
	if err != nil || entries != nil {
		t.Fatalf("UnmountedSettings on an empty dir = %v, %v; want no entries and no error", entries, err)
	}
	if want := filepath.Join(root, settings.FileName); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestValidateBuildTargetWantsPackageMain(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/lib\n\ngo 1.24\n")
	write("lib.go", "package lib\n")
	t.Chdir(dir)
	err := ValidateBuildTarget(context.Background(), ".")
	if err == nil || !strings.Contains(err.Error(), `"lib"`) {
		t.Fatalf("ValidateBuildTarget on package lib = %v; want an error naming lib", err)
	}
	write("lib.go", "package main\n\nfunc main() {}\n")
	if err := ValidateBuildTarget(context.Background(), "."); err != nil {
		t.Fatalf("ValidateBuildTarget on package main = %v", err)
	}
}

func stubReadBuildInfo(t *testing.T, info *debug.BuildInfo) {
	t.Helper()
	old := ReadBuildInfo
	ReadBuildInfo = func() (*debug.BuildInfo, bool) { return info, info != nil }
	t.Cleanup(func() { ReadBuildInfo = old })
}

func TestCliVersionReadsOnlyTheGofastrModule(t *testing.T) {
	withCLIVersion(t, "dev")
	for main, want := range map[string]string{
		GofastrModule:                  "v0.87.0",
		GofastrModule + "/cmd/gofastr": "v0.87.0",
		GofastrModule + "X":            "dev",
		GofastrModule + "/framework":   "dev",
		"example.com/app":              "dev",
	} {
		stubReadBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: main, Version: "v0.87.0"}})
		if got := CliVersion(); got != want {
			t.Errorf("CliVersion() with main module %q = %q, want %q", main, got, want)
		}
	}
}

func TestInstalledFrameworkVersionIsEmptyForADevBuild(t *testing.T) {
	withCLIVersion(t, "dev")
	stubReadBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: GofastrModule, Version: "(devel)"}})
	if got := InstalledFrameworkVersion(); got != "" {
		t.Fatalf("InstalledFrameworkVersion() = %q for a dev build, want empty", got)
	}
	stubReadBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Path: GofastrModule, Version: "v0.87.0"}})
	if got := InstalledFrameworkVersion(); got != "v0.87.0" {
		t.Fatalf("InstalledFrameworkVersion() = %q from go install build info, want v0.87.0", got)
	}
	withCLIVersion(t, "0.88.1")
	if got := InstalledFrameworkVersion(); got != "v0.88.1" {
		t.Fatalf("InstalledFrameworkVersion() = %q with ldflags 0.88.1, want v0.88.1", got)
	}
}

func TestVersionLinePrintsCommitTimeOnlyWithoutABuildDate(t *testing.T) {
	withCLIVersion(t, "v1.0.0")
	stubReadBuildInfo(t, &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
	}})
	oldCommit, oldDate := cli.Commit, cli.BuildDate
	t.Cleanup(func() { cli.Commit, cli.BuildDate = oldCommit, oldDate })
	cli.Commit, cli.BuildDate = "none", "unknown"
	if got, want := VersionLine(), "GoFastr v1.0.0 (commit: 0123456789ab, committed: 2026-01-02T03:04:05Z)"; got != want {
		t.Fatalf("VersionLine() = %q, want %q", got, want)
	}
	cli.Commit, cli.BuildDate = "feedface", "2026-02-03"
	if got, want := VersionLine(), "GoFastr v1.0.0 (commit: feedface, built: 2026-02-03)"; got != want {
		t.Fatalf("VersionLine() = %q, want %q", got, want)
	}
}
