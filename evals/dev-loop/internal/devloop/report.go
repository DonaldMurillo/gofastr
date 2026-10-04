package devloop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// writeReport tallies r.Passed and r.TasksDone, then writes results.json
// and RESULTS.md.
func writeReport(runDir string, r *Report) error {
	r.Passed, r.TasksDone = 0, 0
	for _, t := range r.Trials {
		if t.Verdict.Pass {
			r.Passed++
		}
		if t.Verdict.TaskDone {
			r.TasksDone++
		}
	}
	if err := writeJSON(filepath.Join(runDir, "results.json"), r); err != nil {
		return err
	}
	return fileperm.WriteOwnerOnly(filepath.Join(runDir, "RESULTS.md"), []byte(markdown(*r)))
}

func markdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Dev-loop eval\n\nModel `%s`: **%d of %d** trials developed under `gofastr dev`. %d of %d finished the task.\n\n",
		r.Model, r.Passed, len(r.Trials), r.TasksDone, len(r.Trials))
	b.WriteString("| Trial | Dev loop | Task done | `gofastr dev` launches | `go run` | Binary launches | Dev rebuilds | Cost (USD) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, t := range r.Trials {
		s := t.Signals
		fmt.Fprintf(&b, "| %02d | %t | %t | %d | %d | %d | %d | %.2f |\n",
			t.Index, t.Verdict.Pass, t.Verdict.TaskDone, s.DevLaunches, len(s.GoRuns), len(s.BinaryLaunches), s.DevRebuilds, t.CostUSD)
	}
	for _, t := range r.Trials {
		if t.Verdict.Pass && t.Verdict.TaskDone && len(t.Issues) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## Trial %02d\n\n", t.Index)
		for _, f := range t.Verdict.Failures {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		for _, f := range t.Verdict.TaskFailures {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		for _, issue := range t.Issues {
			fmt.Fprintf(&b, "- technical: %s\n", issue)
		}
		for _, run := range append(append([]string(nil), t.Signals.GoRuns...), t.Signals.BinaryLaunches...) {
			fmt.Fprintf(&b, "- bypass: `%s`\n", strings.ReplaceAll(run, "`", "'"))
		}
	}
	return b.String()
}
