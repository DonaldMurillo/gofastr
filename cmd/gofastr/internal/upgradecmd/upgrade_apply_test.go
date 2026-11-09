package upgradecmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// #669 (Upgrade engine): --apply used to run `go get` on a downgrade
// anyway, and a report that still held breaking hits exited 0.

// stubRegistry replaces the embedded registry for one test: the release
// range v0.85.0→v0.86.0 holds exactly the given notes.
func stubRegistry(t *testing.T, notes ...*upgrade.Note) {
	t.Helper()
	old := loadRegistry
	loadRegistry = func() (*upgrade.Registry, error) {
		return &upgrade.Registry{
			Through:  "v0.86.0",
			Releases: []upgrade.Release{{Version: "v0.86.0", Title: "T", Notes: notes}},
		}, nil
	}
	t.Cleanup(func() { loadRegistry = old })
}

// downgradedApp is an app pinned NEWER than the target: --apply would
// run `go get ...@v0.85.0`.
func downgradedApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	clitest.WriteFileAll(t, dir+"/go.mod", "module app\n\ngo 1.27\n\nrequire "+project.GofastrModule+" v0.86.0\n")
	return dir
}

// --apply on a downgrade is refused unless --allow-downgrade says so:
// the report still prints, but no step runs and the exit is non-zero.
func TestUpgradeApplyRefusesDowngrade(t *testing.T) {
	stubRegistry(t)
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true}, nil
	})
	ran := false
	stubRunStep(t, func() error { ran = true; return nil })

	dir := downgradedApp(t)
	var out string
	code := -1
	out = clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunUpgrade([]string{dir, "--to", "v0.85.0", "--apply"}) })
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for an unallowed downgrade under --apply", code)
	}
	if ran {
		t.Error("a refused downgrade ran a step anyway")
	}
	if !strings.Contains(out, "--allow-downgrade") || !strings.Contains(out, "Downgrade refused") {
		t.Fatalf("refusal must name the flag:\n%s", out)
	}
}

// --allow-downgrade runs the steps.
func TestUpgradeApplyAllowDowngradeRunsSteps(t *testing.T) {
	stubRegistry(t)
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true}, nil
	})
	ran := 0
	stubRunStep(t, func() error { ran++; return nil })

	dir := downgradedApp(t)
	var out string
	code := -1
	out = clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunUpgrade([]string{dir, "--to", "v0.85.0", "--apply", "--allow-downgrade"}) })
	})
	// -1 is the capture helper's "no osExit": the CLI's success.
	if code != -1 {
		t.Fatalf("exit = %d, want success (out captured:\n%s)", code, out)
	}
	if !strings.Contains(out, "Upgraded") {
		t.Fatalf("allowed downgrade did not report completion:\n%s", out)
	}
}

// A report whose breaking notes still have hits exits 1, plan and apply
// alike: a script must not read unfinished migration work as success.
func TestUpgradeExitsNonZeroOnRemainingBreakingHits(t *testing.T) {
	note := &upgrade.Note{
		Change: "removed X", Breaking: true,
		Guidance: "Port to Y.",
		Find:     upgrade.Find{Uses: []upgrade.Symbol{{Pkg: "p", Name: "X"}}},
	}
	stubRegistry(t, note)
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{
			TypeChecked: true,
			Hits:        map[*upgrade.Note][]scan.Hit{note: {{File: "a.go", Line: 3, Why: "p.X"}}},
		}, nil
	})
	dir := downgradedApp(t)
	var out string
	code := -1
	out = clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunUpgrade([]string{dir, "--from", "v0.85.0", "--to", "v0.86.0"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 with breaking hits outstanding (out:\n%s)", code, out)
	}
	if !strings.Contains(out, "breaking-change edit hits remain") {
		t.Fatalf("the non-zero exit must say why:\n%s", out)
	}
}

// The gate is the BREAKING notes: a run whose only hits sit on
// non-breaking notes exits 0.
func TestUpgradeExitsZeroWithoutBreakingHits(t *testing.T) {
	note := &upgrade.Note{
		Change:   "Y added",
		Guidance: "Optional.",
		Find:     upgrade.Find{Uses: []upgrade.Symbol{{Pkg: "p", Name: "X"}}},
	}
	stubRegistry(t, note)
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{
			TypeChecked: true,
			Hits:        map[*upgrade.Note][]scan.Hit{note: {{File: "a.go", Line: 3, Why: "p.X"}}},
		}, nil
	})
	dir := downgradedApp(t)
	code := clitest.CovT_capExit(t, func() {
		_ = clitest.CovT_capStdout(t, func() { RunUpgrade([]string{dir, "--from", "v0.85.0", "--to", "v0.86.0"}) })
	})
	if code != -1 {
		t.Fatalf("exit = %d, want no exit (0 for the CLI) with no breaking hit", code)
	}
}

// A hits: review note's hit may already be right (v0.97's well-known
// note names every framework.NewApp call), so it never clears: counting
// it made a finished port exit 1 on every run. Only edit hits count —
// a review note's hit read from a compile error is one, as the report
// files it.
func TestUpgradeReviewHitsLeaveExitZero(t *testing.T) {
	note := &upgrade.Note{
		Change: "default flipped", Breaking: true, Review: true,
		Guidance: "Check the call.",
		Find:     upgrade.Find{Uses: []upgrade.Symbol{{Pkg: "p", Name: "X"}}},
	}
	stubRegistry(t, note)
	hits := []scan.Hit{{File: "a.go", Line: 3, Why: "p.X"}}
	stubScan(t, func(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true, Hits: map[*upgrade.Note][]scan.Hit{note: hits}}, nil
	})
	dir := downgradedApp(t)
	run := func() int {
		return clitest.CovT_capExit(t, func() {
			_ = clitest.CovT_capStdout(t, func() { RunUpgrade([]string{dir, "--from", "v0.85.0", "--to", "v0.86.0"}) })
		})
	}
	if code := run(); code != -1 {
		t.Fatalf("exit = %d, want no exit with only review hits", code)
	}
	hits = []scan.Hit{{File: "a.go", Line: 3, Why: "p.X", Err: "undefined: p.X"}}
	if code := run(); code != 1 {
		t.Fatalf("exit = %d, want 1 for a review note's compile-error hit", code)
	}
}

// stubRunStep replaces the step runner for one test; fn replaces the
// whole exec (no command runs).
func stubRunStep(t *testing.T, fn func() error) {
	t.Helper()
	old := runStep
	runStep = func(name string, args []string, dir string) error {
		return fn()
	}
	t.Cleanup(func() { runStep = old })
}
