package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUpgradeRegistryParsesAndIsSorted(t *testing.T) {
	reg, err := loadUpgradeRegistry()
	if err != nil {
		t.Fatalf("loadUpgradeRegistry: %v", err)
	}
	if len(reg) < 5 {
		t.Fatalf("registry suspiciously small: %d releases", len(reg))
	}
	for i, r := range reg {
		if _, err := parseSemver(r.Version); err != nil {
			t.Errorf("release %d version %q: %v", i, r.Version, err)
		}
		if len(r.Notes) == 0 {
			t.Errorf("release %s has no notes", r.Version)
		}
		for _, n := range r.Notes {
			if n.Change == "" || n.Guidance == "" {
				t.Errorf("release %s: note missing change/guidance: %+v", r.Version, n)
			}
		}
		if i > 0 && !semverLess(reg[i-1].Version, r.Version) {
			t.Errorf("registry not sorted ascending: %s before %s", reg[i-1].Version, r.Version)
		}
	}
}

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.4.0", true},
		{"v0.4.0", "v0.3.0", false},
		{"v0.9.0", "v0.10.0", true},
		{"v0.23.0", "v0.23.0", false},
		{"v0.23.0", "v0.23.1", true},
		{"v1.0.0", "v0.25.0", false},
	}
	for _, c := range cases {
		if got := semverLess(c.a, c.b); got != c.want {
			t.Errorf("semverLess(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUpgradeNotesInRange(t *testing.T) {
	reg := []upgradeRelease{
		{Version: "v0.3.0"}, {Version: "v0.5.0"}, {Version: "v0.21.0"}, {Version: "v0.23.0"},
	}
	got := releasesInRange(reg, "v0.5.0", "v0.23.0")
	if len(got) != 2 || got[0].Version != "v0.21.0" || got[1].Version != "v0.23.0" {
		t.Errorf("expected (v0.5.0, v0.23.0] = [v0.21.0 v0.23.0], got %+v", got)
	}
	// current == target → empty.
	if got := releasesInRange(reg, "v0.23.0", "v0.23.0"); len(got) != 0 {
		t.Errorf("same-version range must be empty, got %+v", got)
	}
	// Unknown current (older than everything) includes all up to target.
	if got := releasesInRange(reg, "", "v0.5.0"); len(got) != 2 {
		t.Errorf("empty current means from-the-beginning, got %+v", got)
	}
}

func TestGoModGofastrVersion(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require (
	github.com/DonaldMurillo/gofastr v0.21.0
	github.com/other/dep v1.2.3
)
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || replaced {
		t.Errorf("got v=%q replaced=%v", v, replaced)
	}
}

func TestGoModGofastrVersionReplaceDirective(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace github.com/DonaldMurillo/gofastr => ../gofastr
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("got v=%q replaced=%v, want v0.21.0 + replaced", v, replaced)
	}
}

func TestUpgradeDetectorsPointAtLines(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "theme/theme.go", `package theme

import "github.com/DonaldMurillo/gofastr/framework/ui/theme"

func T() any { return theme.Default(theme.Overrides{}) }
`)
	rel := upgradeRelease{Version: "v0.23.0", Notes: []upgradeNote{{
		Change: "theme.Default() is now adaptive", Breaking: true,
		Guidance: "…", Detect: `theme\.Default\(`,
	}}}
	report := formatUpgradeNotes(dir, []upgradeRelease{rel})
	if !strings.Contains(report, "v0.23.0") || !strings.Contains(report, "theme.Default() is now adaptive") {
		t.Errorf("report missing release/note, got:\n%s", report)
	}
	if !strings.Contains(report, filepath.Join("theme", "theme.go")+":5") {
		t.Errorf("report must point at the detected line, got:\n%s", report)
	}
}

