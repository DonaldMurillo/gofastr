package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// Registry parsing and validation live in internal/upgrade
// (registry_test.go): the embedded file, the refusal table, the
// semver helpers. What stays here is the CLI: go.mod reading, argument
// parsing, and the renderer.

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

func TestParseUpgradeArgsFrom(t *testing.T) {
	for _, args := range [][]string{{"--from", "v0.85.0"}, {"--from=v0.85.0"}} {
		opts, bad := parseUpgradeArgs(args)
		if bad != "" || opts.from != "v0.85.0" {
			t.Errorf("args %v: from=%q bad=%q", args, opts.from, bad)
		}
	}
}

// bumpedApp writes an app whose go.mod already names v0.86.0: the user
// ran go get before gofastr upgrade.
func bumpedApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", "module app\n\ngo 1.27\n\nrequire "+gofastrModule+" v0.86.0\n")
	return dir
}

func TestUpgradeBumpedGoModHintsFrom(t *testing.T) {
	dir := bumpedApp(t)
	out := covT_capStdout(t, func() { runUpgrade([]string{dir, "--to", "v0.86.0"}) })
	if !strings.Contains(out, "nothing to do") || !strings.Contains(out, "--from") {
		t.Fatalf("a bumped go.mod must point at --from:\n%s", out)
	}
}

func TestUpgradeFromOverridesGoMod(t *testing.T) {
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true}, nil
	})
	dir := bumpedApp(t)
	out := covT_capStdout(t, func() { runUpgrade([]string{dir, "--from", "v0.85.0", "--to", "v0.86.0"}) })
	if !strings.Contains(out, "Current: v0.85.0 (--from)") {
		t.Fatalf("--from must replace the go.mod version:\n%s", out)
	}
	if strings.Contains(out, "nothing to do") || !strings.Contains(out, "v0.86.0") {
		t.Fatalf("--from must report the v0.86.0 notes:\n%s", out)
	}
}

func TestUpgradeFromRejectsBadVersion(t *testing.T) {
	dir := bumpedApp(t)
	code := covT_capExit(t, func() { runUpgrade([]string{dir, "--from", "latest", "--to", "v0.86.0"}) })
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a non-semver --from", code)
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

// stubScan replaces the engine entry for one test.
func stubScan(t *testing.T, fn func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error)) {
	t.Helper()
	old := scanRun
	scanRun = fn
	t.Cleanup(func() { scanRun = old })
}

func oneRelease(notes ...*upgrade.Note) []upgrade.Release {
	return []upgrade.Release{{Version: "v0.23.0", Title: "Auth", Notes: notes}}
}

func TestUpgradeReportRendersHits(t *testing.T) {
	note := &upgrade.Note{
		Change: "theme.Default() is now adaptive", Breaking: true,
		Guidance: "Pass Overrides.",
		Find:     upgrade.Find{Uses: []upgrade.Symbol{{Pkg: "p", Name: "N"}}},
	}
	sinks := upgrade.MarkerSinks{AttrKeys: []string{"data-cui-comp"}}
	stubScan(t, func(root string, notes []*upgrade.Note, gotSinks upgrade.MarkerSinks) (*scan.Result, error) {
		if len(notes) != 1 || notes[0] != note {
			t.Errorf("engine must receive the in-range notes, got %d", len(notes))
		}
		if len(gotSinks.AttrKeys) != 1 || gotSinks.AttrKeys[0] != "data-cui-comp" {
			t.Errorf("engine must receive the registry's marker sinks, got %+v", gotSinks)
		}
		return &scan.Result{
			TypeChecked: true,
			Hits: map[*upgrade.Note][]scan.Hit{
				// Deliberately out of order: the renderer sorts.
				note: {
					{File: "web/b.go", Line: 3, Col: 1, Why: "why-b"},
					{File: "theme/theme.go", Line: 5, Col: 12, Why: "p.N"},
					{File: "gofastr.yml", Line: 9, Why: "config auth"}, // no column
				},
			},
		}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), sinks)
	for _, want := range []string{
		"v0.23.0: Auth",
		"! BREAKING: theme.Default() is now adaptive",
		"Pass Overrides.",
		"Edit these:",
		"theme/theme.go:5:12  p.N",
		"gofastr.yml:9  config auth",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q, got:\n%s", want, report)
		}
	}
	if strings.Index(report, "theme/theme.go") > strings.Index(report, "web/b.go") {
		t.Errorf("hits must print in file order, got:\n%s", report)
	}
	if strings.Contains(report, "Compile errors") {
		t.Errorf("no Unexplained errors were reported, got:\n%s", report)
	}
}

