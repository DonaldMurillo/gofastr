package contracts

import (
	"strings"
	"sync"
	"testing"
)

// sweepPinnedRuleIDs is every catalog ID with the slug it was issued under.
var sweepPinnedRuleIDs = map[string]string{
	"GOFASTR0001": "meta/suppression-without-reason",
	"GOFASTR0002": "meta/stale-suppression",
	"GOFASTR0003": "meta/unknown-suppressed-rule",
	"GOFASTR0004": "meta/malformed-suppression",
	"GOFASTR1001": "routing/duplicate-route",
	"GOFASTR1002": "routing/colon-path-parameter",
	"GOFASTR1003": "routing/untested-route",
	"GOFASTR1004": "routing/in-page-state-as-route",
	"GOFASTR1005": "routing/non-uppercase-method",
	"GOFASTR1006": "routing/prefix-without-segment-boundary",
	"GOFASTR1101": "testing/route-not-exercised",
	"GOFASTR1102": "testing/permission-not-exercised",
	"GOFASTR1103": "testing/entity-crud-not-exercised",
	"GOFASTR1104": "testing/coverage-below-minimum",
	"GOFASTR1105": "testing/disabled-test",
	"GOFASTR1106": "testing/no-coverage-manifest",
	"GOFASTR1107": "testing/hook-not-fired",
	"GOFASTR1108": "testing/event-subscriber-not-exercised",
	"GOFASTR1109": "testing/role-not-exercised",
	"GOFASTR1110": "testing/coverage-manifest-unreadable",
	"GOFASTR1201": "accessibility/missing-alt",
	"GOFASTR1202": "accessibility/missing-accessible-name",
	"GOFASTR1203": "accessibility/unnamed-landmark",
	"GOFASTR1204": "accessibility/incomplete-form-control",
	"GOFASTR1205": "accessibility/implicit-heading-level",
	"GOFASTR1206": "accessibility/missing-element-metadata",
	"GOFASTR1301": "architecture/layer-violation",
	"GOFASTR1302": "architecture/forbidden-import",
	"GOFASTR1401": "security/sql-string-concat",
	"GOFASTR1402": "security/form-without-csrf",
	"GOFASTR1403": "security/html-concat",
	"GOFASTR1404": "security/insecure-cookie",
	"GOFASTR1405": "security/hardcoded-secret",
	"GOFASTR1406": "security/forwarded-proto-without-enum",
	"GOFASTR1407": "security/raw-json-body-decode",
	"GOFASTR1408": "security/absolute-attempts-counter",
	"GOFASTR1409": "security/unfenced-claim-write",
	"GOFASTR1410": "security/folded-key-save",
	"GOFASTR1411": "security/fetch-metadata-copy",
	"GOFASTR1412": "security/url-attr-html-escape",
	"GOFASTR1413": "security/vary-set-overwrites",
	"GOFASTR1414": "security/dialect-twin-where-drift",
	"GOFASTR1415": "security/erase-export-coverage",
	"GOFASTR1416": "security/inline-ddl-constraint",
	"GOFASTR1417": "security/status-update-skips-stamps",
	"GOFASTR1418": "security/nullable-primary-key",
	"GOFASTR1501": "performance/regexp-compile-per-call",
	"GOFASTR1502": "performance/query-in-loop",
	"GOFASTR1503": "performance/reflection-per-request",
	"GOFASTR1601": "data/ignored-exec",
	"GOFASTR1701": "entities/mcp-without-crud",
	"GOFASTR1702": "entities/public-entity",
	"GOFASTR1703": "entities/crud-without-auth",
	"GOFASTR1801": "rendering/bespoke-css",
	"GOFASTR1802": "rendering/hard-navigation",
	"GOFASTR1803": "rendering/bespoke-event-source",
	"GOFASTR1804": "rendering/inline-style",
	"GOFASTR1805": "rendering/inline-script",
	"GOFASTR1806": "rendering/unknown-theme-token",
	"GOFASTR1807": "rendering/hardcoded-token-value",
	"GOFASTR1808": "rendering/fallback-drift",
	"GOFASTR1809": "rendering/ownerless-stylesheet",
	"GOFASTR1810": "rendering/kit-class-selector",
	"GOFASTR1811": "rendering/important",
	"GOFASTR1812": "rendering/raw-media-width",
	"GOFASTR1813": "rendering/animation-without-reduced-motion",
	"GOFASTR1814": "rendering/stale-style-source",
	"GOFASTR1815": "rendering/upstream-candidate",
	"GOFASTR1816": "rendering/duplicate-style-name",
	"GOFASTR1817": "rendering/kit-root-restyled",
	"GOFASTR1818": "rendering/owned-style-outside-owner",
	"GOFASTR1819": "rendering/app-sheet-non-class",
	"GOFASTR1820": "rendering/theme-token-redeclared",
	"GOFASTR1821": "rendering/duplicate-token-value",
	"GOFASTR1822": "rendering/repeated-literal",
	"GOFASTR1823": "rendering/bare-theme-literal",
	"GOFASTR1824": "rendering/physical-side",
	"GOFASTR1825": "rendering/owned-statement-at-rule",
	"GOFASTR1826": "rendering/flex-reverse",
	"GOFASTR1901": "permissions/unscoped-pii",
	"GOFASTR1902": "permissions/unguarded-mutation",
	"GOFASTR1903": "permissions/auth-not-wired",
	"GOFASTR1904": "permissions/unregistered-permission",
	"GOFASTR2001": "ai/handrolled-crud",
	"GOFASTR2002": "ai/handrolled-battery",
	"GOFASTR2003": "ai/raw-sql-over-repository",
}

