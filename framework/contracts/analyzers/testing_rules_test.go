package analyzers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// testingRun is hookFixture generalised to a file map and an explicit
// config: the disabled-test rule needs _test.go files and the coverage
// floor needs a profile plus a configured minimum, neither of which the
// single-source helper can express.
func testingRun(t *testing.T, cfg *contracts.Config, files map[string]string) []contracts.Diagnostic {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.26\n"
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"testing"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("analyzer errors: %v", report.Errors)
	}
	return report.Diagnostics
}

// ----------------------------------------------------------------------
// GOFASTR1103 — entity CRUD not exercised
// ----------------------------------------------------------------------

const entitySource = `package main

import "github.com/DonaldMurillo/gofastr/framework"

func wire(app *framework.App) {
	app.Entity("posts", framework.EntityConfig{})
	app.Entity("orders", framework.EntityConfig{})
}
`

func TestUnexercisedEntityIsReported(t *testing.T) {
	ds := hookFixture(t, entitySource,
		`{"version":1,"routes":{},"entities":{},"hooks":{}}`)
	d := assertHas(t, ds, contracts.RuleEntityNotExercised)
	if !strings.Contains(d.Message, "posts") && !strings.Contains(d.Message, "orders") {
		t.Errorf("message does not name an entity: %q", d.Message)
	}
	if d.Evidence["entity"] == "" {
		t.Errorf("evidence does not carry the entity name: %v", d.Evidence)
	}
}

func TestExercisedEntityIsNotReported(t *testing.T) {
	// One entity covered, one not. The uncovered finding is what proves
	// the analyzer looked at this tree at all — without it, a manifest
	// the analyzer never read would make this test pass vacuously.
	ds := hookFixture(t, entitySource,
		`{"version":1,"routes":{},"entities":{"posts":["create"]},"hooks":{}}`)
	d := assertHas(t, ds, contracts.RuleEntityNotExercised)
	if !strings.Contains(d.Message, "orders") {
		t.Errorf("message does not name the uncovered entity: %q", d.Message)
	}
	for _, other := range ds {
		if other.RuleID == contracts.RuleEntityNotExercised && strings.Contains(other.Message, `"posts"`) {
			t.Error("an entity the manifest records as exercised was reported")
		}
	}
}

// ----------------------------------------------------------------------
// GOFASTR1104 — line-coverage floor
// ----------------------------------------------------------------------

// coverageFloorConfig turns the floor on. MinimumSet is what gates the
// whole check, so a config that only set Minimum would silently test
// nothing.
func coverageFloorConfig(minimum float64) *contracts.Config {
	cfg := contracts.DefaultConfig()
	cfg.Coverage.Minimum = minimum
	cfg.Coverage.MinimumSet = true
	return cfg
}

func TestCoverageBelowFloorIsReported(t *testing.T) {
	// Two of ten statements covered is 20%, statement-weighted the same
	// way `go tool cover -func` weights it.
	ds := testingRun(t, coverageFloorConfig(80), map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"coverage.out": "mode: set\n" +
			"example.com/app/main.go:3.1,5.2 2 1\n" +
			"example.com/app/main.go:7.1,9.2 8 0\n",
	})
	d := assertHas(t, ds, contracts.RuleCoverageBelowMinimum)
	if !strings.Contains(d.Message, "below the configured floor") {
		t.Errorf("message does not state the floor was missed: %q", d.Message)
	}
	if d.Evidence["actual"] != "20.00" || d.Evidence["minimum"] != "80.00" {
		t.Errorf("evidence does not carry the percentages: %v", d.Evidence)
	}
}

func TestCoverageAtFloorIsClean(t *testing.T) {
	// Exactly at the floor must pass — the boundary where an inverted
	// comparison would flip. The debt skip alongside it exists so the
	// clean result is provably "analyzer ran and accepted the profile",
	// not "analyzer never ran": if the testing analyzer were skipped
	// entirely, the missing GOFASTR1105 would fail this test.
	ds := testingRun(t, coverageFloorConfig(80), map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"coverage.out": "mode: set\n" +
			"example.com/app/main.go:3.1,5.2 8 1\n" +
			"example.com/app/main.go:7.1,9.2 2 0\n",
		"skip_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestCheckout(t *testing.T) {\n\tt.Skip(\"temporarily disabled\")\n}\n",
	})
	assertHas(t, ds, contracts.RuleDisabledTest)
	assertNot(t, ds, contracts.RuleCoverageBelowMinimum, "80.0% meets a floor of 80")
}

// ----------------------------------------------------------------------
// GOFASTR1105 — disabled tests
// ----------------------------------------------------------------------

func TestDebtSkipIsReported(t *testing.T) {
	ds := testingRun(t, contracts.DefaultConfig(), map[string]string{
		"checkout_test.go": `package main

import "testing"

func TestCheckout(t *testing.T) {
	t.Skip("temporarily disabled - flaky ordering")
}
`,
	})
	d := assertHas(t, ds, contracts.RuleDisabledTest)
	if !strings.Contains(d.File, "checkout_test.go") {
		t.Errorf("finding does not point at the test file: %q", d.File)
	}
}

