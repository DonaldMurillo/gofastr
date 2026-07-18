package framework

import (
	"strings"
	"testing"
)

// This file adds unit coverage for the PURE probe-report rendering helpers
// in processmodule_probe.go (ProbeID.String/Title, ProbeStatus.String,
// ConformanceReport.Result/Summary, denialDetail, tailForDetail) and the
// pure splitCSV helper in processmodule_probe_unix.go. The conformance
// suite itself spawns children and is environment-gated (covered elsewhere).

// ---- ProbeID.String / Title ----

func TestProbeID_StringOutOfRange(t *testing.T) {
	if got := ProbeID(0).String(); got != "Probe(0)" {
		t.Errorf("ProbeID(0).String() = %q", got)
	}
	if got := ProbeID(99).String(); got != "Probe(99)" {
		t.Errorf("ProbeID(99).String() = %q", got)
	}
	// Every declared probe renders as P1..P7.
	for _, p := range allProbes {
		if got := p.String(); !strings.HasPrefix(got, "P") {
			t.Errorf("%s.String() = %q, want P-prefix", p, got)
		}
	}
}

func TestProbeID_TitleDefault(t *testing.T) {
	// An unknown probe ID falls back to its String() form.
	if got := ProbeID(99).Title(); got != "Probe(99)" {
		t.Errorf("unknown Title = %q", got)
	}
	// Every declared probe has a distinct human-readable title.
	for _, p := range allProbes {
		if got := p.Title(); got == "" || got == p.String() {
			t.Errorf("%s.Title() = %q, want a label", p, got)
		}
	}
}

// ---- ProbeStatus.String ----

func TestProbeStatus_StringAll(t *testing.T) {
	cases := []struct {
		s    ProbeStatus
		want string
	}{
		{ProbeStatusPass, "pass"},
		{ProbeStatusFail, "fail"},
		{ProbeStatusUnreachable, "unreachable"},
		{ProbeStatusUnknown, "unknown"},
		{ProbeStatus(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("status(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

// ---- ConformanceReport.Result ----

func TestConformanceReport_ResultKnownAndUnknown(t *testing.T) {
	rep := ConformanceReport{
		Backend:   "fake",
		Available: true,
		Results:   []ProbeResult{{ID: ProbeNoInheritedSecret, Status: ProbeStatusPass, Detail: "denied"}},
	}
	got := rep.Result(ProbeNoInheritedSecret)
	if got.Status != ProbeStatusPass || got.Detail != "denied" {
		t.Errorf("known Result = %+v", got)
	}
	// Unknown probe → zero-value result with Unknown status.
	unk := rep.Result(ProbeNoPrivReEscalation)
	if unk.Status != ProbeStatusUnknown || unk.ID != ProbeNoPrivReEscalation {
		t.Errorf("unknown Result = %+v", unk)
	}
}

// ---- ConformanceReport.Summary ----

func TestConformanceReport_SummaryShapes(t *testing.T) {
	// Unavailable backend includes the missing reason.
	unavail := ConformanceReport{Backend: "bwrap", Available: false, MissingReason: "bwrap not on PATH"}.Summary()
	if !strings.Contains(unavail, "available=false") || !strings.Contains(unavail, "bwrap not on PATH") {
		t.Errorf("unavailable summary = %q", unavail)
	}
	if !strings.Contains(unavail, "NOT CONFORMING") {
		t.Errorf("unavailable summary missing NOT CONFORMING: %q", unavail)
	}
	// A fully-passing report (all 7 probes) includes the CONFORMS line + a
	// per-probe detail line. Conforms() requires len(Results) >= len(allProbes).
	results := make([]ProbeResult, 0, len(allProbes))
	for _, p := range allProbes {
		detail := ""
		if p == ProbeDistinctPrincipal {
			detail = "uid mismatch observed"
		}
		results = append(results, ProbeResult{ID: p, Status: ProbeStatusPass, Detail: detail})
	}
	passing := ConformanceReport{Backend: "fake", Available: true, Results: results}.Summary()
	if !strings.Contains(passing, "CONFORMS") {
		t.Errorf("passing summary missing CONFORMS: %q", passing)
	}
	if !strings.Contains(passing, "uid mismatch observed") {
		t.Errorf("passing summary missing detail line: %q", passing)
	}
}
func TestDenialDetail_emptyAndNonEmpty(t *testing.T) {
	if got := denialDetail(ProbeNoNetworkEgress, ""); got != "" {
		t.Errorf("denialDetail('') = %q, want empty", got)
	}
	got := denialDetail(ProbeNoNetworkEgress, "permission denied")
	if !strings.Contains(got, "denial observed") || !strings.Contains(got, "permission denied") {
		t.Errorf("denialDetail = %q", got)
	}
}

func TestTailForDetail_shortAndLong(t *testing.T) {
	if got := tailForDetail("short"); got != "short" {
		t.Errorf("tailForDetail(short) = %q", got)
	}
	long := strings.Repeat("x", 300)
	got := tailForDetail(long)
	// "…" is 3 UTF-8 bytes; tail is the last 200 bytes → total 203.
	if !strings.HasPrefix(got, "…") || len(got) != 203 {
		t.Errorf("tailForDetail(long) = len %d, want ellipsis (3 bytes) + 200 = 203", len(got))
	}
}

// ---- splitCSV (probe_unix pure helper) ----

func TestSplitCSV_pure(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , ", []string{"a", "b"}}, // trims + drops empties
		{" , , ", nil},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitCSV(%q) = %+v, want %+v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
