package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ----------------------------------------------------------------------
// Catalog invariants
// ----------------------------------------------------------------------

func TestCatalogRulesAreWellFormed(t *testing.T) {
	rules := AllRules()
	if len(rules) < 30 {
		t.Fatalf("catalog has only %d rules", len(rules))
	}
	for _, r := range rules {
		if err := validateRule(r); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		// Why and Fix are the whole point of the catalog; a one-word
		// placeholder passes validateRule but teaches nothing.
		if len(r.Why) < 40 {
			t.Errorf("%s: Why is too short to explain a consequence: %q", r.ID, r.Why)
		}
		if len(r.Fix) < 20 {
			t.Errorf("%s: Fix is too short to name a remedy: %q", r.ID, r.Fix)
		}
	}
}

func TestLookupRuleAcceptsIDAndSlug(t *testing.T) {
	byID, ok := LookupRule(RuleColonPathParam)
	if !ok {
		t.Fatal("lookup by ID failed")
	}
	bySlug, ok := LookupRule("routing/colon-path-parameter")
	if !ok {
		t.Fatal("lookup by slug failed")
	}
	if byID.ID != bySlug.ID {
		t.Fatalf("id %s != slug %s", byID.ID, bySlug.ID)
	}
	if _, ok := LookupRule("gofastr9999"); ok {
		t.Fatal("unknown rule resolved")
	}
}

