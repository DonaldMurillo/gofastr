// Command devloop-eval runs the dev-loop eval: does a coding agent in a
// fresh GoFastr scaffold develop under `gofastr dev`? See
// evals/dev-loop/README.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr/evals/dev-loop/internal/devloop"
)

func main() {
	var (
		runs    = flag.Int("runs", 3, "independent agent trials")
		model   = flag.String("model", "opus", "Claude model for the agent")
		claude  = flag.String("claude-bin", "claude", "Claude Code executable")
		timeout = flag.Duration("timeout", 20*time.Minute, "per-trial agent timeout")
		out     = flag.String("out", "", "run directory (default dist/dev-loop-eval/<UTC timestamp>)")
		regrade = flag.String("regrade", "", "re-grade an existing run directory without launching agents")
	)
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var (
		report devloop.Report
		runDir string
		err    error
	)
	if *regrade != "" {
		runDir = *regrade
		report, err = devloop.Regrade(ctx, runDir)
	} else {
		repoRoot, wdErr := os.Getwd()
		if wdErr != nil {
			fail(wdErr)
		}
		if _, statErr := os.Stat(filepath.Join(repoRoot, "cmd", "gofastr")); statErr != nil {
			fail(fmt.Errorf("run from the repository root: %w", statErr))
		}
		runDir = *out
		if runDir == "" {
			runDir = filepath.Join(repoRoot, "dist", "dev-loop-eval", time.Now().UTC().Format("20060102T150405Z"))
		}
		if runDir, err = filepath.Abs(runDir); err != nil {
			fail(err)
		}
		report, err = devloop.Run(ctx, devloop.Options{
			RepoRoot: repoRoot, RunDir: runDir, Runs: *runs,
			Model: *model, ClaudeBin: *claude, Timeout: *timeout,
		})
	}
	if err != nil {
		fail(err)
	}
	fmt.Printf("%d of %d trials developed under gofastr dev; %d finished the task. Report: %s\n",
		report.Passed, len(report.Trials), report.TasksDone, filepath.Join(runDir, "RESULTS.md"))
	if report.Passed != len(report.Trials) {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "devloop-eval:", err)
	os.Exit(2)
}