// sweepRetiredRuleIDs holds deleted IDs; none may be issued again.
var sweepRetiredRuleIDs = map[string]bool{}

func TestSweepEngineRuleIDsAreNeverRecycled(t *testing.T) {
	for id, slug := range sweepPinnedRuleIDs {
		if sweepRetiredRuleIDs[id] {
			if _, ok := LookupRule(id); ok {
				t.Errorf("retired rule ID %s is in the catalog again", id)
			}
			continue
		}
		r, ok := LookupRule(id)
		if !ok {
			t.Errorf("rule %s (%s) left the catalog: move it to sweepRetiredRuleIDs, never reuse the number", id, slug)
			continue
		}
		if r.Slug != slug {
			t.Errorf("rule ID %s now names %q, it was issued for %q", id, r.Slug, slug)
		}
	}
	for _, r := range AllRules() {
		if !strings.HasPrefix(r.ID, "GOFASTR") {
			continue
		}
		if _, pinned := sweepPinnedRuleIDs[r.ID]; !pinned && sweepRetiredRuleIDs[r.ID] {
			t.Errorf("rule %s reuses a retired ID", r.ID)
		}
	}
}

func sweepValidRule(id string) Rule {
	return Rule{
		ID: id, Slug: "data/sweep-" + strings.ToLower(id), Title: "t", Capability: CapData,
		Severity: SeverityWarn, Summary: "s", Why: "w", Fix: "f", Doc: "entity-declarations",
		Examples: []Example{{Bad: "b", Good: "g"}},
	}
}

func TestSweepEngineRegisterRulesRequiresEveryDocField(t *testing.T) {
	mutations := map[string]func(*Rule){
		"Title":       func(r *Rule) { r.Title = " " },
		"Summary":     func(r *Rule) { r.Summary = "" },
		"Why":         func(r *Rule) { r.Why = "" },
		"Fix":         func(r *Rule) { r.Fix = "" },
		"Doc":         func(r *Rule) { r.Doc = "" },
		"Examples":    func(r *Rule) { r.Examples = nil },
		"Example.Bad": func(r *Rule) { r.Examples = []Example{{Good: "g"}} },
		"SeverityOff": func(r *Rule) { r.Severity = SeverityOff },
	}
	for name, mutate := range mutations {
		r := sweepValidRule("SWEEPA101")
		mutate(&r)
		if err := validateRule(r); err == nil {
			t.Errorf("a rule with a bad %s was accepted", name)
		}
	}
	if err := validateRule(sweepValidRule("SWEEPA102")); err != nil {
		t.Fatalf("the valid control rule was refused: %v", err)
	}
}