// ----------------------------------------------------------------------
// Glob matching
// ----------------------------------------------------------------------

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"cmd/**", "cmd/gofastr/main.go", true},
		{"cmd/**", "internal/cmd/x.go", false},
		{"**/testdata/**", "a/b/testdata/c/d.go", true},
		{"**/testdata/**", "a/b/test/c.go", false},
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false},
		{"**/*.go", "cmd/main.go", true},
		{"testdata", "a/testdata/b.go", true},
		{"testdata", "a/testdatax/b.go", false},
		{"cmd/", "cmd/a/b.go", true},
		// `**` matches zero segments, so the pattern covers the tree root
		// as well as its contents. See matchGlob's doc comment.
		{"core/**", "core", true},
		{"core/**", "core/router/x.go", true},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/x/c", false},
		{"", "anything", false},
		// A Windows user typing a native path gets a working glob rather
		// than an exemption that looks applied and silently matches
		// nothing. Every path this matcher sees is slash-separated, so a
		// backslash can only ever have been meant as a separator.
		{`cmd\**`, "cmd/gofastr/main.go", true},
		{`examples\site\styles.go`, "examples/site/styles.go", true},
		{`cmd\**`, "internal/x.go", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// ----------------------------------------------------------------------
// Config
// ----------------------------------------------------------------------

func writeTemp(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultConfigEnforcesEverything(t *testing.T) {
	cfg := DefaultConfig()
	for _, r := range AllRules() {
		if got := cfg.SeverityFor(r); got != r.Severity {
			t.Errorf("%s: default severity %v, want declared %v", r.ID, got, r.Severity)
		}
		if !cfg.Enabled(r) {
			t.Errorf("%s: disabled by default", r.ID)
		}
	}
	if cfg.FailOn != SeverityError {
		t.Errorf("FailOn = %v, want error", cfg.FailOn)
	}
}

func TestConfigRelaxations(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", `
contracts:
  exempt:
    - "vendorized/**"
  performance: warn
  rules:
    GOFASTR1003: "off"
    security/html-concat:
      severity: warn
      exempt: ["templates/**"]
  coverage:
    minimum: 85
    routes: false
`)
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	perf, _ := LookupRule(RuleRegexpCompilePerCall)
	if got := cfg.SeverityFor(perf); got != SeverityWarn {
		t.Errorf("capability downgrade not applied: %v", got)
	}
	untested, _ := LookupRule(RuleUntestedRoute)
	if cfg.Enabled(untested) {
		t.Error("GOFASTR1003 should be off")
	}
	htmlConcat, _ := LookupRule(RuleHTMLConcat)
	if got := cfg.SeverityFor(htmlConcat); got != SeverityWarn {
		t.Errorf("rule downgrade by slug not applied: %v", got)
	}
	if !cfg.ExemptFor(htmlConcat, "templates/page.go") {
		t.Error("rule-level exempt not applied")
	}
	if cfg.ExemptFor(perf, "templates/page.go") {
		t.Error("rule-level exempt leaked to another rule")
	}
	if !cfg.ExemptPath("vendorized/x.go") {
		t.Error("global exempt not applied")
	}
	if !cfg.Coverage.MinimumSet || cfg.Coverage.Minimum != 85 {
		t.Errorf("coverage minimum = %v (set=%v)", cfg.Coverage.Minimum, cfg.Coverage.MinimumSet)
	}
	if cfg.Coverage.Routes {
		t.Error("coverage.routes should be off")
	}
	// Relaxations must be reported, not silently honoured.
	if len(cfg.Relaxations()) < 4 {
		t.Errorf("Relaxations() = %v, expected every downgrade listed", cfg.Relaxations())
	}
}

// "Visible opt-outs" is the config's whole posture, and the footer's job
// is "what did config change". Per-rule and per-capability exemptions
// were the two relaxation forms the list omitted, an exempt path is a
// place a rule cannot fire, which is exactly what the footer exists to
// say out loud.
func TestRelaxationsListEveryExemptionForm(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", `exempt:
  - "vendor/**"
rules:
  GOFASTR1403:
    exempt: ["gen/**"]
security:
  exempt: ["legacy/**"]
`)
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.Relaxations(), "\n")
	for _, want := range []string{"vendor/**", "GOFASTR1403", "gen/**", "security", "legacy/**"} {
		if !strings.Contains(got, want) {
			t.Errorf("relaxations omit %q:\n%s", want, got)
		}
	}
}

func TestConfigRejectsUnknownRule(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", "contracts:\n  rules:\n    GOFASTR9999: off\n")
	_, err := LoadConfig(dir, "")
	if err == nil {
		t.Fatal("unknown rule accepted — a typo would silently enforce the rule it meant to disable")
	}
	if !strings.Contains(err.Error(), "GOFASTR9999") {
		t.Errorf("error does not name the rule: %v", err)
	}
}

func TestConfigIgnoresBlueprintWithoutContractsBlock(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.yml", "app:\n  name: demo\nentities:\n  - name: posts\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatalf("a blueprint with no contracts block must not be an error: %v", err)
	}
	if cfg.Path != "" {
		t.Errorf("claimed %q as a contracts config", cfg.Path)
	}
}

func TestConfigReadsContractsBlockInBlueprint(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.yml", "app:\n  name: demo\ncontracts:\n  performance: \"off\"\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	perf, _ := LookupRule(RuleRegexpCompilePerCall)
	if cfg.Enabled(perf) {
		t.Error("contracts block inside gofastr.yml not applied")
	}
}

func TestMalformedConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", "contracts:\n  rules:\n    GOFASTR1003: banana\n")
	if _, err := LoadConfig(dir, ""); err == nil {
		t.Fatal("a bad severity silently fell back to defaults — the relaxation would look applied")
	}
}

// ----------------------------------------------------------------------
// Suppression
// ----------------------------------------------------------------------

// probe is a throwaway analyzer that reports one diagnostic per line
// matching a marker, so suppression can be tested without depending on
// any real rule's detection logic.
func probePass(t *testing.T, files map[string]string) (*Pass, *suppressionSet) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeTemp(t, dir, name, body)
	}
	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return p, collectSuppressions(p)
}

func TestSuppressionCoversItsLine(t *testing.T) {
	p, sup := probePass(t, map[string]string{
		"a.go": `package a

func f() {
	trip() //gofastr:allow(GOFASTR1403) trailing form
	trip()
}

//gofastr:allow(GOFASTR1403) standalone form covers the next code line
func g() { trip() }
`,
	})
	rule, _ := LookupRule(RuleHTMLConcat)

	if !sup.suppressed(Diagnostic{File: "a.go", Line: 4}, rule) {
		t.Error("trailing directive did not cover its own line")
	}
	if sup.suppressed(Diagnostic{File: "a.go", Line: 5}, rule) {
		t.Error("directive leaked to the following line")
	}
	if !sup.suppressed(Diagnostic{File: "a.go", Line: 9}, rule) {
		t.Error("standalone directive did not cover the next code line")
	}
	_ = p
}

// A block-comment directive with code after it on the same line trails
// that code. It does not cover the next line. Classifying any line that
// STARTS with a comment as standalone waived the wrong line and then
// reported the directive stale at the very line where its match sits.
func TestLeadingBlockDirectiveCoversItsOwnLine(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\nfunc f() {\n\t/*gofastr:allow(GOFASTR1403) reviewed*/ trip()\n}\n",
	})
	rule, _ := LookupRule(RuleHTMLConcat)
	if !sup.suppressed(Diagnostic{File: "a.go", Line: 4}, rule) {
		t.Error("a leading block directive did not cover the code on its own line")
	}
	if sup.suppressed(Diagnostic{File: "a.go", Line: 5}, rule) {
		t.Error("the directive leaked to the following line")
	}
}

func TestSuppressionInStringLiteralIsNotLive(t *testing.T) {
	// The regression that made this rule necessary: documenting the
	// directive must not disable the rule being documented.
	_, sup := probePass(t, map[string]string{
		"doc.go": `package a

// Suppress it with ` + "`" + `//gofastr:allow(GOFASTR1403) reason` + "`" + ` on the line.
const example = "//gofastr:allow(GOFASTR1403) inside a string"

func f() { trip() }
`,
	})
	rule, _ := LookupRule(RuleHTMLConcat)
	for line := 1; line <= 6; line++ {
		if sup.suppressed(Diagnostic{File: "doc.go", Line: line}, rule) {
			t.Fatalf("line %d suppressed by a quoted directive", line)
		}
	}
	if len(sup.issues) != 0 {
		t.Errorf("quoted directives produced meta findings: %+v", sup.issues)
	}
}

func TestSuppressionRequiresAReason(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\nfunc f() { g() } //gofastr:allow(GOFASTR1403)\n",
	})
	if len(sup.issues) != 1 || sup.issues[0].RuleID != RuleSuppressionNoReason {
		t.Fatalf("expected a no-reason finding, got %+v", sup.issues)
	}
}

func TestSuppressionRejectsCatchAll(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow(all) blanket\nfunc f() {}\n",
	})
	if len(sup.issues) != 1 || sup.issues[0].RuleID != RuleSuppressionMalformed {
		t.Fatalf("expected a malformed finding for allow(all), got %+v", sup.issues)
	}
}

func TestSuppressionRejectsUnknownRule(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow(GOFASTR7777) typo\nfunc f() {}\n",
	})
	if len(sup.issues) != 1 || sup.issues[0].RuleID != RuleSuppressionUnknownRule {
		t.Fatalf("expected an unknown-rule finding, got %+v", sup.issues)
	}
}

// TestSuppressionAcceptsAnalyzerName: a marker naming a repo analyzer
// (//gofastr:allow(mapwriter) …) belongs to the vettool's allow
// filter, so the contracts pipeline neither reports it unknown nor
// counts it stale when no contracts rule matches it.
func TestSuppressionAcceptsAnalyzerName(t *testing.T) {
	p, sup := probePass(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow(mapwriter) one-entry map, order is fixed\nfunc f() {}\n",
	})
	if len(sup.issues) != 0 {
		t.Fatalf("analyzer marker reported as a suppression issue: %+v", sup.issues)
	}
	if st := sup.stale(p); len(st) != 0 {
		t.Fatalf("analyzer marker reported stale: %+v", st)
	}
}

func TestFileScopedSuppression(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "//gofastr:allow-file(GOFASTR1403) whole file is a fixture\npackage a\n\nfunc f() {}\nfunc g() {}\n",
	})
	rule, _ := LookupRule(RuleHTMLConcat)
	for _, line := range []int{1, 4, 5, 99} {
		if !sup.suppressed(Diagnostic{File: "a.go", Line: line}, rule) {
			t.Errorf("file-scoped directive did not cover line %d", line)
		}
	}
}

// ----------------------------------------------------------------------
// Run / Report
// ----------------------------------------------------------------------

func TestRunRejectsUndeclaredRule(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// An analyzer that emits a rule it did not declare must have the
	// finding dropped and the breach reported. Otherwise the catalog
	// stops describing what can actually fire.
	smuggler := &Analyzer{
		Name:  "test-smuggler",
		Doc:   "emits a rule it did not declare",
		Rules: []string{RuleUntestedRoute},
		Run: func(*Pass) ([]Diagnostic, error) {
			return []Diagnostic{{RuleID: RuleSQLStringConcat, File: "a.go", Line: 1, Message: "smuggled"}}, nil
		},
	}
	Register(smuggler)

	report, err := Run(p, RunOptions{Analyzers: []string{"test-smuggler"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.RuleID == RuleSQLStringConcat {
			t.Fatal("undeclared rule was reported")
		}
	}
	if len(report.Errors) == 0 {
		t.Fatal("the breach was dropped silently")
	}
	if report.Passed() {
		t.Error("a run with an analyzer error must not pass — a check that did not run proved nothing")
	}
}

func TestReportApplyRejectsStaleEdit(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	report := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{{
			RuleID: RuleNonUppercaseVerb, File: "a.go", Line: 1,
			Fix: &SuggestedFix{
				Description: "out of range",
				Edits:       []TextEdit{{File: "a.go", Start: 0, End: 9999, New: "x"}},
			},
		}},
	}
	if _, err := report.Apply(); err == nil {
		t.Fatal("an out-of-range edit was applied — that corrupts the file")
	}
	body, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(body) != "package a\n" {
		t.Fatalf("file was modified despite the error: %q", body)
	}
}

func TestReportApplyRejectsChangedContent(t *testing.T) {
	// A file edited since analysis but still long enough passes every
	// bounds check, and a stale offset silently applied is a corrupted
	// source file. When the edit records what it expects to replace, the
	// mismatch is caught by content, not length.
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package abcd\n")
	stale := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{{
			RuleID: RuleNonUppercaseVerb, File: "a.go", Line: 1,
			Fix: &SuggestedFix{
				Description: "same length, different bytes",
				Edits:       []TextEdit{{File: "a.go", Start: 8, End: 12, Old: "wxyz", New: "PKGX"}},
			},
		}},
	}
	if _, err := stale.Apply(); err == nil {
		t.Fatal("an edit whose expected text is gone was applied — that corrupts the file")
	}
	body, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(body) != "package abcd\n" {
		t.Fatalf("file was modified despite the mismatch: %q", body)
	}

	// The same edit with the truth in Old applies cleanly.
	fresh := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{{
			RuleID: RuleNonUppercaseVerb, File: "a.go", Line: 1,
			Fix: &SuggestedFix{
				Description: "matching expectation",
				Edits:       []TextEdit{{File: "a.go", Start: 8, End: 12, Old: "abcd", New: "pkgx"}},
			},
		}},
	}
	if _, err := fresh.Apply(); err != nil {
		t.Fatalf("an edit whose Old matches was refused: %v", err)
	}
	body, _ = os.ReadFile(filepath.Join(dir, "a.go"))
	if string(body) != "package pkgx\n" {
		t.Fatalf("got %q", body)
	}
}

func TestReportApplyRefusesAnEditThatBreaksParsing(t *testing.T) {
	// A short Old ("}") can match a coincidental byte after the file moved
	// underneath the report, every bounds and content check passes, and
	// the edit lands inside something else. When a file that PARSED
	// before the edit no longer parses after it, the edit is what broke
	// it, and writing that to disk is corruption reported as success.
	body := "package a\n\nfunc f() {\n}\n"
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", body)
	brace := strings.LastIndex(body, "}")
	report := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{{
			RuleID: RuleInsecureCookie, File: "a.go", Line: 3,
			Fix: &SuggestedFix{
				Description: "cookie fields, landing in the wrong brace",
				Edits: []TextEdit{{
					File: "a.go", Start: brace, End: brace + 1,
					Old: "}", New: "\nSecure: true,\n}",
				}},
			},
		}},
	}
	if _, err := report.Apply(); err == nil {
		t.Fatal("an edit that turns parsing Go into a parse error was written to disk")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(got) != body {
		t.Fatalf("the file was corrupted despite the refusal: %q", got)
	}

	// A file that was ALREADY mid-refactor keeps the lenient path: the
	// input did not parse to begin with, so the edit cannot be blamed,
	// and silently discarding the fix would be worse.
	broken := "package a\n\nfunc f( {\n}\n"
	writeTemp(t, dir, "b.go", broken)
	lenient := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{{
			RuleID: RuleInsecureCookie, File: "b.go", Line: 3,
			Fix: &SuggestedFix{
				Description: "fix on an already-unparsable file",
				Edits:       []TextEdit{{File: "b.go", Start: 0, End: 7, Old: "package", New: "package"}},
			},
		}},
	}
	if _, err := lenient.Apply(); err != nil {
		t.Fatalf("an already-unparsable input was refused: %v", err)
	}
}

func TestReportApplyRejectsOverlap(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package aaaa\n")
	report := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{
			{RuleID: RuleNonUppercaseVerb, File: "a.go", Fix: &SuggestedFix{
				Description: "one", Edits: []TextEdit{{File: "a.go", Start: 0, End: 8, New: "x"}}}},
			{RuleID: RuleNonUppercaseVerb, File: "a.go", Fix: &SuggestedFix{
				Description: "two", Edits: []TextEdit{{File: "a.go", Start: 4, End: 12, New: "y"}}}},
		},
	}
	if _, err := report.Apply(); err == nil {
		t.Fatal("overlapping edits were applied — the result depends on ordering luck")
	}
}

func TestReportApplyWritesNonOverlappingEdits(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "aaaa bbbb cccc\n")
	report := &Report{
		Root: dir,
		Diagnostics: []Diagnostic{
			{RuleID: RuleNonUppercaseVerb, File: "a.go", Fix: &SuggestedFix{
				Description: "first", Edits: []TextEdit{{File: "a.go", Start: 0, End: 4, New: "AAAA"}}}},
			{RuleID: RuleNonUppercaseVerb, File: "a.go", Fix: &SuggestedFix{
				Description: "second", Edits: []TextEdit{{File: "a.go", Start: 10, End: 14, New: "CCCC"}}}},
		},
	}
	applied, err := report.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %d fixes, want 2", len(applied))
	}
	body, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(body) != "AAAA bbbb CCCC\n" {
		t.Fatalf("got %q", body)
	}
}

func TestExitCodeAndFailOn(t *testing.T) {
	warnOnly := &Report{
		FailOn:      SeverityError,
		Diagnostics: []Diagnostic{{Severity: SeverityWarn}},
	}
	if !warnOnly.Passed() || warnOnly.ExitCode() != 0 {
		t.Error("warnings must not fail a default run")
	}
	warnOnly.FailOn = SeverityWarn
	if warnOnly.Passed() || warnOnly.ExitCode() != 1 {
		t.Error("--strict must make warnings fail")
	}
}

// ----------------------------------------------------------------------
// Output formats
// ----------------------------------------------------------------------

func TestFormatJSONCarriesTheWholeRule(t *testing.T) {
	rule, _ := LookupRule(RuleColonPathParam)
	report := &Report{
		Root:   "/tmp/app",
		FailOn: SeverityError,
		Diagnostics: []Diagnostic{{
			RuleID: rule.ID, Slug: rule.Slug, Capability: rule.Capability,
			Severity: SeverityError, File: "main.go", Line: 12,
			Message: "boom", Rule: &rule,
		}},
	}
	report.summarize()
	data, err := FormatJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema      int  `json:"schema"`
		Passed      bool `json:"passed"`
		Diagnostics []struct {
			Rule    string `json:"rule"`
			RuleDoc struct {
				Why string `json:"why"`
				Fix string `json:"fix"`
			} `json:"ruleDoc"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != JSONSchemaVersion {
		t.Errorf("schema = %d", doc.Schema)
	}
	if doc.Passed {
		t.Error("a report with an error must not say passed")
	}
	// The redundancy is the feature: an agent handed one diagnostic can
	// act on it without a second lookup.
	if doc.Diagnostics[0].RuleDoc.Why == "" || doc.Diagnostics[0].RuleDoc.Fix == "" {
		t.Error("diagnostic does not carry its rule's why/fix")
	}
}

func TestFormatSARIFIsWellFormed(t *testing.T) {
	rule, _ := LookupRule(RuleSQLStringConcat)
	report := &Report{
		Diagnostics: []Diagnostic{{
			RuleID: rule.ID, Severity: SeverityError,
			File: "db.go", Line: 7, Message: "concat", Rule: &rule,
		}},
	}
	data, err := FormatSARIF(report, "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID      string `json:"id"`
						HelpURI string `json:"helpUri"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version = %q", doc.Version)
	}
	run := doc.Runs[0]
	if len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != rule.ID {
		t.Errorf("driver rules = %+v", run.Tool.Driver.Rules)
	}
	if run.Results[0].Level != "error" {
		t.Errorf("level = %q", run.Results[0].Level)
	}
	if run.Results[0].Locations[0].PhysicalLocation.Region.StartLine != 7 {
		t.Error("line lost in SARIF conversion")
	}
}

func TestFormatSARIFDeclaresItsUriBase(t *testing.T) {
	// Artifact URIs are relative to the ANALYSED root, which is not
	// necessarily the repository root. Without a declared base a consumer
	// assumes repo-root and maps every annotation onto a path that does
	// not exist, silently, which is the worst way to be wrong.
	rule, _ := LookupRule(RuleIgnoredExec)
	report := &Report{
		Root: "/work/app/sub",
		Diagnostics: []Diagnostic{{
			RuleID: rule.ID, Severity: SeverityError,
			File: "db.go", Line: 3, Message: "discarded", Rule: &rule,
		}},
	}
	data, err := FormatSARIF(report, "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			OriginalUriBaseIds map[string]struct {
				URI string `json:"uri"`
			} `json:"originalUriBaseIds"`
			Results []struct {
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI       string `json:"uri"`
							UriBaseID string `json:"uriBaseId"`
						} `json:"artifactLocation"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	run := doc.Runs[0]
	al := run.Results[0].Locations[0].PhysicalLocation.ArtifactLocation
	if al.UriBaseID == "" {
		t.Fatal("artifact location declares no uriBaseId")
	}
	base, ok := run.OriginalUriBaseIds[al.UriBaseID]
	if !ok {
		t.Fatalf("uriBaseId %q is not declared in originalUriBaseIds", al.UriBaseID)
	}
	// The base must be an absolute file:// URI ending in a slash, or the
	// concatenation below is not a path.
	if !strings.HasPrefix(base.URI, "file:///") || !strings.HasSuffix(base.URI, "/") {
		t.Errorf("base URI is not a slash-terminated absolute file URI: %q", base.URI)
	}
	if got := base.URI + al.URI; got != "file:///work/app/sub/db.go" {
		t.Errorf("base + uri = %q, want the analysed file's absolute path", got)
	}
}

func TestFormatTextIsPlainWithoutColor(t *testing.T) {
	rule, _ := LookupRule(RuleIgnoredExec)
	report := &Report{
		FailOn: SeverityError,
		Diagnostics: []Diagnostic{{
			RuleID: rule.ID, Slug: rule.Slug, Capability: rule.Capability,
			Severity: SeverityError, File: "a.go", Line: 3,
			Message: "discarded", Rule: &rule,
		}},
	}
	report.summarize()
	out := FormatText(report, TextOptions{})
	if strings.Contains(out, "\033[") {
		t.Error("ANSI escapes emitted with Color off")
	}
	for _, want := range []string{rule.ID, "a.go:3", "discarded", "why:", "fix:"} {
		if !strings.Contains(out, want) {
			t.Errorf("report is missing %q", want)
		}
	}
}

func TestFormatTextReportsRelaxations(t *testing.T) {
	// A clean run that is clean because rules were switched off must say
	// so. Otherwise a passing report is not evidence of anything.
	report := &Report{FailOn: SeverityError, Relaxations: []string{"capability security → off"}}
	report.summarize()
	out := FormatText(report, TextOptions{})
	if !strings.Contains(out, "relaxations in effect") || !strings.Contains(out, "security → off") {
		t.Errorf("clean report hides its relaxations:\n%s", out)
	}
}

// ----------------------------------------------------------------------
// Pass
// ----------------------------------------------------------------------

func TestPassClassifiesFiles(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "go.mod", "module example.com/app\n\ngo 1.26\n")
	writeTemp(t, dir, "main.go", "package main\n")
	writeTemp(t, dir, "main_test.go", "package main\n")
	writeTemp(t, dir, "zz_gen.go", "// Code generated by x. DO NOT EDIT.\npackage main\n")
	writeTemp(t, dir, "dist/ignored.go", "package dist\n")
	writeTemp(t, dir, "sub/pkg.go", "package sub\n")

	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if p.ModulePath != "example.com/app" {
		t.Errorf("module path = %q", p.ModulePath)
	}
	names := map[string]SourceFile{}
	for _, f := range p.Files() {
		names[f.Rel] = f
	}
	if _, found := names["dist/ignored.go"]; found {
		t.Error("build output directory was walked")
	}
	if !names["zz_gen.go"].IsGenerated {
		t.Error("generated file not detected")
	}
	if !names["main_test.go"].IsTest {
		t.Error("test file not detected")
	}
	if got := names["sub/pkg.go"].Package; got != "example.com/app/sub" {
		t.Errorf("package path = %q", got)
	}
	for _, f := range p.AppFiles() {
		if f.IsTest || f.IsGenerated {
			t.Errorf("AppFiles included %s", f.Rel)
		}
	}
}

func TestPassMemoComputesOnce(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for i := 0; i < 3; i++ {
		v := p.Memo("k", func() any { calls++; return 42 })
		if v.(int) != 42 {
			t.Fatalf("memo returned %v", v)
		}
	}
	if calls != 1 {
		t.Errorf("memo body ran %d times", calls)
	}
}

// ----------------------------------------------------------------------
// Capability parsing and ordering
// ----------------------------------------------------------------------

func TestParseCapabilityAcceptsAliases(t *testing.T) {
	// The CLI shipped `gofastr audit a11y` for a year; rejecting
	// `gofastr verify a11y` afterwards would just be rude.
	cases := map[string]Capability{
		"a11y": CapAccessibility, "accessibility": CapAccessibility,
		"sec": CapSecurity, "perf": CapPerformance, "arch": CapArchitecture,
		"tests": CapTesting, "coverage": CapTesting, "authz": CapPermissions,
		"entity": CapEntities, "routes": CapRouting, "ui": CapRendering,
		"db": CapData, "guidance": CapAI, "meta": CapMeta,
		"  SECURITY  ": CapSecurity,
	}
	for input, want := range cases {
		got, err := ParseCapability(input)
		if err != nil {
			t.Errorf("ParseCapability(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseCapability(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseCapability("nonsense"); err == nil {
		t.Error("an unknown capability was accepted")
	}
}

func TestCapabilitiesAreOrderedAndComplete(t *testing.T) {
	caps := Capabilities()
	if len(caps) == 0 {
		t.Fatal("no capabilities")
	}
	seen := map[Capability]bool{}
	for i, c := range caps {
		if !c.Valid() {
			t.Errorf("%q reports invalid", c)
		}
		if c.Order() != i {
			t.Errorf("%q Order() = %d, want %d", c, c.Order(), i)
		}
		if seen[c] {
			t.Errorf("%q appears twice", c)
		}
		seen[c] = true
		if c.Title() == "" {
			t.Errorf("%q has no title", c)
		}
	}
	// Every rule's capability has to be in the report order, or its
	// findings sort into a section that never prints.
	for _, r := range AllRules() {
		if !seen[r.Capability] {
			t.Errorf("%s uses capability %q, which is not in report order", r.ID, r.Capability)
		}
	}
	// Mutating the returned slice must not corrupt the package state.
	caps[0] = Capability("mutated")
	if Capabilities()[0] == Capability("mutated") {
		t.Error("Capabilities() exposes its backing array")
	}
}

func TestUnknownCapabilitySortsLast(t *testing.T) {
	unknown := Capability("invented")
	if unknown.Valid() {
		t.Error("an invented capability reports valid")
	}
	if unknown.Order() < len(Capabilities()) {
		t.Error("an unknown capability jumped the report order")
	}
}

// ----------------------------------------------------------------------
// Architecture config
// ----------------------------------------------------------------------

func TestArchitectureConfigParses(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", `
contracts:
  architecture:
    layers:
      - name: app
        packages: ["app/**"]
      - name: core
        packages: ["core/**", "shared/**"]
    forbid:
      - from: "core/**"
        to: "app/**"
        reason: "core must not know about the app"
`)
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Architecture.Configured() {
		t.Fatal("architecture not marked configured")
	}
	if len(cfg.Architecture.Layers) != 2 {
		t.Fatalf("layers = %+v", cfg.Architecture.Layers)
	}
	// Order is the whole semantics: layer 0 is the top.
	if cfg.Architecture.Layers[0].Name != "app" || cfg.Architecture.Layers[1].Name != "core" {
		t.Errorf("layer order lost: %+v", cfg.Architecture.Layers)
	}
	if len(cfg.Architecture.Layers[1].Packages) != 2 {
		t.Errorf("layer packages = %+v", cfg.Architecture.Layers[1].Packages)
	}
	f := cfg.Architecture.Forbid
	if len(f) != 1 || f[0].From != "core/**" || f[0].To != "app/**" || f[0].Reason == "" {
		t.Errorf("forbid = %+v", f)
	}
}

func TestUnconfiguredArchitectureIsSilent(t *testing.T) {
	// Inventing a layering for someone else's package tree would be wrong
	// more often than right, and a wrong architecture rule teaches people
	// to ignore the analyzer.
	if DefaultConfig().Architecture.Configured() {
		t.Error("the default config claims an architecture")
	}
}

func TestLayerWithoutPackagesIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml",
		"contracts:\n  architecture:\n    layers:\n      - name: app\n")
	if _, err := LoadConfig(dir, ""); err == nil {
		t.Fatal("a layer with no packages was accepted")
	}
}

func TestCapabilityBlockFormsParse(t *testing.T) {
	// All four spellings are things people write; rejecting three of them
	// teaches nothing. The rule under test defaults to error, so any
	// relaxation is visible.
	cases := []struct {
		body string
		want Severity
	}{
		{"contracts:\n  capabilities:\n    security: warn\n", SeverityWarn},
		{"contracts:\n  capabilities:\n    security: false\n", SeverityOff},
		{"contracts:\n  capabilities:\n    security:\n      enabled: false\n", SeverityOff},
		{"contracts:\n  capabilities:\n    security:\n      severity: info\n      exempt: [\"gen/**\"]\n", SeverityInfo},
		{"contracts:\n  security: warn\n", SeverityWarn}, // top-level short form
	}
	sqlConcat, _ := LookupRule(RuleSQLStringConcat)
	if sqlConcat.Severity != SeverityError {
		t.Fatalf("test premise broken: %s defaults to %v", sqlConcat.ID, sqlConcat.Severity)
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeTemp(t, dir, "gofastr.contracts.yml", c.body)
		cfg, err := LoadConfig(dir, "")
		if err != nil {
			t.Fatalf("%q: %v", c.body, err)
		}
		if got := cfg.SeverityFor(sqlConcat); got != c.want {
			t.Errorf("%q gave severity %v, want %v", c.body, got, c.want)
		}
	}
}

func TestCapabilityExemptAppliesOnlyToItsCapability(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml",
		"contracts:\n  capabilities:\n    security:\n      exempt: [\"gen/**\"]\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	sqlConcat, _ := LookupRule(RuleSQLStringConcat)
	routing, _ := LookupRule(RuleColonPathParam)
	if !cfg.ExemptFor(sqlConcat, "gen/db.go") {
		t.Error("capability exempt not applied to its own capability")
	}
	if cfg.ExemptFor(routing, "gen/db.go") {
		t.Error("capability exempt leaked to another capability")
	}
}

func TestFailOnOffIsRejected(t *testing.T) {
	// `fail-on: off` would make every run pass, which is worse than no
	// gate at all because it still prints a report.
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", "contracts:\n  fail-on: \"off\"\n")
	if _, err := LoadConfig(dir, ""); err == nil {
		t.Fatal("fail-on: off was accepted")
	}
}

func TestStrictLowersTheFailFloor(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", "contracts:\n  strict: true\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailOn != SeverityWarn {
		t.Errorf("FailOn = %v, want warn under strict", cfg.FailOn)
	}
}

// ----------------------------------------------------------------------
// Output helpers
// ----------------------------------------------------------------------

func TestFormatExplainCoversTheWholeRule(t *testing.T) {
	rule, _ := LookupRule(RuleColonPathParam)
	out := FormatExplain(rule, false)
	for _, want := range []string{
		rule.ID, rule.Title, "What", "Why", "Fix", "Example",
		"Docs", "Suppress once", "Relax project-wide", rule.Doc,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explain output missing %q", want)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Error("ANSI escapes emitted with color off")
	}
}

func TestFormatCatalogJSONIsSelfContained(t *testing.T) {
	data, err := FormatCatalogJSON(AllRules())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema int `json:"schema"`
		Rules  []struct {
			ID         string `json:"id"`
			DocURL     string `json:"docUrl"`
			DocCommand string `json:"docCommand"`
			Suppress   string `json:"suppress"`
			Why        string `json:"why"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != JSONSchemaVersion || len(doc.Rules) != len(AllRules()) {
		t.Fatalf("schema=%d rules=%d", doc.Schema, len(doc.Rules))
	}
	// The catalog is what an agent reads before writing code, so every
	// entry has to carry its own derived fields.
	for _, r := range doc.Rules {
		if r.DocURL == "" || r.DocCommand == "" || r.Suppress == "" || r.Why == "" {
			t.Errorf("%s is missing a derived field: %+v", r.ID, r)
		}
		if !strings.Contains(r.Suppress, r.ID) {
			t.Errorf("%s: suppress line does not name the rule: %q", r.ID, r.Suppress)
		}
	}
}

func TestDiagnosticLocationDegradesGracefully(t *testing.T) {
	cases := []struct {
		d    Diagnostic
		want string
	}{
		{Diagnostic{}, "<project>"},
		{Diagnostic{File: "a.go"}, "a.go"},
		{Diagnostic{File: "a.go", Line: 7}, "a.go:7"},
		{Diagnostic{File: "a.go", Line: 7, Column: 3}, "a.go:7:3"},
	}
	for _, c := range cases {
		if got := c.d.Location(); got != c.want {
			t.Errorf("Location() = %q, want %q", got, c.want)
		}
	}
}

func TestDiagnosticsSortWorstFirst(t *testing.T) {
	ds := []Diagnostic{
		{Severity: SeverityInfo, Capability: CapRouting, File: "a.go", Line: 1, RuleID: "GOFASTR1003"},
		{Severity: SeverityError, Capability: CapSecurity, File: "z.go", Line: 9, RuleID: "GOFASTR1401"},
		{Severity: SeverityWarn, Capability: CapRouting, File: "a.go", Line: 2, RuleID: "GOFASTR1004"},
		{Severity: SeverityError, Capability: CapRouting, File: "a.go", Line: 5, RuleID: "GOFASTR1001"},
	}
	sortDiagnostics(ds)
	// Severity first, then capability order, then location: the order
	// someone works through a report in.
	if ds[0].RuleID != "GOFASTR1001" || ds[1].RuleID != "GOFASTR1401" {
		t.Fatalf("errors did not lead, and in capability order: %+v", ds)
	}
	if ds[2].Severity != SeverityWarn || ds[3].Severity != SeverityInfo {
		t.Errorf("severity ordering broken: %+v", ds)
	}
}

func TestDedupeKeepsFirstOccurrence(t *testing.T) {
	// Two analyzers legitimately reach the same conclusion about the same
	// place, routing and permissions both look at an unguarded route.
	ds := dedupe([]Diagnostic{
		{RuleID: "A", File: "a.go", Line: 1, Message: "same"},
		{RuleID: "A", File: "a.go", Line: 1, Message: "same"},
		{RuleID: "A", File: "a.go", Line: 2, Message: "same"},
		{RuleID: "B", File: "a.go", Line: 1, Message: "same"},
	})
	if len(ds) != 3 {
		t.Fatalf("dedupe left %d, want 3: %+v", len(ds), ds)
	}
}

func TestMatchPathIsExported(t *testing.T) {
	// The architecture analyzer needs the same dialect for import paths.
	if !MatchPath("core/**", "core/router") || MatchPath("core/**", "framework/app") {
		t.Error("MatchPath does not behave like matchGlob")
	}
}

func TestAnalyzerCapabilitiesDerivedFromRules(t *testing.T) {
	a := &Analyzer{
		Name:  "test-capabilities",
		Doc:   "spans two capabilities",
		Rules: []string{RuleSQLStringConcat, RuleUnguardedMutation},
		Run:   func(*Pass) ([]Diagnostic, error) { return nil, nil },
	}
	caps := a.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("capabilities = %v", caps)
	}
	// Reported in report order, not declaration order.
	if caps[0] != CapPermissions || caps[1] != CapSecurity {
		t.Errorf("capabilities not in report order: %v", caps)
	}
}

// ----------------------------------------------------------------------
// Baseline: the adoption ratchet
// ----------------------------------------------------------------------

func baselineReport(files map[string]int) *Report {
	// files maps "rule|file" to a count of findings to synthesise.
	r := &Report{FailOn: SeverityWarn}
	for key, n := range files {
		parts := strings.SplitN(key, "|", 2)
		rule, _ := LookupRule(parts[0])
		for i := 0; i < n; i++ {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{
				RuleID: rule.ID, Slug: rule.Slug, Capability: rule.Capability,
				Severity: SeverityWarn, File: parts[1], Line: i + 1,
				Message: fmt.Sprintf("finding %d", i),
			})
		}
	}
	sortDiagnostics(r.Diagnostics)
	r.summarize()
	return r
}

func TestBaselineAbsorbsExistingFindings(t *testing.T) {
	// The adoption problem: strict-by-default plus a mature codebase means
	// hundreds of findings at once, and the realistic response is to turn
	// the tool off. A baseline is the third option.
	before := baselineReport(map[string]int{
		RuleUnguardedMutation + "|main.go": 3,
		RuleUntestedRoute + "|api.go":      2,
	})
	if before.Passed() {
		t.Fatal("test premise: the un-baselined report should fail under --strict")
	}
	b := NewBaseline(before, time.Unix(0, 0), "")
	if b.Total() != 5 {
		t.Fatalf("baseline recorded %d findings, want 5", b.Total())
	}

	after := baselineReport(map[string]int{
		RuleUnguardedMutation + "|main.go": 3,
		RuleUntestedRoute + "|api.go":      2,
	})
	res := after.ApplyBaseline(b)
	if res.Accepted != 5 || len(after.Diagnostics) != 0 {
		t.Fatalf("accepted=%d remaining=%d, want 5/0", res.Accepted, len(after.Diagnostics))
	}
	if !after.Passed() {
		t.Error("a fully baselined report must pass")
	}
	if after.Baselined != 5 {
		t.Errorf("report.Baselined = %d", after.Baselined)
	}
}

func TestBaselineDoesNotAbsorbNewFindings(t *testing.T) {
	// The whole point of the ratchet: existing debt is carried, new debt
	// is not. Without this the baseline is just a mute button.
	b := NewBaseline(baselineReport(map[string]int{
		RuleUnguardedMutation + "|main.go": 1,
	}), time.Unix(0, 0), "")

	// Same rule, one MORE occurrence in the same file.
	grown := baselineReport(map[string]int{RuleUnguardedMutation + "|main.go": 2})
	res := grown.ApplyBaseline(b)
	if res.Accepted != 1 || len(grown.Diagnostics) != 1 {
		t.Fatalf("accepted=%d remaining=%d, want 1/1", res.Accepted, len(grown.Diagnostics))
	}
	if grown.Passed() {
		t.Error("a new finding beyond the accepted count must still fail")
	}

	// Same rule, a DIFFERENT file: nothing accepted there.
	elsewhere := baselineReport(map[string]int{RuleUnguardedMutation + "|other.go": 1})
	if res := elsewhere.ApplyBaseline(b); res.Accepted != 0 {
		t.Errorf("a finding in an unlisted file was absorbed: %d", res.Accepted)
	}

	// A rule the baseline never recorded is never absorbed.
	newRule := baselineReport(map[string]int{RuleSQLStringConcat + "|main.go": 1})
	if res := newRule.ApplyBaseline(b); res.Accepted != 0 {
		t.Errorf("an unrecorded rule was absorbed: %d", res.Accepted)
	}
}

func TestBaselineReportsPaidDownDebt(t *testing.T) {
	// Debt that was fixed keeps its allowance until someone re-records,
	// and that slack is exactly where a new finding could hide. Saying so
	// is what makes the baseline shrink instead of ossify.
	b := NewBaseline(baselineReport(map[string]int{
		RuleUnguardedMutation + "|main.go": 3,
	}), time.Unix(0, 0), "")

	fixed := baselineReport(map[string]int{RuleUnguardedMutation + "|main.go": 1})
	res := fixed.ApplyBaseline(b)
	if res.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", res.Accepted)
	}
	if len(res.Fixed) != 1 {
		t.Fatalf("Fixed = %+v, want one over-accepting entry", res.Fixed)
	}
	if res.Fixed[0].Baseline != 3 || res.Fixed[0].Current != 1 {
		t.Errorf("delta = %+v, want baseline 3 / current 1", res.Fixed[0])
	}
	if fixed.BaselineFixed != 1 {
		t.Errorf("report.BaselineFixed = %d", fixed.BaselineFixed)
	}
}

func TestBaselineKeepsTheWorstFindingVisible(t *testing.T) {
	// When a bucket overflows, the finding left showing should be the
	// most severe, not whichever happened to sort last.
	r := &Report{FailOn: SeverityWarn}
	for _, sev := range []Severity{SeverityWarn, SeverityError} {
		rule, _ := LookupRule(RuleUnguardedMutation)
		r.Diagnostics = append(r.Diagnostics, Diagnostic{
			RuleID: rule.ID, Capability: rule.Capability, Severity: sev,
			File: "main.go", Message: fmt.Sprint(sev),
		})
	}
	sortDiagnostics(r.Diagnostics)
	b := &Baseline{Schema: BaselineSchemaVersion, Counts: map[string]map[string]int{
		RuleUnguardedMutation: {"main.go": 1},
	}}
	r.ApplyBaseline(b)
	if len(r.Diagnostics) != 1 {
		t.Fatalf("remaining = %d, want 1", len(r.Diagnostics))
	}
	if r.Diagnostics[0].Severity != SeverityWarn {
		t.Errorf("the error was absorbed and the warning kept — worst should survive")
	}
}

func TestBaselineRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, BaselineFileName)
	b := NewBaseline(baselineReport(map[string]int{
		RuleUnguardedMutation + "|main.go": 2,
	}), time.Unix(0, 0), "accepted while migrating")

	if err := WriteBaseline(path, b); err != nil {
		t.Fatal(err)
	}
	got, err := ReadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total() != 2 || got.Note != "accepted while migrating" {
		t.Errorf("round trip lost data: %+v", got)
	}
	if got.Schema != BaselineSchemaVersion {
		t.Errorf("schema = %d", got.Schema)
	}
}