// The report leads with the lines that must change, then the lines to
// look at, then the notes nothing in the project touched. A hit read
// from a compile error is an edit whatever its note says: the line no
// longer builds.
func TestUpgradeReportSplitsEditFromCheck(t *testing.T) {
	edit := &upgrade.Note{Change: "symbol removed", Breaking: true, Guidance: "g-edit"}
	review := &upgrade.Note{Change: "default flipped", Breaking: true, Review: true, Guidance: "g-review"}
	quiet := &upgrade.Note{Change: "nothing here", Breaking: true, Review: true, Guidance: "g-quiet"}
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true, Hits: map[*upgrade.Note][]scan.Hit{
			edit:   {{File: "a.go", Line: 1, Col: 1, Why: "uses x.Gone"}},
			review: {{File: "b.go", Line: 2, Col: 1, Why: "uses x.Flipped"}, {File: "c.go", Line: 3, Col: 1, Why: "uses x.Flipped", Err: "too many arguments"}},
		}}, nil
	})
	report := upgradeReport(t.TempDir(), []upgrade.Release{
		{Version: "v0.22.0", Title: "Old", Notes: []*upgrade.Note{review}},
		{Version: "v0.23.0", Title: "Auth", Notes: []*upgrade.Note{edit, quiet}},
	}, upgrade.MarkerSinks{})
	order := []string{
		"Edit these",
		"v0.22.0: Old", "default flipped", "c.go:3:1", "too many arguments",
		"v0.23.0: Auth", "symbol removed", "a.go:1:1",
		"Check these",
		"v0.22.0: Old", "default flipped", "b.go:2:1",
		"Nothing found for these",
		"v0.23.0: Auth", "nothing here", "g-quiet",
	}
	rest := report
	for _, want := range order {
		i := strings.Index(rest, want)
		if i < 0 {
			t.Fatalf("report missing %q after the previous lines, got:\n%s", want, report)
		}
		rest = rest[i+len(want):]
	}
	if strings.Count(report, "b.go:2:1") != 1 || strings.Count(report, "c.go:3:1") != 1 {
		t.Errorf("each hit prints once, got:\n%s", report)
	}
	// A note prints once per section holding its hits, and under
	// "Nothing found" only when it has none.
	for change, want := range map[string]int{"symbol removed": 1, "default flipped": 2, "nothing here": 1} {
		if got := strings.Count(report, change); got != want {
			t.Errorf("%q prints %d times, want %d, got:\n%s", change, got, want, report)
		}
	}
}

func TestUpgradeReportCapsHitsAtTwenty(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	const total = 25
	var hits []scan.Hit
	for i := range total {
		hits = append(hits, scan.Hit{File: fmt.Sprintf("f%02d.go", i+1), Line: 1, Col: 1, Why: "w"})
	}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true, Hits: map[*upgrade.Note][]scan.Hit{note: hits}}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	rendered := strings.Count(report, ":1:1  w")
	if rendered != maxNoteHits {
		t.Errorf("rendered %d hit lines, want the cap of %d", rendered, maxNoteHits)
	}
	if !strings.Contains(report, "… and 5 more") {
		t.Errorf("capped hits must end with a count line, got:\n%s", report)
	}
}

func TestUpgradeReportListsUnexplained(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	const total = 22
	var unexplained []scan.Hit
	for i := range total {
		unexplained = append(unexplained, scan.Hit{
			File: fmt.Sprintf("e%02d.go", i+1), Line: 2, Col: 3,
			Why: fmt.Sprintf("undefined: ui.SiteHeader%d", i+1),
		})
	}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true, Unexplained: unexplained}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	if !strings.Contains(report, "Compile errors no note explains:") {
		t.Errorf("report missing the unexplained block, got:\n%s", report)
	}
	if !strings.Contains(report, "e01.go:2:3  undefined: ui.SiteHeader1") {
		t.Errorf("report missing the first unexplained error, got:\n%s", report)
	}
	rendered := strings.Count(report, "undefined: ui.SiteHeader")
	if rendered != maxNoteHits {
		t.Errorf("rendered %d unexplained errors, want the cap of %d", rendered, maxNoteHits)
	}
	if !strings.Contains(report, "… and 2 more") {
		t.Errorf("capped errors must end with a count line, got:\n%s", report)
	}
}

func TestUpgradeReportListsUnscanned(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	var unscanned []string
	for i := range maxNoteHits + 3 {
		unscanned = append(unscanned, fmt.Sprintf("x%02d.go (no satisfiable build configuration)", i+1))
	}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true, Unscanned: unscanned}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	head, _, _ := strings.Cut(report, "v0.23.0")
	if !strings.Contains(head, "NOTE: 23 files could not be scanned") {
		t.Errorf("the unscanned NOTE must lead the report, got:\n%s", report)
	}
	if !strings.Contains(head, "  x01.go (no satisfiable build configuration)\n") {
		t.Errorf("report missing the first unscanned file, got:\n%s", report)
	}
	if got := strings.Count(report, "(no satisfiable build configuration)"); got != maxNoteHits {
		t.Errorf("rendered %d unscanned files, want the cap of %d", got, maxNoteHits)
	}
	if !strings.Contains(head, "  … and 3 more\n") {
		t.Errorf("capped files must end with a count line, got:\n%s", report)
	}
}

func TestUpgradeReportPositionlessError(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{Unexplained: []scan.Hit{{Why: "go: inconsistent vendoring\n\tsee go help vendor"}}}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	if !strings.Contains(report, "  (no position)  go: inconsistent vendoring\n      \tsee go help vendor\n") {
		t.Errorf("positionless error must render with no position and indented continuation lines, got:\n%s", report)
	}
}