func TestSweepEngineRegisterRejectsAnUndeclaredCatalogRule(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an analyzer declaring a rule the catalog lacks was registered")
		}
	}()
	Register(&Analyzer{
		Name: "sweep-unknown-rule", Rules: []string{"SWEEPZ999"},
		Run: func(*Pass) ([]Diagnostic, error) { return nil, nil },
	})
}

var sweepClaimOnce sync.Once

func TestSweepEngineRunTakesSeverityFromTheCatalog(t *testing.T) {
	sweepClaimOnce.Do(func() {
		Register(&Analyzer{
			Name: "sweep-claim", Rules: []string{RuleHTMLConcat},
			Run: func(*Pass) ([]Diagnostic, error) {
				return []Diagnostic{{
					RuleID: RuleHTMLConcat, File: "a.go", Line: 1, Message: "claimed",
					Severity: SeverityInfo, Slug: "made/up", Capability: CapAI,
				}}, nil
			},
		})
	})
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	report, err := Run(mustPass(t, dir, DefaultConfig()), RunOptions{Analyzers: []string{"sweep-claim"}})
	if err != nil {
		t.Fatal(err)
	}
	rule, _ := LookupRule(RuleHTMLConcat)
	if len(report.Diagnostics) != 1 {
		t.Fatalf("got %d diagnostics", len(report.Diagnostics))
	}
	d := report.Diagnostics[0]
	if d.Severity != rule.Severity || d.Slug != rule.Slug || d.Capability != rule.Capability {
		t.Errorf("analyzer-claimed fields survived Run: %+v", d)
	}
	if d.Suggestion != rule.Fix || d.Rule == nil || d.Rule.ID != rule.ID {
		t.Errorf("Run did not attach the rule and its Fix: %+v", d)
	}
}

func TestSweepEngineMinifiedScriptsAreSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "app.js", "let a = 1;\n")
	writeTemp(t, dir, "vendor.min.js", "let a = 1;\n")
	writeTemp(t, dir, "bundle.js", "let a = 1;"+strings.Repeat(" ", 1001)+"\n")
	writeTemp(t, dir, "x.go", "package x\n")
	var got []string
	for _, f := range mustPass(t, dir, DefaultConfig()).ScriptFiles() {
		got = append(got, f.Rel)
	}
	if strings.Join(got, ",") != "app.js" {
		t.Errorf("ScriptFiles = %v, want only app.js", got)
	}
}

func TestSweepEngineSkipsBuildAndDependencyTreesButNotTestdata(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"vendor", "node_modules", "dist", "bin", "build", "tmp", "testdata"} {
		writeTemp(t, dir, d+"/x.go", "package x\n")
	}
	var got []string
	for _, f := range mustPass(t, dir, DefaultConfig()).Files() {
		got = append(got, f.Rel)
	}
	if strings.Join(got, ",") != "testdata/x.go" {
		t.Errorf("Files = %v, want only testdata/x.go", got)
	}
}

func TestSweepEngineNarrowedRunKeepsOtherSuppressionsQuiet(t *testing.T) {
	report := tripRun(t, "package a\n\n//gofastr:allow(GOFASTR1003) covered by an e2e suite\nfunc f() {}\n")
	for _, d := range report.Diagnostics {
		if d.RuleID == RuleSuppressionStale {
			t.Fatalf("a suppression for a rule that did not run was reported stale: %+v", d)
		}
	}
}

