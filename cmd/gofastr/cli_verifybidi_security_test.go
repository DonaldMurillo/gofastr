package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/devcmd"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// hostileRunes is the open part of the F25 class: ESC/C0 is already stripped
// by sanitizeText (asserted below as the pin that holds); these are the
// code points that pass today.
var hostileRunes = []struct {
	name string
	r    rune
}{
	{"RLO U+202E", '\u202E'},
	{"LRI U+2066", '\u2066'},
	{"CSI-8bit U+009B", '\u009B'},
	{"OSC-8bit U+009D", '\u009D'},
	{"ZWSP U+200B", '\u200B'},
	{"BOM U+FEFF", '\uFEFF'},
}

// verifyHostileLine triggers RuleBespokeEventSource while carrying every
// hostile rune on the reported line.
const verifyHostileLine = "const es = new EventSource(\"/x\u202Erevo\u2066" +
	"\u009Bcmd\u009D\u200Bhidden\uFEFF\"); // stream"

func assertNoHostile(t *testing.T, label, out string) {
	t.Helper()
	for _, h := range hostileRunes {
		if strings.ContainsRune(out, h.r) {
			t.Errorf("SECURITY: [injection] %s output carries %s from scanned repo content", label, h.name)
		}
	}
}

// TestVerifyReportScrubBidiAndC1 exercises both print boundaries for the one
// property: the full `gofastr verify` text report (surface 1, end-to-end
// through runVerify) and the dev loop's per-save summary (surface 2, the
// summarise print boundary the dev watcher calls after every reload).
func TestVerifyReportScrubBidiAndC1(t *testing.T) {
	dir := clitest.WriteModule(t, map[string]string{
		// Only .go/.css files are discovered; the EventSource trigger
		// lives inside a Go string literal, the shape the rule exists for.
		"stream.go": "package main\n\nconst js = `" + verifyHostileLine + "`\n",
	})

	// Surface 1: `gofastr verify` text report.
	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet"})

	// Non-vacuous: the fixture must have produced the finding, so the
	// snippet print path actually ran over the hostile line.
	if !strings.Contains(out, "EventSource") {
		t.Fatalf("fixture check: verify reported no EventSource finding; the hostile snippet never reached the printer:\n%s", out)
	}
	// The pin that holds today: ESC/C0 is stripped at this boundary.
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("regression: ESC reached verify's terminal output")
	}
	// The finding: the rest of the class is not.
	assertNoHostile(t, "verify report", out)
	// Surface 2: the dev loop summary prints Location and Message with no
	// scrub at all. Built from the same diagnostic shape the analyzers
	// emit (source-derived Message detail + repo path).
	rule, ok := contracts.LookupRule(contracts.RuleBespokeEventSource)
	if !ok {
		t.Fatal("rule missing from catalog")
	}
	report := &contracts.Report{FailOn: contracts.SeverityWarn}
	report.Diagnostics = append(report.Diagnostics, contracts.Diagnostic{
		RuleID:  rule.ID,
		Slug:    rule.Slug,
		File:    "ev\u202Eil.js",
		Line:    1,
		Message: "bespoke EventSource: matched `new EventSource(\x1b[31m\u009B\u202E)`" + verifyHostileLine,
	})
	w := devcmd.NewDevContractWatch(".")
	summary := w.Summarise(report)

	if !strings.Contains(summary, "EventSource") {
		t.Fatalf("fixture check: dev summary lost the finding entirely:\n%s", summary)
	}
	if strings.ContainsRune(summary, 0x1b) {
		t.Errorf("SECURITY: [injection] dev-loop summary passes ESC (C0) raw from repo content; verify's boundary strips it, this one has no scrub")
	}
	assertNoHostile(t, "dev-loop summary", summary)
}