func TestLaneBoundarySkipIsNotReported(t *testing.T) {
	// The debt skip in the second file is the proof the rule ran over
	// this tree; only then does the absence of a finding for the lane
	// skip mean the boundary phrases were actually honoured.
	ds := testingRun(t, contracts.DefaultConfig(), map[string]string{
		"postgres_test.go": `package main

import "testing"

func TestAgainstPostgres(t *testing.T) {
	t.Skip("DATABASE_URL not set")
}
`,
		"checkout_test.go": `package main

import "testing"

func TestCheckout(t *testing.T) {
	t.Skip("todo: restore this test")
}
`,
	})
	var disabled []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == contracts.RuleDisabledTest {
			disabled = append(disabled, d)
		}
	}
	if len(disabled) != 1 {
		t.Fatalf("expected exactly the debt skip; got %d findings: %v", len(disabled), rules(ds))
	}
	if !strings.Contains(disabled[0].File, "checkout_test.go") {
		t.Errorf("the lane-boundary skip was reported instead: %q", disabled[0].File)
	}
}

// Entity collection feeds five rules, so an over-broad match is expensive:
// a host app with its own registry type and an `Entity(name, cfg)` method
// got phantom findings from all of them. Every other collector in this
// package guards on the import; this one did not.
func TestForeignEntityMethodIsIgnored(t *testing.T) {
	const foreign = `package main

type Catalog struct{}

type Spec struct{ Name string }

func (c *Catalog) Entity(name string, spec Spec) {}

func wire(c *Catalog) {
	c.Entity("widgets", Spec{Name: "widgets"})
}
`
	ds := testingRun(t, contracts.DefaultConfig(), map[string]string{
		"main.go":                         foreign,
		".gofastr/semantic-coverage.json": `{"version":1,"routes":{},"entities":{},"hooks":{}}`,
	})
	assertNot(t, ds, contracts.RuleEntityNotExercised, "the call is not a GoFastr entity registration")

	// The same shape in a file that DOES import the framework is still
	// collected — the guard must not have turned the rule off wholesale.
	ds = hookFixture(t, entitySource, `{"version":1,"routes":{},"entities":{},"hooks":{}}`)
	assertHas(t, ds, contracts.RuleEntityNotExercised)
}

// The sibling of TestForeignEntityMethodIsIgnored. Hook collection reads
// the entity name from the SECOND argument, which is also the shape of an
// ordinary trigger helper — `OnBeforeCreate(db, "orders", fn)`. Without an
// import guard the two are indistinguishable.
func TestForeignHookFunctionIsIgnored(t *testing.T) {
	const declaring = `package main

import "github.com/DonaldMurillo/gofastr/framework"

func wire(app *framework.App) {
	app.Entity("widgets", framework.EntityConfig{})
}
`
	const foreign = `package main

import "database/sql"

func OnBeforeCreate(db *sql.DB, table string, fn func() error) {}

func setup(db *sql.DB) {
	OnBeforeCreate(db, "widgets", func() error { return nil })
}
`
	manifest := `{"version":1,"routes":{},"entities":{"widgets":["create"]},"hooks":{}}`

	ds := testingRun(t, contracts.DefaultConfig(), map[string]string{
		"main.go":                         declaring,
		"local.go":                        foreign,
		".gofastr/semantic-coverage.json": manifest,
	})
	assertNot(t, ds, contracts.RuleHookNotFired, "a local helper is not a framework hook")

	// The real constructor in a framework-importing file is still read —
	// the guard must narrow the match, not disable the rule.
	ds = testingRun(t, contracts.DefaultConfig(), map[string]string{
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/framework"

func wire(app *framework.App) {
	app.Entity("widgets", framework.EntityConfig{})
	framework.OnBeforeCreate(app, "widgets", nil)
}
`,
		".gofastr/semantic-coverage.json": manifest,
	})
	assertHas(t, ds, contracts.RuleHookNotFired)
}

// Absence and corruption are different failures. A missing manifest is
// normal on a fresh clone and reports at info; a manifest that exists and
// cannot be parsed means every semantic check silently did not run, on
// evidence known to be broken.
//
// This was one rule with an analyzer-set Severity, which Run discards by
// design — so corruption reported at info and the run exited 0 saying
// "Contracts verified".
func TestUnreadableManifestFailsTheRun(t *testing.T) {
	src := "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
		"func a(r *router.Router) { r.Handle(\"GET\", \"/a\", nil) }\n"

	broken := testingRun(t, contracts.DefaultConfig(), map[string]string{
		"main.go":                         src,
		".gofastr/semantic-coverage.json": "{ this is not json",
	})
	d := assertHas(t, broken, contracts.RuleCoverageManifestBroken)
	if d.Severity != contracts.SeverityError {
		t.Errorf("corruption reported at %v, want error — enforcement must not relax when the evidence is broken", d.Severity)
	}
	assertNot(t, broken, contracts.RuleNoCoverageManifest, "the manifest exists; it is unreadable, not absent")

	// Absence keeps its own, gentler rule.
	missing := testingRun(t, contracts.DefaultConfig(), map[string]string{"main.go": src})
	assertHas(t, missing, contracts.RuleNoCoverageManifest)
	assertNot(t, missing, contracts.RuleCoverageManifestBroken, "a missing manifest is not a corrupt one")
}