func TestParseUpgradeArgsForms(t *testing.T) {
	for _, args := range [][]string{{"--to", "v0.23.0"}, {"--to=v0.23.0"}} {
		opts, bad := parseUpgradeArgs(args)
		if bad != "" || opts.to != "v0.23.0" {
			t.Errorf("args %v: to=%q bad=%q", args, opts.to, bad)
		}
	}
	opts, bad := parseUpgradeArgs([]string{"./app", "--apply"})
	if bad != "" || opts.root != "./app" || !opts.apply {
		t.Errorf("got %+v bad=%q", opts, bad)
	}
	if _, bad := parseUpgradeArgs([]string{"--wat"}); bad != "--wat" {
		t.Errorf("unknown flag must be reported, got %q", bad)
	}
}

func writeUpgradeFixture(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRegistryThroughCoversNewestEntry(t *testing.T) {
	reg, through, err := loadUpgradeRegistryFull()
	if err != nil {
		t.Fatalf("loadUpgradeRegistryFull: %v", err)
	}
	if _, err := parseSemver(through); err != nil {
		t.Fatalf("through: %v", err)
	}
	if last := reg[len(reg)-1].Version; semverLess(through, last) {
		t.Errorf("through %s is older than the newest entry %s", through, last)
	}
}

// TestUpgradeRegistryThroughMatchesChangelog is the maintenance
// tripwire: every release PR bumps CHANGELOG.md, and the registry's
// `through` marker must move with it — otherwise `gofastr upgrade`
// wrongly warns (or worse, wrongly reassures) about registry coverage.
func TestUpgradeRegistryThroughMatchesChangelog(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	re := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("no release heading found in CHANGELOG.md")
	}
	_, through, err := loadUpgradeRegistryFull()
	if err != nil {
		t.Fatal(err)
	}
	if want := "v" + m[1]; through != want {
		t.Errorf("upgrades.yml through=%s but CHANGELOG's latest release is %s — bump `through` in the release PR", through, want)
	}
}

func TestUpgradeRegistryDetectorsCompile(t *testing.T) {
	reg, err := loadUpgradeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reg {
		for _, n := range r.Notes {
			if n.Detect == "" {
				continue
			}
			if _, err := regexp.Compile(n.Detect); err != nil {
				t.Errorf("%s %q: detect regex does not compile: %v", r.Version, n.Change, err)
			}
		}
	}
}

func TestGoModGofastrVersionBlockReplace(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", `module example.com/app

go 1.26

require github.com/DonaldMurillo/gofastr v0.21.0

replace (
	github.com/DonaldMurillo/gofastr => ../gofastr
	github.com/other/dep => ../dep
)
`)
	v, replaced, err := goModGofastrVersion(dir)
	if err != nil {
		t.Fatalf("goModGofastrVersion: %v", err)
	}
	if v != "v0.21.0" || !replaced {
		t.Errorf("block-form replace must be detected: v=%q replaced=%v", v, replaced)
	}
}

func TestSemverPrereleaseAndPseudoVersions(t *testing.T) {
	// A pseudo-version sits between its base's predecessor and the base.
	if !semverLess("v0.25.0", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("pseudo-version of v0.25.1 must be newer than v0.25.0")
	}
	if !semverLess("v0.25.1-0.20260715120000-abcdef123456", "v0.25.1") {
		t.Errorf("prerelease must sort before its release")
	}
	if semverLess("v0.25.1", "v0.25.1-0.20260715120000-abcdef123456") {
		t.Errorf("release must not sort before its own prerelease")
	}
	// Prerelease targets parse.
	if _, err := parseSemver("v0.26.0-rc.1"); err != nil {
		t.Errorf("prerelease target must parse: %v", err)
	}
	// releasesInRange with a pseudo-version current skips already-crossed releases.
	reg := []upgradeRelease{{Version: "v0.23.0"}, {Version: "v0.25.0"}}
	got := releasesInRange(reg, "v0.24.1-0.20260701000000-aaaaaaaaaaaa", "v0.25.0")
	if len(got) != 1 || got[0].Version != "v0.25.0" {
		t.Errorf("pseudo-version current must not re-include older notes, got %+v", got)
	}
}