func TestMissingBaselineIsNotAnError(t *testing.T) {
	// No baseline is the normal state for a new project; it must not nag.
	b, err := ReadBaseline(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || b != nil {
		t.Fatalf("ReadBaseline(missing) = %v, %v; want nil, nil", b, err)
	}
}

func TestBaselineRejectsAFutureSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.json")
	if err := os.WriteFile(path, []byte(`{"schema":99,"counts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBaseline(path); err == nil {
		t.Fatal("a newer baseline was read as if this build understood it")
	}
}

func TestNilBaselineIsANoOp(t *testing.T) {
	r := baselineReport(map[string]int{RuleUnguardedMutation + "|main.go": 2})
	if res := r.ApplyBaseline(nil); res.Accepted != 0 || len(r.Diagnostics) != 2 {
		t.Errorf("a nil baseline changed the report")
	}
}

// A project's own rules ride the same pipeline as the catalog: register
// the rule, register the analyzer, and config, suppressions, severity
// and reporting all apply. The only namespace requirement is a non-
// GOFASTR prefix, so a custom ID can never collide with a future
// catalog entry.
func TestCustomRulesRideTheWholePipeline(t *testing.T) {
	customOnce.Do(func() {
		RegisterRules(Rule{
			ID: "ACME101", Slug: "data/orders-are-audited",
			Title: "Order writes bypass the audit trail", Capability: CapData, Severity: SeverityError,
			Summary: "A write to orders does not go through audit.Record.",
			Why:     "Regulated flows must leave a trail; a silent write is an audit failure.",
			Fix:     "Wrap the write in audit.Record(...).",
			Doc:     "entity-declarations",
			Examples: []Example{{
				Bad:  `db.Exec("UPDATE orders ...")`,
				Good: `audit.Record(ctx, func() { db.Exec("UPDATE orders ...") })`,
			}},
		})
		Register(&Analyzer{
			Name:  "acme-audit",
			Doc:   "flags rawOrderWrite() calls",
			Rules: []string{"ACME101"},
			Run: func(p *Pass) ([]Diagnostic, error) {
				var ds []Diagnostic
				for _, f := range p.Files() {
					for i, line := range p.Lines(f.Rel) {
						if strings.Contains(line, "rawOrderWrite()") {
							ds = append(ds, Diagnostic{
								RuleID: "ACME101", File: f.Rel, Line: i + 1,
								Message: "order write outside the audit trail",
							})
						}
					}
				}
				return ds, nil
			},
		})
	})

	run := func(t *testing.T, source, config string) *Report {
		t.Helper()
		dir := t.TempDir()
		writeTemp(t, dir, "a.go", source)
		cfgPath := ""
		if config != "" {
			cfgPath = writeTemp(t, dir, "gofastr.contracts.yml", config)
		}
		cfg, err := LoadConfig(dir, cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"acme-audit"}})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}

	// Fires like any catalog rule, carrying its full rule document.
	report := run(t, "package a\n\nfunc f() { rawOrderWrite() }\n", "")
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].RuleID != "ACME101" {
		t.Fatalf("diagnostics = %+v, want one ACME101 finding", report.Diagnostics)
	}
	d := report.Diagnostics[0]
	if d.Severity != SeverityError || d.Rule == nil || d.Rule.Why == "" {
		t.Errorf("the custom finding is not carrying its rule document: %+v", d)
	}
	if report.Passed() {
		t.Error("an error-severity custom finding passed the gate")
	}

	// Suppressible with the standard directive.
	suppressed := run(t, "package a\n\nfunc f() { rawOrderWrite() } //gofastr:allow(ACME101) legacy import path\n", "")
	if len(suppressed.Diagnostics) != 0 || suppressed.Suppressed != 1 {
		t.Errorf("suppression did not apply to the custom rule: %+v", suppressed.Diagnostics)
	}

	// Configurable like any catalog rule.
	relaxed := run(t, "package a\n\nfunc f() { rawOrderWrite() }\n",
		"rules:\n  ACME101: off\n")
	if len(relaxed.Diagnostics) != 0 {
		t.Errorf("config could not turn the custom rule off: %+v", relaxed.Diagnostics)
	}
}

var customOnce sync.Once

func mustPass(t *testing.T, dir string, cfg *Config) *Pass {
	t.Helper()
	p, err := NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A custom prefix that merely STARTS with "GOFASTR" is still a custom
// prefix. HasPrefix alone routed GOFASTRA123 into the block check, where
// TrimPrefix left "A123" and Atoi panicked the host app at init with
// "unparsable number", describing neither the rule nor a prohibition.
// Block discipline belongs to GOFASTR + digits, exactly.
func TestGofastrLookalikePrefixIsALegalCustomPrefix(t *testing.T) {
	r := Rule{
		ID: "GOFASTRA123", Slug: "data/lookalike-prefix",
		Title: "t", Capability: CapData, Severity: SeverityWarn,
		Summary: "s", Why: "w", Fix: "f", Doc: "entity-declarations",
		Examples: []Example{{Bad: "b", Good: "g"}},
	}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("a legal custom prefix panicked at init: %v", rec)
		}
	}()
	RegisterRules(r)
}

// The GOFASTR namespace keeps its discipline: custom prefixes skip the
// capability block check (they have no assigned blocks), but a GOFASTR
// ID outside its capability's block is still refused.
func TestCustomPrefixSkipsTheBlockCheckGofastrKeepsIt(t *testing.T) {
	base := Rule{
		Slug: "data/x-rule", Title: "t", Capability: CapData, Severity: SeverityWarn,
		Summary: "s", Why: "w", Fix: "f", Doc: "entity-declarations",
		Examples: []Example{{Bad: "b", Good: "g"}},
	}
	out := base
	out.ID = "GOFASTR9901" // data does not own the 99xx block
	func() {
		defer func() {
			if recover() == nil {
				t.Error("a GOFASTR ID outside its capability block was accepted")
			}
		}()
		RegisterRules(out)
	}()

	ok := base
	ok.ID = "ACME999"
	ok.Slug = "data/x-rule-ok"
	RegisterRules(ok) // must not panic: custom prefixes have no blocks
}

// Only matches nothing for an unknown name, the safe default for a
// fixer, but silence about it is a library-caller trap: a typo'd custom
// rule ID produced an empty, PASSING scope. The CLI and MCP validate
// names before calling; a direct caller now gets the analyzer-error
// contract instead ("a check that could not execute has proven nothing").
func TestOnlyReportsAnUnknownRuleName(t *testing.T) {
	r := baselineReport(map[string]int{RuleUnguardedMutation + "|main.go": 1})
	narrowed := r.Only("ACMETYPO999")
	if len(narrowed.Diagnostics) != 0 {
		t.Errorf("an unknown name matched %d diagnostics", len(narrowed.Diagnostics))
	}
	if len(narrowed.Errors) == 0 {
		t.Fatal("an unknown rule name narrowed to a silent empty scope")
	}
	if narrowed.Passed() {
		t.Error("a scope that references a rule that does not exist passed")
	}
}

// tripRun runs the real pipeline, Run with suppressions live, over a
// tree whose only analyzer emits GOFASTR1403 on every line containing
// `trip()`. The baseline/suppression interaction cannot be tested through
// baselineReport: the hole under test is exactly that suppression is
// consumed inside Run, before ApplyBaseline ever sees the report.
func registerTripAnalyzer() {
	registerOnce.Do(func() {
		Register(&Analyzer{
			Name:  "test-trip",
			Doc:   "emits GOFASTR1403 on every line containing trip()",
			Rules: []string{RuleHTMLConcat},
			Run: func(p *Pass) ([]Diagnostic, error) {
				var ds []Diagnostic
				for _, f := range p.Files() {
					for i, line := range p.Lines(f.Rel) {
						if strings.Contains(line, "trip()") {
							ds = append(ds, Diagnostic{
								RuleID: RuleHTMLConcat, File: f.Rel, Line: i + 1,
								Message: "tripped",
							})
						}
					}
				}
				return ds, nil
			},
		})
	})
}

func tripRun(t *testing.T, source string) *Report {
	t.Helper()
	registerTripAnalyzer()
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", source)
	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(p, RunOptions{Analyzers: []string{"test-trip"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) > 0 {
		t.Fatalf("analyzer errors: %v", report.Errors)
	}
	return report
}

var registerOnce sync.Once

func TestSuppressedFindingKeepsItsBaselineSlot(t *testing.T) {
	// The ratchet's promise is count-based: N accepted occurrences of a
	// rule in a file, and occurrence N+1 fails. Suppressing one of the N
	// must not hand its slot to a brand-new finding. Otherwise adding a
	// //gofastr:allow to baselined debt quietly widens what the gate
	// accepts, with the count balancing so nothing ever says so.
	b := NewBaseline(tripRun(t, `package a

func f() {
	trip()
	trip()
}
`), time.Unix(0, 0), "")
	if b.Total() != 2 {
		t.Fatalf("premise: baseline recorded %d, want 2", b.Total())
	}

	// One original finding is now suppressed, and a NEW third occurrence
	// appears. Total occurrences grew 2 → 3; the gate must fail.
	after := tripRun(t, `package a

func f() {
	trip() //gofastr:allow(GOFASTR1403) accepted debt, documented
	trip()
	trip()
}
`)
	if after.Suppressed != 1 || len(after.Diagnostics) != 2 {
		t.Fatalf("premise: suppressed=%d visible=%d, want 1/2", after.Suppressed, len(after.Diagnostics))
	}
	res := after.ApplyBaseline(b)
	if res.Accepted != 1 {
		t.Errorf("accepted = %d, want 1 — the suppressed finding's slot absorbed a new finding", res.Accepted)
	}
	if len(after.Diagnostics) != 1 {
		t.Errorf("visible = %d, want 1 — growth beyond the accepted count must stay visible", len(after.Diagnostics))
	}
	if after.Passed() {
		t.Error("a new finding beyond the accepted count passed the gate")
	}
}

// The meta rule that polices suppressions must itself honour the
// suppression and exemption contract. Its findings were appended after
// the filtering loop without either check, so GOFASTR0002 was the one
// rule in the catalog that could not be waived locally, and the waiver
// directive was itself reported stale.
func TestStaleSuppressionIsWaivableLikeAnyFinding(t *testing.T) {
	registerTripAnalyzer()
	run := func(t *testing.T, files map[string]string) *Report {
		t.Helper()
		dir := t.TempDir()
		for name, body := range files {
			writeTemp(t, dir, name, body)
		}
		cfg, err := LoadConfig(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"test-trip"}})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	countStale := func(r *Report) int {
		n := 0
		for _, d := range r.Diagnostics {
			if d.RuleID == RuleSuppressionStale {
				n++
			}
		}
		return n
	}

	const staleFile = "package a\n\n//gofastr:allow(GOFASTR1403) no longer trips\nfunc f() {}\n"

	// Premise: the directive really is stale and really is reported.
	if got := countStale(run(t, map[string]string{"a.go": staleFile})); got != 1 {
		t.Fatalf("premise: stale findings = %d, want 1", got)
	}

	// A file-scoped waiver silences it, and must not itself be stale.
	waived := run(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow-file(GOFASTR0002) vendored tree, directives kept for upstream\n\n" +
			"//gofastr:allow(GOFASTR1403) no longer trips\nfunc f() {}\n",
	})
	if got := countStale(waived); got != 0 {
		t.Errorf("stale findings with a waiver = %d, want 0: %+v", got, waived.Diagnostics)
	}
	if waived.Suppressed == 0 {
		t.Error("the waived stale finding was not counted as suppressed")
	}

	// A per-rule exemption does the same.
	exempt := run(t, map[string]string{
		"a.go":                  staleFile,
		"gofastr.contracts.yml": "rules:\n  GOFASTR0002:\n    exempt: [\"a.go\"]\n",
	})
	if got := countStale(exempt); got != 0 {
		t.Errorf("stale findings under an exemption = %d, want 0: %+v", got, exempt.Diagnostics)
	}
}

// The slot guarantee has to hold for GOFASTR0002 itself. The stale
// two-pass has its own suppression branch, and that branch skipped the
// suppressedAt recording the main loop does, so waiving a baselined
// stale directive freed its slot, and a brand-new unwaived stale
// directive in the same file was absorbed behind a balanced count.
func TestWaivedStaleDirectiveKeepsItsBaselineSlot(t *testing.T) {
	registerTripAnalyzer()
	run := func(t *testing.T, src string) *Report {
		t.Helper()
		dir := t.TempDir()
		writeTemp(t, dir, "a.go", src)
		cfg := DefaultConfig()
		cfg.FailOn = SeverityWarn
		report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"test-trip"}})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}

	b := NewBaseline(run(t, "package a\n\n//gofastr:allow(GOFASTR1403) gone\nfunc f() {}\n"),
		time.Unix(0, 0), "")
	if b.Counts[RuleSuppressionStale]["a.go"] != 1 {
		t.Fatalf("premise: baseline did not record the stale finding: %v", b.Counts)
	}

	// The recorded directive is now waived, and a SECOND, unwaived stale
	// directive appears: total stale debt grew, the gate must see it.
	after := run(t, `package a

//gofastr:allow(GOFASTR0002) directive kept for the vendor sync
//gofastr:allow(GOFASTR1403) gone
func f() {}

//gofastr:allow(GOFASTR1403) also gone
func g() {}
`)
	if after.Suppressed != 1 || len(after.Diagnostics) != 1 {
		t.Fatalf("premise: suppressed=%d visible=%d, want 1/1", after.Suppressed, len(after.Diagnostics))
	}
	after.ApplyBaseline(b)
	if len(after.Diagnostics) != 1 {
		t.Errorf("visible = %d, want 1 — the waived stale directive's slot absorbed brand-new stale debt",
			len(after.Diagnostics))
	}
}

// Applying a baseline is a one-shot consumption: a second call sees the
// already-emptied diagnostic list, re-claims the suppressed slots, and
// rewrites Baselined to 0 and Fixed to nonsense. No caller does this on
// purpose, which is exactly why the misuse must be inert rather than
// silently corrupting.
func TestApplyBaselineIsSingleShot(t *testing.T) {
	b := NewBaseline(tripRun(t, "package a\n\nfunc f() {\n\ttrip()\n\ttrip()\n}\n"), time.Unix(0, 0), "")
	report := tripRun(t, "package a\n\nfunc f() {\n\ttrip() //gofastr:allow(GOFASTR1403) accepted\n\ttrip()\n}\n")

	first := report.ApplyBaseline(b)
	if first.Accepted != 1 || report.Baselined != 1 || report.BaselineFixed != 1 {
		t.Fatalf("premise: first apply accepted=%d baselined=%d fixed=%d, want 1/1/1",
			first.Accepted, report.Baselined, report.BaselineFixed)
	}

	second := report.ApplyBaseline(b)
	if second.Accepted != 0 || len(second.Fixed) != 0 {
		t.Errorf("second apply did work: %+v", second)
	}
	if report.Baselined != 1 || report.BaselineFixed != 1 {
		t.Errorf("second apply rewrote the counters: baselined=%d fixed=%d, want 1/1",
			report.Baselined, report.BaselineFixed)
	}
}

// The slot guarantee cannot depend on the current run's fail floor. A
// baseline recorded under --strict holds warn-severity entries; a later
// non-strict run still applies them, and if warn suppressions were not
// tracked there, a suppressed warn finding freed its slot and the next
// new warn finding was displayed as accepted debt instead of as itself.
func TestSuppressedSlotHoldsAcrossFailOnMismatch(t *testing.T) {
	registerTripAnalyzer()
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", `package a

func f() {
	trip() //gofastr:allow(GOFASTR1403) accepted while migrating
	trip()
}
`)
	writeTemp(t, dir, "gofastr.contracts.yml", "rules:\n  GOFASTR1403: warn\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailOn != SeverityError {
		t.Fatalf("premise: default fail floor is %v, want error", cfg.FailOn)
	}
	report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"test-trip"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Suppressed != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("premise: suppressed=%d visible=%d, want 1/1", report.Suppressed, len(report.Diagnostics))
	}

	// As recorded by a --strict run: one accepted warn finding.
	b := &Baseline{Schema: BaselineSchemaVersion, Counts: map[string]map[string]int{
		RuleHTMLConcat: {"a.go": 1},
	}}
	report.ApplyBaseline(b)
	if len(report.Diagnostics) != 1 {
		t.Errorf("visible = %d, want 1 — the suppressed warn finding's slot absorbed the new one", len(report.Diagnostics))
	}
}

func TestSuppressedSlotIsReportedAsOverAccepting(t *testing.T) {
	// Once a baselined finding is suppressed, a re-recorded baseline
	// would no longer include it, the entry is dead weight. Saying so is
	// the same nudge that keeps paid-down debt from ossifying.
	b := NewBaseline(tripRun(t, `package a

func f() {
	trip()
	trip()
}
`), time.Unix(0, 0), "")

	after := tripRun(t, `package a

func f() {
	trip() //gofastr:allow(GOFASTR1403) accepted debt, documented
	trip()
}
`)
	res := after.ApplyBaseline(b)
	if res.Accepted != 1 || len(after.Diagnostics) != 0 {
		t.Fatalf("accepted=%d visible=%d, want 1/0", res.Accepted, len(after.Diagnostics))
	}
	if len(res.Fixed) != 1 {
		t.Fatalf("Fixed = %+v, want the suppressed slot reported as over-accepting", res.Fixed)
	}
	if res.Fixed[0].Baseline != 2 || res.Fixed[0].Current != 1 {
		t.Errorf("delta = %+v, want baseline 2 / current 1", res.Fixed[0])
	}
}

// ----------------------------------------------------------------------
// --changed narrowing
// ----------------------------------------------------------------------

func TestRestrictToKeepsOnlyChangedFiles(t *testing.T) {
	r := baselineReport(map[string]int{
		RuleUnguardedMutation + "|new.go": 1,
		RuleUnguardedMutation + "|old.go": 2,
	})
	// A project-level finding, which belongs to no file.
	r.Diagnostics = append(r.Diagnostics, Diagnostic{
		RuleID: RuleNoCoverageManifest, Capability: CapTesting,
		Severity: SeverityInfo, Message: "no manifest",
	})

	dropped := r.RestrictTo(map[string]bool{"new.go": true})
	if dropped != 3 {
		t.Fatalf("dropped %d, want 3 (two in old.go, one project-level)", dropped)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].File != "new.go" {
		t.Fatalf("remaining = %+v", r.Diagnostics)
	}
	// A narrowed run must never read as a whole-repository all-clear.
	if r.OutsideChange != 3 {
		t.Errorf("OutsideChange = %d, want 3", r.OutsideChange)
	}
}

func TestRestrictToNilIsANoOp(t *testing.T) {
	// Not being in a git repository is a legitimate state; the caller
	// should report everything rather than silently report nothing.
	r := baselineReport(map[string]int{RuleUnguardedMutation + "|a.go": 2})
	if dropped := r.RestrictTo(nil); dropped != 0 || len(r.Diagnostics) != 2 {
		t.Errorf("a nil file set narrowed the report")
	}
}

func TestChangedFilesOutsideAGitRepo(t *testing.T) {
	files, err := ChangedFiles(t.TempDir(), "")
	if err != nil {
		t.Fatalf("a non-repository must not be an error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil (do not narrow), got %v", files)
	}
}

func TestChangedFilesListsUncommittedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", ".")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	writeTemp(t, dir, "committed.go", "package a\n")
	git("add", "-A")
	git("commit", "-qm", "init")

	// One tracked file modified, one brand-new file never added. A new
	// file full of findings is exactly what a pre-commit check must
	// catch, and `git diff` alone never lists it.
	writeTemp(t, dir, "committed.go", "package a\n\nvar X = 1\n")
	writeTemp(t, dir, "untracked.go", "package a\n")

	files, err := ChangedFiles(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !files["committed.go"] {
		t.Errorf("modified file missing: %v", SortedFiles(files))
	}
	if !files["untracked.go"] {
		t.Errorf("untracked file missing — a new file's findings would be invisible: %v", SortedFiles(files))
	}
}

func TestBaselineRecordsOnlyGatingFindings(t *testing.T) {
	// A baseline exists to unblock a gate. Recording a finding that
	// cannot fail the run is not merely noise in the file, the entry
	// absorbs that finding on every later run, so an informational signal
	// the project deliberately kept visible disappears instead.
	//
	// This bit for real: the semantic-coverage rules are
	// environment-dependent, so this repository downgraded them to info
	// rather than let them gate, and `--baseline-write` then silenced
	// the very findings the downgrade was meant to keep in view.
	r := &Report{FailOn: SeverityWarn}
	gating, _ := LookupRule(RuleUnguardedMutation)
	informational, _ := LookupRule(RuleRouteNotExercised)
	r.Diagnostics = []Diagnostic{
		{RuleID: gating.ID, Capability: gating.Capability, Severity: SeverityWarn, File: "a.go"},
		{RuleID: gating.ID, Capability: gating.Capability, Severity: SeverityError, File: "a.go"},
		{RuleID: informational.ID, Capability: informational.Capability, Severity: SeverityInfo, File: "b.go"},
	}

	b := NewBaseline(r, time.Unix(0, 0), "")
	if b.Total() != 2 {
		t.Fatalf("baseline recorded %d, want only the 2 gating findings", b.Total())
	}
	if _, recorded := b.Counts[informational.ID]; recorded {
		t.Errorf("an info-severity finding was baselined — it will now be hidden on every run")
	}

	// And the informational finding must survive applying that baseline.
	after := &Report{FailOn: SeverityWarn, Diagnostics: append([]Diagnostic(nil), r.Diagnostics...)}
	after.ApplyBaseline(b)
	if len(after.Diagnostics) != 1 || after.Diagnostics[0].RuleID != informational.ID {
		t.Fatalf("remaining = %+v, want the info finding still visible", after.Diagnostics)
	}
	if !after.Passed() {
		t.Error("an info finding must not fail a warn-gated run")
	}
}

func TestConfigCanEscalateNotOnlyRelax(t *testing.T) {
	// The docs describe config as "only ever relaxes", which is the
	// posture for *defaults*: nothing is enforced less than declared
	// unless someone writes it down. But a team wanting a rule to be
	// STRICTER than the catalog default is a legitimate, visible choice,
	// and every test until now only exercised the downward direction.
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml",
		"contracts:\n  rules:\n    GOFASTR1902: error\n  routing: error\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	unguarded, _ := LookupRule(RuleUnguardedMutation)
	if unguarded.Severity != SeverityWarn {
		t.Fatalf("test premise broken: %s now defaults to %v", unguarded.ID, unguarded.Severity)
	}
	if got := cfg.SeverityFor(unguarded); got != SeverityError {
		t.Errorf("rule escalation not applied: %v", got)
	}
	// Capability-level escalation too.
	untested, _ := LookupRule(RuleUntestedRoute)
	if got := cfg.SeverityFor(untested); got != SeverityError {
		t.Errorf("capability escalation not applied: %v", got)
	}
	// And it must show up as a stated relaxation-list entry either way:
	// the footer's job is "what did config change", not "what did it
	// weaken".
	if len(cfg.Relaxations()) == 0 {
		t.Error("an escalation is invisible in the report footer")
	}
}

func TestPanickingAnalyzerDoesNotKillTheRun(t *testing.T) {
	// Twelve analyzers run concurrently over one pass. If any one of them
	// can take the process down, a single bad rule makes the whole tool
	// unusable, so a panic is caught, attributed, and the other eleven
	// still report. A half-report beats no report.
	Register(&Analyzer{
		Name: "test-panicker", Doc: "panics on purpose",
		Rules: []string{RuleUntestedRoute},
		Run:   func(*Pass) ([]Diagnostic, error) { panic("boom") },
	})
	dir := t.TempDir()
	writeTemp(t, dir, "a.go", "package a\n")
	p, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(p, RunOptions{})
	if err != nil {
		t.Fatalf("one panicking analyzer aborted the entire run: %v", err)
	}
	named := false
	for _, e := range report.Errors {
		if strings.Contains(e, "test-panicker") && strings.Contains(e, "boom") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the panic was swallowed rather than attributed: %v", report.Errors)
	}
	// And a run whose analyzer crashed has proven nothing, so it must not
	// report success.
	if report.Passed() {
		t.Error("a run with a crashed analyzer reported success")
	}
}
