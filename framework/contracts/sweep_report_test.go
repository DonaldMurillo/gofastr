package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweepReportApplyKeepsCRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	src := "package a\r\n\r\nvar x = 1\r\n"
	writeTemp(t, dir, "a.go", src)
	start := strings.Index(src, "1")
	r := &Report{Root: dir, Diagnostics: []Diagnostic{{
		RuleID: RuleHTMLConcat, File: "a.go", Line: 3,
		Fix: &SuggestedFix{Edits: []TextEdit{{File: "a.go", Start: start, End: start + 1, Old: "1", New: "2"}}},
	}}}
	if _, err := r.Apply(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if string(got) != "package a\r\n\r\nvar x = 2\r\n" {
		t.Errorf("CRLF file rewritten as %q", got)
	}
}

func TestSweepReportSkippedVetHasNoVerdict(t *testing.T) {
	skipped, err := json.Marshal(&VetResult{Skipped: "--no-vet"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(skipped), `"passed"`) {
		t.Errorf("a skipped vet stage carries a verdict: %s", skipped)
	}
	failed, _ := json.Marshal(&VetResult{Ran: true})
	if !strings.Contains(string(failed), `"passed":false`) {
		t.Errorf("a failed vet stage dropped its verdict: %s", failed)
	}
}

func TestSweepReportFooterListsEveryConfiguredChange(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.settings.yml", `contracts:
  exempt: ["gen/**"]
  performance: warn
  security:
    exempt: ["legacy/**"]
  rules:
    GOFASTR1003: error
    GOFASTR1403:
      exempt: ["tpl/**"]
`)
	writeTemp(t, dir, "a.go", "package a\n")
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	registerTripAnalyzer()
	report, err := Run(mustPass(t, dir, cfg), RunOptions{Analyzers: []string{"test-trip"}})
	if err != nil {
		t.Fatal(err)
	}
	out := FormatText(report, TextOptions{})
	for _, want := range []string{
		"exempt path gen/**", "capability performance → warn", "capability security exempt at legacy/**",
		"rule GOFASTR1003 → error", "rule GOFASTR1403 exempt at tpl/**",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("footer omits %q:\n%s", want, out)
		}
	}
}

func TestSweepReportApplyRefusesAnInvertedEdit(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.txt", "abcdefgh\n")
	r := &Report{Root: dir, Diagnostics: []Diagnostic{{
		RuleID: RuleHTMLConcat, File: "a.txt", Line: 1,
		Fix: &SuggestedFix{Edits: []TextEdit{{File: "a.txt", Start: 5, End: 3, New: "x"}}},
	}}}
	if _, err := r.Apply(); err == nil {
		t.Error("an edit ending before it starts was applied")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "abcdefgh\n" {
		t.Errorf("file modified: %q", got)
	}
}

func TestSweepReportNarrowedCopyCannotReapplyTheBaseline(t *testing.T) {
	b := &Baseline{Schema: BaselineSchemaVersion, Counts: map[string]map[string]int{RuleHTMLConcat: {"a.go": 1}}}
	r := &Report{FailOn: SeverityError, Diagnostics: []Diagnostic{
		{RuleID: RuleHTMLConcat, File: "a.go", Line: 1, Severity: SeverityError},
		{RuleID: RuleHTMLConcat, File: "a.go", Line: 2, Severity: SeverityError},
	}}
	r.ApplyBaseline(b)
	if res := r.Only(RuleHTMLConcat).ApplyBaseline(b); res.Accepted != 0 {
		t.Errorf("a narrowed copy re-applied the baseline and absorbed %d", res.Accepted)
	}
}

func sweepWithheldReport() *Report {
	r := &Report{
		FailOn: SeverityError, Suppressed: 2, Baselined: 3, BaselineFixed: 1, OutsideChange: 4,
		Capabilities: []Capability{CapRouting},
	}
	r.summarize()
	return r
}

func TestSweepReportJSONAdmitsWhatItDidNotReport(t *testing.T) {
	blob, err := FormatJSON(sweepWithheldReport())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"suppressed": 2`, `"baselined": 3`, `"baselineFixed": 1`, `"outsideChange": 4`, `"diagnostics": []`, `"summary": []`} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("JSON lacks %s:\n%s", want, blob)
		}
	}
}

func TestSweepReportFooterAdmitsWhatItDidNotReport(t *testing.T) {
	out := FormatText(sweepWithheldReport(), TextOptions{})
	for _, want := range []string{"2 findings suppressed", "3 pre-existing findings", "1 baseline entry is now over-accepting", "4 findings outside the changed files", "only checked: routing"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer lacks %q:\n%s", want, out)
		}
	}
}
