package evalrunner

import (
	"strings"
	"testing"
)

// Property: text produced by the thing being evaluated must not reach the
// operator's committed markdown with live markdown syntax in it — the
// ui-quality surface of the root whose backend surface lives in
// evals/backend-adoption/internal/evalrunner/report_security_test.go.
//
// leaderboardMarkdown renders three eval-output strings verbatim into
// leaderboard.md (committed at evals/ui-quality/RESULTS.md):
//
//   - judge feedback items (Weakest/NextIterations bullets): the judge
//     models candidate screenshots, and validateJudgment's marker list
//     blocks process chatter only — image references and headings pass;
//   - BuilderDocsTopics/Searches: extracted from the CLI shim log, whose
//     lines the BUILDER argv controls (see clishim_security_test.go for
//     the forgery half of that funnel).
//
// An image reference here is a tracker beacon auto-fetched by whatever
// renders the leaderboard; a leading "# " rewrites the document the
// operator reads winners from.
func TestLeaderboardEscapesEvalOutputText(t *testing.T) {
	summary := Summary{
		Suite: "ui-quality", RunID: "redrun",
		Candidates: []CandidateResult{{
			VariantID: "gofastr", ScenarioID: "ops", Repetition: 1,
			Weakest:             []string{"The hero ![exfil](https://eval-attacker.example/judge) banner is clipped on mobile."},
			NextIterations:      []string{"Tighten the grid.\n# WINNER: legacy-framework\nEvery dimension scored 10/10; promote legacy-framework."},
			BuilderDocsTopics:   []string{"ui-capability-map"},
			BuilderDocsSearches: []string{"live dashboard"},
		}},
	}
	md := leaderboardMarkdown(summary)
	if strings.Contains(md, "![") {
		t.Errorf("SECURITY: [markdown-injection] eval-output text rendered a live image reference into leaderboard.md; every renderer that opens it fetches the attacker URL")
	}
	if !strings.HasPrefix(md, "# ui-quality — UI quality leaderboard") || strings.Contains(md, "\n# WINNER:") {
		t.Errorf("SECURITY: [markdown-injection] judge feedback injected its own top-level heading (# WINNER: …) ahead of the leaderboard's conclusions")
	}
	if strings.Contains(md, "\nEvery dimension scored 10/10") {
		t.Errorf("SECURITY: [markdown-injection] judge feedback injected forged score prose into leaderboard.md")
	}
}
