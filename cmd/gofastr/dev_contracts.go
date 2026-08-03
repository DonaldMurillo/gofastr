package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// devContractWatch reports contract findings in the dev loop, after the
// reload rather than before it.
//
// The RFC's long-term goal is inline diagnostics as you type. This is the
// practical approximation: you already save and wait for a rebuild, so
// the findings arrive in that same beat — but they must not delay it.
// Analysis takes about a second on a large tree, and a second added to
// every save is the difference between a loop people use and one they
// turn off. So the server restarts first, this runs behind it, and the
// report prints when it is ready.
//
// Scoped with --changed semantics: a dev loop that prints findings from
// code you have not touched is noise. The analysis still runs whole-tree
// (route tables and entity lists are only meaningful whole); only the
// reporting narrows.
type devContractWatch struct {
	root string
	// mu guards running, so a burst of saves does not stack analyses.
	mu      sync.Mutex
	running bool
	// lastSummary suppresses reprinting an unchanged report. Saving a
	// file with the same findings should be quiet, or the loop trains you
	// to ignore it.
	lastSummary string
}

func newDevContractWatch(root string) *devContractWatch {
	return &devContractWatch{root: root}
}

// Run analyses in the background and prints any findings. It returns
// immediately; a run already in flight is skipped rather than queued,
// because the newer save supersedes it anyway.
func (w *devContractWatch) Run() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
		}()
		// A panic in an analyzer must never take the dev server with it.
		defer func() {
			if rec := recover(); rec != nil {
				warn("contracts: %v", rec)
			}
		}()
		w.analyse()
	}()
}

func (w *devContractWatch) analyse() {
	// Every failure of the analysis ITSELF is reported, once. A silent
	// return here leaves the loop printing nothing while the analyzers
	// have not actually looked — and in a loop, nothing reads as "clean".
	cfg, err := contracts.LoadConfig(w.root, "")
	if err != nil {
		w.printErrOnce(err)
		return
	}
	pass, err := contracts.NewPass(w.root, cfg)
	if err != nil {
		w.printErrOnce(err)
		return
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		w.printErrOnce(err)
		return
	}
	baseline, berr := contracts.ReadBaseline(devBaselinePath(w.root))
	if berr != nil {
		// Matching `gofastr verify`, which fails hard here: skipping an
		// unreadable baseline would flood the loop with the very debt it
		// records, and reporting against a state nobody chose is worse
		// than saying why nothing can be reported.
		w.printErrOnce(berr)
		return
	}
	if baseline != nil {
		report.ApplyBaseline(baseline)
	}
	// Narrow to what is actually being worked on. Outside a repository
	// this returns nil and nothing narrows, which is the right fallback.
	// A repository where the changed-set CANNOT be computed — no commits
	// yet, a broken index — also falls back to the whole tree, but says
	// so: silently widening the report is how "what did I just break"
	// becomes "what is wrong with everything".
	var note string
	files, cerr := contracts.ChangedFiles(w.root, "")
	switch {
	case cerr != nil:
		note = fmt.Sprintf("    changed-set unavailable (%v) — showing the whole tree\n", cerr)
	case files != nil:
		report.RestrictTo(files)
	}

	if len(report.Diagnostics) == 0 {
		w.mu.Lock()
		hadFindings := w.lastSummary != ""
		w.lastSummary = ""
		w.mu.Unlock()
		if hadFindings {
			success("contracts: clean")
		}
		return
	}
	w.printOnce(note + w.summarise(report))
}

// printErrOnce reports a failure of the analysis itself — config, scan,
// run, baseline — once per distinct message, under a plain warning
// rather than the findings header. The "err:" key keeps an error from
// colliding with a findings summary in the dedupe, and a later clean or
// failing run replaces it, so recovery is announced like any other
// transition.
func (w *devContractWatch) printErrOnce(err error) {
	key := "err:" + err.Error()
	w.mu.Lock()
	if key == w.lastSummary {
		w.mu.Unlock()
		return
	}
	w.lastSummary = key
	w.mu.Unlock()
	warn("contracts: %v", err)
}

// summarise renders the compact dev-loop form: one line per finding, with
// the rule ID so `gofastr verify --explain` is one copy-paste away. The
// full report — reasons, examples, fixes — is what `gofastr verify` is
// for; repeating it on every save would bury the loop.
func (w *devContractWatch) summarise(r *contracts.Report) string {
	var b strings.Builder
	shown := 0
	for _, d := range r.Diagnostics {
		if shown == devContractMaxLines {
			fmt.Fprintf(&b, "    … %d more — run `gofastr verify --changed`\n",
				len(r.Diagnostics)-shown)
			break
		}
		fmt.Fprintf(&b, "    %s  %s  %s\n", d.RuleID, d.Location(), d.Message)
		shown++
	}
	fmt.Fprintf(&b, "    %s", "explain any of these: gofastr verify --explain <rule>")
	// Only offer the fix when one is actually on the table. Suggesting it
	// against a report full of rules that have no autofix teaches people
	// the hint is noise.
	if rule, ok := firstFixableRule(r); ok {
		fmt.Fprintf(&b, "\n    fix %s: gofastr verify --rule %s --fix", rule, rule)
	}
	return b.String()
}

// firstFixableRule returns the rule ID of the first finding carrying a
// mechanical fix, in the order they are reported.
func firstFixableRule(r *contracts.Report) (string, bool) {
	for _, d := range r.Diagnostics {
		if d.Fix != nil && len(d.Fix.Edits) > 0 {
			return d.RuleID, true
		}
	}
	return "", false
}

// devContractMaxLines caps the dev-loop report. A wall of findings after
// a save is something you scroll past, not something you read.
const devContractMaxLines = 8

func (w *devContractWatch) printOnce(summary string) {
	w.mu.Lock()
	if summary == w.lastSummary {
		w.mu.Unlock()
		return
	}
	w.lastSummary = summary
	w.mu.Unlock()

	fmt.Println()
	warn("contracts — findings in what you changed:")
	fmt.Println(summary)
}

// devBaselinePath is the conventional baseline location under root.
func devBaselinePath(root string) string {
	if root == "" || root == "." {
		return contracts.BaselineFileName
	}
	return strings.TrimSuffix(root, "/") + "/" + contracts.BaselineFileName
}
