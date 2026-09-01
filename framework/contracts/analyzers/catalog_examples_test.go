package analyzers_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// Every rule in the catalog ships a bad/good example pair, and those pairs
// are what a developer (or an agent reading contracts_explain) copies. If
// the analyzers disagree with them, the documentation is worse than none:
// following the Fix produces the finding it was supposed to resolve.
//
// The analyzers parse rather than type-check, so a snippet only has to be
// syntactically valid. Unresolved identifiers and unused imports are
// fine. That is what makes checking the catalog against itself possible.

// exampleFilePrelude gives snippets a package, plausible imports, and
// receivers to reference. None of it needs to type-check.
const exampleFilePrelude = `package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)
`

// snippetNeedsFileScope reports whether a snippet is a declaration, which
// cannot be nested inside a function body.
func snippetNeedsFileScope(s string) bool {
	for _, kw := range []string{"var ", "func ", "type ", "const ", "//", "import "} {
		if strings.HasPrefix(s, kw) {
			return true
		}
	}
	return false
}

func wrapExample(snippet string) string {
	s := strings.TrimSpace(snippet)
	if snippetNeedsFileScope(s) {
		return exampleFilePrelude + "\n" + s + "\n"
	}
	return exampleFilePrelude + `
func example(
	r *router.Router,
	app *framework.App,
	policy *access.Policy,
	db *sql.DB,
	w http.ResponseWriter,
	req *http.Request,
) {
` + s + "\n}\n"
}

// ruleFiresOn reports whether ruleID appears when the analyzers run over a
// tree containing just this snippet.
func ruleFiresOn(t *testing.T, ruleID, snippet string) bool {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.26\n")
	write("main.go", wrapExample(snippet))

	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatalf("%s: NewPass: %v", ruleID, err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatalf("%s: Run: %v", ruleID, err)
	}
	for _, d := range report.Diagnostics {
		if d.RuleID == ruleID {
			return true
		}
	}
	return false
}