func TestSweepEngineLineWaiverOfAStaleDirectiveIsNotItselfStale(t *testing.T) {
	report := tripRun(t, "package a\n\n//gofastr:allow(GOFASTR0002) kept for the upstream copy\n//gofastr:allow(GOFASTR1403) no longer trips\nfunc f() {}\n")
	for _, d := range report.Diagnostics {
		if d.RuleID == RuleSuppressionStale {
			t.Errorf("stale finding survived a line waiver: %+v", d)
		}
	}
	if report.Suppressed != 1 {
		t.Errorf("Suppressed = %d, want 1", report.Suppressed)
	}
}

var sweepTwinOnce sync.Once

func TestSweepEngineRunReportsAConclusionTwoAnalyzersShareOnce(t *testing.T) {
	sweepTwinOnce.Do(func() {
		for _, name := range []string{"sweep-twin-a", "sweep-twin-b"} {
			Register(&Analyzer{
				Name: name, Rules: []string{RuleHTMLConcat},
				Run: func(*Pass) ([]Diagnostic, error) {
					return []Diagnostic{{RuleID: RuleHTMLConcat, File: "a.go", Line: 1, Message: "same"}}, nil
				},
			})
		}
	})
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	report, err := Run(mustPass(t, dir, DefaultConfig()), RunOptions{Analyzers: []string{"sweep-twin-a", "sweep-twin-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 {
		t.Errorf("got %d diagnostics, want the shared one once", len(report.Diagnostics))
	}
}

func TestSweepEngineSuggestRulesKeepsTheDistanceRanking(t *testing.T) {
	got := SuggestRules("gofastr2x03")
	if len(got) == 0 || !strings.HasPrefix(got[0], RuleRawSQLOverRepo+" ") {
		t.Errorf("closest match is not first: %v", got)
	}
}

func TestSweepEngineSuggestRulesCapsTheEditBudgetAtThree(t *testing.T) {
	for _, s := range SuggestRules("routing/unxxxxed-route") {
		if strings.Contains(s, "routing/untested-route") {
			t.Errorf("a four-edit guess was suggested: %v", s)
		}
	}
}

func TestSweepEngineGoAccessorsNeverHandOutClientFiles(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	writeTemp(t, dir, "app.js", "let a = 1;\n")
	writeTemp(t, dir, "app.css", "body { margin: 0 }\n")
	p := mustPass(t, dir, DefaultConfig())
	for _, set := range [][]SourceFile{p.Files(), p.AppFiles(), p.TestFiles()} {
		for _, f := range set {
			if f.IsScript || f.IsCSS {
				t.Errorf("a Go accessor handed out %s", f.Rel)
			}
		}
	}
	for _, rel := range []string{"app.js", "app.css"} {
		if _, ok := p.AST(rel); ok {
			t.Errorf("%s parsed as Go", rel)
		}
	}
	if u := p.Unparsed(); len(u) != 0 {
		t.Errorf("client files recorded as unparsed: %v", u)
	}
}

func TestSweepEngineMemoKeepsTheFirstStoredValue(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	p := mustPass(t, dir, DefaultConfig())
	var entered sync.WaitGroup
	entered.Add(2)
	got := make([]any, 2)
	var done sync.WaitGroup
	for i := range got {
		done.Add(1)
		go func() {
			defer done.Done()
			got[i] = p.Memo("k", func() any { entered.Done(); entered.Wait(); return new(int) })
		}()
	}
	done.Wait()
	if got[0] != got[1] {
		t.Error("concurrent Memo callers saw different values")
	}
}

func TestSweepEngineRunAppliesTheConfiguredSeverity(t *testing.T) {
	registerTripAnalyzer()
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.settings.yml", "contracts:\n  rules:\n    GOFASTR1403: info\n")
	writeTemp(t, dir, "a.go", "package a\n\nfunc f() { trip() }\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"test-trip"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Severity != SeverityInfo {
		t.Errorf("configured severity not applied: %+v", report.Diagnostics)
	}
}