func TestUpgradeReportShowsCompileError(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{Hits: map[*upgrade.Note][]scan.Hit{note: {
			{File: "a.go", Line: 3, Col: 5, Why: "field x.Config.Public", Err: "unknown field Public in struct literal"},
			{File: "b.go", Line: 7, Col: 2, Why: "x.Config"},
		}}}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	if !strings.Contains(report, "a.go:3:5  field x.Config.Public\n          compile error: unknown field Public in struct literal\n") {
		t.Errorf("a hit read from a compile error must show the error under it, got:\n%s", report)
	}
	if strings.Count(report, "compile error:") != 1 {
		t.Errorf("a typed hit must not carry a compile-error line, got:\n%s", report)
	}
}

func TestUpgradeReportNotesBrokenTypeCheck(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	var broken []string
	for i := range 7 {
		broken = append(broken, fmt.Sprintf("example.com/app/p%d", i+1))
	}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{Broken: broken}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	if !strings.Contains(report, "did not type-check") {
		t.Errorf("report missing the not-type-checked NOTE, got:\n%s", report)
	}
	if !strings.Contains(report, "example.com/app/p5") {
		t.Errorf("NOTE must name the first five broken packages, got:\n%s", report)
	}
	if strings.Contains(report, "example.com/app/p6") || strings.Contains(report, "example.com/app/p7,") {
		t.Errorf("NOTE must cap at five packages, got:\n%s", report)
	}
	if !strings.Contains(report, "and 2 more") {
		t.Errorf("NOTE must count the packages past the cap, got:\n%s", report)
	}
	if i := strings.Index(report, "NOTE: the app did not type-check"); i > strings.Index(report, "v0.23.0") {
		t.Errorf("the NOTE must print before the notes, got:\n%s", report)
	}
}

func TestUpgradeReportScansErrorShowsNotes(t *testing.T) {
	notes := []*upgrade.Note{
		{Change: "first change", Breaking: true, Guidance: "first guidance"},
		{Change: "second change", Breaking: true, Guidance: "second guidance"},
	}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return nil, fmt.Errorf("no go.mod at or above %s", root)
	})
	report := upgradeReport(t.TempDir(), oneRelease(notes...), upgrade.MarkerSinks{})
	for _, want := range []string{
		"NOTE: could not scan this project",
		"no go.mod at or above",
		"! BREAKING: first change",
		"first guidance",
		"! BREAKING: second change",
		"second guidance",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("engine failure must not cost guidance: missing %q, got:\n%s", want, report)
		}
	}
	if strings.Contains(report, "Edit these") || strings.Contains(report, "Check these") {
		t.Errorf("a failed scan renders no hit sections, got:\n%s", report)
	}
}

func TestUpgradeReportEmptyRangeSkipsScan(t *testing.T) {
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		t.Error("no notes in range: the engine must not run")
		return nil, nil
	})
	report := upgradeReport(t.TempDir(), nil, upgrade.MarkerSinks{})
	want := "No migration notes between these versions: the mechanical steps below are all there is.\n"
	if report != want {
		t.Errorf("got:\n%s\nwant:\n%s", report, want)
	}
}

// A go.mod version is project data: one carrying terminal escapes is
// refused, never printed raw.
func TestUpgradeRefusesEscapeInGoModVersion(t *testing.T) {
	dir := t.TempDir()
	writeUpgradeFixture(t, dir, "go.mod", "module app\n\ngo 1.27\n\nrequire "+gofastrModule+" v0.80.0\x1b]0;PWNED\a\x1b[2J\n")
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runUpgrade([]string{dir, "--to", "v0.86.0"}) })
	})
	if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, '\a') {
		t.Errorf("the go.mod version reached the terminal raw:\n%q", out)
	}
	if code != 1 {
		t.Errorf("exit = %d, want 1 for a go.mod version that is not vX.Y.Z", code)
	}
}

// Hit text comes from the project (gofastr.yml keys, file names,
// compile errors): control and bidi runes never reach the terminal.
func TestUpgradeReportScrubsHitText(t *testing.T) {
	note := &upgrade.Note{Change: "c", Breaking: true, Guidance: "g"}
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{
			Hits: map[*upgrade.Note][]scan.Hit{note: {
				{File: "gofastr.yml", Line: 2, Why: "config auth\u202Eevil\u009B2J", Err: "bad\x1b[2Jthing"},
			}},
			Unexplained: []scan.Hit{{File: "x\u009B.go", Line: 1, Why: "err\x1b]0;T\a"}},
			Unscanned:   []string{"y\u202E.go"},
			Broken:      []string{"pkg\x1b[2J"},
		}, nil
	})
	report := upgradeReport(t.TempDir(), oneRelease(note), upgrade.MarkerSinks{})
	for _, r := range []rune{0x1b, '\a', 0x202E, 0x9B} {
		if strings.ContainsRune(report, r) {
			t.Errorf("report carries raw %U:\n%q", r, report)
		}
	}
	if !strings.Contains(report, "gofastr.yml:2  config authevil2J") {
		t.Errorf("scrubbed hit text missing, got:\n%s", report)
	}
}
