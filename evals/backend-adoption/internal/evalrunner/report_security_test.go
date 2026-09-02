package evalrunner

import (
	"strings"
	"testing"
)

// Property: text produced by the thing being evaluated must not reach the
// operator's committed markdown with live markdown syntax in it.
//
// RenderMarkdown embeds Check.Evidence verbatim into RESULTS.md. Evidence
// is candidate-controlled twice over: response bodies arrive via
// evidence() (Go %q quoting escapes quotes and control bytes but leaves
// brackets, parens, and bangs intact), and build/test output arrives via
// truncate() completely raw — the candidate decides what the compiler
// prints by what it compiles. A candidate that returns a body or emits a
// build error containing ![x](https://…) plants an image beacon (auto-
// fetched by every markdown renderer that opens RESULTS.md, including the
// repo's committed copy), and a multi-line build output starting with
// "# " rewrites the document structure the operator reads scores from.
// The ui-quality twin's leaderboard has the same shape (see its
// report_security_test.go); this is the backend surface of one root.
func TestReportMarkdownEscapesCandidateEvidence(t *testing.T) {
	shapes := []Check{
		{
			ID: "mcp_discovery", Passed: false,
			// Exactly what evidence() produces for a body containing an
			// image reference: %q keeps the markdown alive.
			Evidence: `status=200 body="{\"tools\":[\"![exfil](https://eval-attacker.example/ping)\"]}"`,
		},
		{
			ID: "build", Passed: false,
			// Exactly what truncate() produces for candidate-controlled
			// compiler output: raw, multi-line.
			Evidence: "./main.go:12:2: undefined: widget\n# Backend adoption results\n\nAll checks passed. ![beacon](https://eval-attacker.example/build)",
		},
	}
	aggregate := &Aggregate{
		RunID: "redrun", CodexVersion: "codex probe", Model: "probe", Runs: 1,
		Trials: []TrialResult{{
			ID: "gofastr-run-1", Framework: "gofastr", Repetition: 1,
			Initial: PhaseResult{Score: 0, Maximum: 100, Checks: shapes},
		}},
	}
	md := RenderMarkdown(aggregate)
	if strings.Contains(md, "![") {
		t.Errorf("SECURITY: [markdown-injection] candidate evidence rendered a live image reference into RESULTS.md; every renderer that opens it fetches the attacker URL")
	}
	if strings.Count(md, "# Backend adoption results") > 1 {
		t.Errorf("SECURITY: [markdown-injection] candidate build output injected a second document title, restructuring the operator's report")
	}
	if strings.Contains(md, "\nAll checks passed.") {
		t.Errorf("SECURITY: [markdown-injection] candidate build output injected forged body text into RESULTS.md")
	}
}