// A rule that flags its own documented Good example is self-contradicting:
// the reader follows the Fix and still gets the finding. There are no
// exceptions to this one, and there must never be.
func TestNoRuleFiresOnItsOwnGoodExample(t *testing.T) {
	checked := 0
	for _, r := range contracts.AllRules() {
		for i, ex := range r.Examples {
			if strings.TrimSpace(ex.Good) == "" {
				continue
			}
			checked++
			if ruleFiresOn(t, r.ID, ex.Good) {
				t.Errorf("%s (%s) fires on its own good example #%d — following the documented Fix would not clear the finding:\n%s",
					r.ID, r.Slug, i, ex.Good)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no good examples were checked — the gate is vacuous")
	}
	t.Logf("checked %d good examples", checked)
}

// examplesNeedingMoreContext are rules whose bad example is illustrative
// rather than self-contained: the analyzer needs something a one-file
// snippet cannot express. Each entry says what is missing.
//
// Landing here is NOT an exemption from being tested. It moves the
// obligation to a purpose-built fixture elsewhere.
// [TestExemptedRulesAreTestedElsewhere] enforces that, because this
// comment previously asserted the coverage existed when for four rules it
// did not, which is worse than the gap: it tells a reader not to look.
var examplesNeedingMoreContext = map[string]string{
	contracts.RuleUnguardedMutation:      "needs a route table with no access declaration anywhere in the package",
	contracts.RuleUnscopedPII:            "needs an entity declaration with PII-shaped columns and no scoping",
	contracts.RuleAuthNotWired:           "needs the app's auth wiring to be absent, which one file cannot show",
	contracts.RuleLayerViolation:         "needs a module path and a multi-package layout",
	contracts.RuleQueryInLoop:            "needs a loop whose body queries with the loop variable, across statements",
	contracts.RuleDisabledTest:           "needs a _test.go file, not application source",
	contracts.RuleHookNotFired:           "needs a semantic-coverage manifest to compare against",
	contracts.RuleEventNotEmitted:        "needs a semantic-coverage manifest to compare against",
	contracts.RuleRoleNotExercised:       "needs a semantic-coverage manifest to compare against",
	contracts.RuleHandrolledCRUD:         "needs an entity declaration to contrast the hand-rolled handler with",
	contracts.RuleUntestedRoute:          "needs a module that has test files, so a route can be absent from them",
	contracts.RuleForbiddenImport:        "needs a contracts.architecture.forbid entry to violate",
	contracts.RuleRouteNotExercised:      "needs a semantic-coverage manifest to compare against",
	contracts.RulePermissionNotExercised: "needs a semantic-coverage manifest to compare against",
	contracts.RuleEntityNotExercised:     "needs a semantic-coverage manifest to compare against",
	contracts.RuleCoverageBelowMinimum:   "reads a coverage profile and a configured floor, neither of which is Go source",
	contracts.RuleCoverageManifestBroken: "its example is a corrupt JSON manifest, not Go source, so no snippet can carry it",
	contracts.RuleNoCoverageManifest:     "fires on the ABSENCE of a manifest; its example is the absence, not a snippet",
	contracts.RuleHandrolledBattery:      "needs the hand-rolled subsystem's imports, which a snippet does not carry",
	contracts.RuleRawSQLOverRepo:         "needs an entity declaration whose table the raw query targets",
	contracts.RuleUnknownThemeToken:      "fires on a .css file; its example is CSS, which no Go snippet can carry", // not-a-secret: a rule id, flagged only because the constant name ends in "Token"
	contracts.RuleHardcodedTokenValue:    "fires only inside the design-system trees (core-ui/, framework/ui/, …); a snippet at an app's root is an app surface, where GOFASTR1801 already reports any CSS at all",
}

// The other half: a rule whose bad example does NOT produce it has either
// drifted from the analyzer or stopped working. Both are silent failures.
// The rule keeps appearing in `--list` and never fires again.
func TestEveryRuleFiresOnItsOwnBadExample(t *testing.T) {
	var covered, skipped []string
	for _, r := range contracts.AllRules() {
		if len(r.Examples) == 0 || strings.TrimSpace(r.Examples[0].Bad) == "" {
			continue
		}
		if why, ok := examplesNeedingMoreContext[r.ID]; ok {
			// Guard the exemption itself: if the example starts firing,
			// the entry is stale and should be deleted.
			if ruleFiresOn(t, r.ID, r.Examples[0].Bad) {
				t.Errorf("%s is listed as needing more context (%q) but its bad example now fires — remove the entry", r.ID, why)
			}
			skipped = append(skipped, r.ID)
			continue
		}
		if !ruleFiresOn(t, r.ID, r.Examples[0].Bad) {
			t.Errorf("%s (%s) does not fire on its own bad example — the rule and its documentation disagree:\n%s",
				r.ID, r.Slug, r.Examples[0].Bad)
			continue
		}
		covered = append(covered, r.ID)
	}
	sort.Strings(covered)
	sort.Strings(skipped)
	if len(covered) == 0 {
		t.Fatal("no bad examples fired — the gate is vacuous")
	}
	t.Logf("%d rules proven by their own example; %d need a purpose-built fixture: %s",
		len(covered), len(skipped), strings.Join(skipped, " "))
}

// A rule nothing can emit is documentation pretending to be a check.
//
// This lives here rather than in package contracts because the catalog
// deliberately does not import its analyzers, so the same assertion over
// there could only ever guard on an empty set and skip. It did, silently,
// for every run.
func TestEveryCatalogRuleHasAnAnalyzer(t *testing.T) {
	claimed := map[string]bool{}
	for _, a := range contracts.Analyzers() {
		for _, id := range a.Rules {
			claimed[id] = true
		}
	}
	if len(claimed) == 0 {
		t.Fatal("no analyzers registered — the gate would be vacuous")
	}
	for _, r := range contracts.AllRules() {
		// Meta rules are emitted by the suppression pass rather than by
		// an analyzer, so they are the one exempt class.
		if r.Capability == contracts.CapMeta || claimed[r.ID] {
			continue
		}
		t.Errorf("%s (%s) is in the catalog but no analyzer emits it", r.ID, r.Slug)
	}
}

// The converse: an analyzer claiming a rule that is not in the catalog
// would emit diagnostics that `--explain` cannot describe.
func TestEveryAnalyzerRuleIsInTheCatalog(t *testing.T) {
	for _, a := range contracts.Analyzers() {
		for _, id := range a.Rules {
			if _, ok := contracts.LookupRule(id); !ok {
				t.Errorf("analyzer %q claims %s, which is not in the catalog", a.Name, id)
			}
		}
	}
}

// GOFASTR1805 reuses core-ui/check via a per-file entry point rather than
// the recursive walk, so the two skip rules have to be verified here: a
// file opting out with //check-csp:ignore-file is skipped, and a script
// with a src is not a finding.
func TestInlineScriptRuleHonoursItsSkips(t *testing.T) {
	fires := func(t *testing.T, body string) bool {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ui.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"rendering"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range report.Diagnostics {
			if d.RuleID == contracts.RuleInlineScript {
				return true
			}
		}
		return false
	}

	const inline = "package main\n\nfunc bad() string { return `<script>alert(1)</script>` }\n"
	if !fires(t, inline) {
		t.Error("an inline script body was not reported")
	}
	if fires(t, "//check-csp:ignore-file\n"+inline) {
		t.Error("//check-csp:ignore-file did not exempt the file")
	}
	const external = "package main\n\nfunc good() string { return `<script src=\"/s.js\" defer></script>` }\n"
	if fires(t, external) {
		t.Error("a script with a src was reported")
	}
}

// The rendering analyzer pre-filters lines on a few bytes before running
// its regexes. Each guard is meant to be a strict superset of what its
// regex can match; this pins the shapes where that is least obvious:
// a hard navigation with no colon, an EventSource with no colon, and an
// at-rule whose only trigger byte is '@'.
func TestRenderingPreFilterDropsNothing(t *testing.T) {
	cases := []struct {
		name, line, rule string
	}{
		{"hard nav, no colon", "func go() { window.location.reload() }", contracts.RuleHardNavigation},
		{"hard nav assignment", "func go() { location.href = `/orders` }", contracts.RuleHardNavigation},
		{"event source, no colon", "var s = `new EventSource('/x')`", contracts.RuleBespokeEventSource},
		{"at-rule", "var css = `@media (min-width: 40rem) { .a { color: red } }`", contracts.RuleBespokeCSS},
		{"style tag", "var s = `<style>.a{color:red}</style>`", contracts.RuleBespokeCSS},
		{"inline style attr", "var s = `<div style=\"margin-top: 4px\"></div>`", contracts.RuleInlineStyle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			src := "package main\n\n" + c.line + "\n"
			if err := os.WriteFile(filepath.Join(dir, "ui.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			report, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"rendering"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range report.Diagnostics {
				if d.RuleID == c.rule {
					return
				}
			}
			t.Errorf("%s was not reported for:\n%s", c.rule, c.line)
		})
	}
}

// A rule exempted from the bad-example gate has to earn that exemption by
// being tested some other way. Without this, adding an entry to
// examplesNeedingMoreContext is a way to make a rule untested and have the
// suite go quiet about it.
func TestExemptedRulesAreTestedElsewhere(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("cannot locate the module root: %v", err)
	}
	var body strings.Builder
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// This file is the gate itself; a rule named only here is not
		// tested, it is merely exempted.
		if filepath.Base(path) == "catalog_examples_test.go" {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr == nil {
			body.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if body.Len() == 0 {
		t.Fatal("no test sources read — the gate would be vacuous")
	}
	all := body.String()

	constants, err := ruleConstantNames(root)
	if err != nil {
		t.Fatalf("read rule constants: %v", err)
	}
	if len(constants) == 0 {
		t.Fatal("no rule constants parsed — the gate would misreport every rule as untested")
	}

	for id, why := range examplesNeedingMoreContext {
		rule, ok := contracts.LookupRule(id)
		if !ok {
			t.Errorf("%s is exempted but not in the catalog", id)
			continue
		}
		// A test may name a rule by ID, by slug, or, most often, by its
		// Go constant, which shares neither substring with the ID. Missing
		// the constant form would report well-tested rules as untested and
		// train a reader to ignore this gate.
		if strings.Contains(all, id) || strings.Contains(all, rule.Slug) ||
			(constants[id] != "" && strings.Contains(all, constants[id])) {
			continue
		}
		t.Errorf("%s (%s) is exempted from the example gate (%q) but no other test references it — "+
			"it is simply untested", id, rule.Slug, why)
	}
}

// repoRoot walks up from the working directory to the enclosing module.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fs.ErrNotExist
		}
		dir = parent
	}
}

// reRuleConstant matches the catalog's `RuleName = "GOFASTR1234"` lines.
var reRuleConstant = regexp.MustCompile(`(Rule[A-Za-z0-9]+)\s*=\s*"(GOFASTR\d+)"`)

// ruleConstantNames maps each rule ID to the Go constant that names it, so
// the gate can recognise a test that refers to a rule the usual way.
func ruleConstantNames(root string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(root, "framework", "contracts", "catalog.go"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range reRuleConstant.FindAllStringSubmatch(string(body), -1) {
		out[m[2]] = m[1]
	}
	return out, nil
}
