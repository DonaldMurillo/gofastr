package devcmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
	"github.com/DonaldMurillo/gofastr/internal/contractsrun"
)

type devContractWatch struct {
	root        string
	mu          sync.Mutex
	running     bool
	pending     bool
	lastSummary string
}

// NewDevContractWatch returns the contract report for the project at root.
func NewDevContractWatch(root string) *devContractWatch {
	return &devContractWatch{root: root}
}

// Run starts an analysis in the background and returns; calls during one queue a single trailing pass.
func (w *devContractWatch) Run() {
	w.mu.Lock()
	if w.running {
		w.pending = true
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		for {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						cli.Warn("contracts: %s", textsafe.Recovered(rec))
					}
				}()
				w.analyse()
			}()
			w.mu.Lock()
			if !w.pending {
				w.running = false
				w.mu.Unlock()
				return
			}
			w.pending = false
			w.mu.Unlock()
		}
	}()
}

func (w *devContractWatch) analyse() {
	baselinePath := contracts.BaselineFileName
	if w.root != "" && w.root != "." {
		baselinePath = strings.TrimSuffix(w.root, "/") + "/" + baselinePath
	}
	report, err := contractsrun.RunPipeline(w.root, baselinePath, contracts.RunOptions{}, true)
	if err != nil {
		w.printErrOnce(err)
		return
	}
	var note string
	files, cerr := contracts.ChangedFiles(w.root, "")
	switch {
	case cerr != nil:
		note = fmt.Sprintf("    changed-set unavailable (%v); showing the whole tree\n", cerr)
	case files != nil:
		report.RestrictTo(files)
	}

	if len(report.Diagnostics) == 0 {
		w.mu.Lock()
		hadFindings := w.lastSummary != ""
		w.lastSummary = ""
		w.mu.Unlock()
		if hadFindings {
			cli.Success("contracts: clean")
		}
		return
	}
	w.printOnce(note + w.Summarise(report))
}

func (w *devContractWatch) printErrOnce(err error) {
	key := "err:" + err.Error()
	w.mu.Lock()
	if key == w.lastSummary {
		w.mu.Unlock()
		return
	}
	w.lastSummary = key
	w.mu.Unlock()
	cli.Warn("contracts: %v", err)
}

// Summarise renders r as one line per finding, capped at devContractMaxLines.
func (w *devContractWatch) Summarise(r *contracts.Report) string {
	var b strings.Builder
	shown := 0
	for _, d := range r.Diagnostics {
		if shown == devContractMaxLines {
			fmt.Fprintf(&b, "    … %d more; run `gofastr verify --changed`\n",
				len(r.Diagnostics)-shown)
			break
		}
		fmt.Fprintf(&b, "    %s  %s  %s\n", d.RuleID, textsafe.ScrubTerminal(d.Location()), textsafe.ScrubTerminal(d.Message))
		shown++
	}
	fmt.Fprintf(&b, "    %s", "explain any of these: gofastr verify --explain <rule>")
	if rule, ok := firstFixableRule(r); ok {
		fmt.Fprintf(&b, "\n    fix %s: gofastr verify --rule %s --fix", rule, rule)
	}
	return b.String()
}

func firstFixableRule(r *contracts.Report) (string, bool) {
	for _, d := range r.Diagnostics {
		if d.Fix != nil && len(d.Fix.Edits) > 0 {
			return d.RuleID, true
		}
	}
	return "", false
}

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
	cli.Warn("contracts: findings in what you changed:")
	cli.Println(summary)
}
