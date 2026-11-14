// primitives-eval measures whether the primitives registry carries what
// code comments carried. See evals/primitives/README.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type listFlag []string

func (f *listFlag) String() string     { return strings.Join(*f, ",") }
func (f *listFlag) Set(v string) error { *f = append(*f, v); return nil }

type config struct {
	repo, base, head string
	claude, model    string
	work, out        string
	timeout          time.Duration
	cellBudget       float64
}

func main() {
	var tasksFilter, conditionsFilter listFlag
	cfg := config{}
	flag.StringVar(&cfg.base, "base", "origin/main", "ref for the comments condition")
	flag.StringVar(&cfg.head, "head", "HEAD", "ref for the registry and bare conditions")
	flag.StringVar(&cfg.claude, "claude", "claude", "Claude Code CLI program")
	flag.StringVar(&cfg.model, "model", "sonnet", "model for every cell")
	flag.StringVar(&cfg.work, "work", "", "directory for cell workspaces, outside the repository (default: a new temp dir)")
	flag.StringVar(&cfg.out, "out", "", "artifact directory (default: dist/primitives-eval/<timestamp>)")
	flag.DurationVar(&cfg.timeout, "timeout", 20*time.Minute, "timeout per cell")
	flag.Float64Var(&cfg.cellBudget, "cell-budget", 2, "most one cell may spend, in USD")
	budget := flag.Float64("budget", 10, "stop starting cells once the run has spent this much, in USD")
	runs := flag.Int("runs", 1, "trials per task and condition")
	concurrency := flag.Int("concurrency", 3, "cells running at once")
	flag.Var(&tasksFilter, "task", "run only this task (repeatable)")
	flag.Var(&conditionsFilter, "condition", "run only this condition (repeatable)")
	flag.Parse()

	if err := run(cfg, tasksFilter, conditionsFilter, *runs, *concurrency, *budget); err != nil {
		fmt.Fprintf(os.Stderr, "primitives-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg config, tasksFilter, conditionsFilter []string, runs, concurrency int, budget float64) error {
	repo, err := gitOutput(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	cfg.repo = repo
	stamp := time.Now().UTC().Format("20060102-150405")
	if cfg.out == "" {
		cfg.out = filepath.Join(repo, "dist", "primitives-eval", stamp)
	}
	if cfg.work == "" {
		if cfg.work, err = os.MkdirTemp("", "primitives-eval-"); err != nil {
			return err
		}
	}
	if rel, err := filepath.Rel(repo, cfg.work); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("-work %s is inside the repository: a cell there would read this checkout's CLAUDE.md", cfg.work)
	}
	if err := os.MkdirAll(cfg.out, 0o700); err != nil {
		return err
	}

	var cells []*cell
	for _, t := range tasks {
		if len(tasksFilter) > 0 && !slices.Contains(tasksFilter, t.ID) {
			continue
		}
		for _, c := range conditions {
			if len(conditionsFilter) > 0 && !slices.Contains(conditionsFilter, c) {
				continue
			}
			for n := 1; n <= runs; n++ {
				cells = append(cells, &cell{Task: t.ID, Condition: c, Run: n, task: t})
			}
		}
	}
	if len(cells) == 0 {
		return fmt.Errorf("no cell matches the -task and -condition filters")
	}

	var (
		mu    sync.Mutex
		spent float64
		wg    sync.WaitGroup
		slots = make(chan struct{}, max(concurrency, 1))
	)
	for _, c := range cells {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			mu.Lock()
			over := spent >= budget
			mu.Unlock()
			if over {
				c.Outcome, c.Note = outcomeSkipped, "run budget reached"
				return
			}
			runCell(cfg, c)
			mu.Lock()
			spent += c.CostUSD
			mu.Unlock()
			fmt.Printf("%-16s %-9s run %d: %-8s $%.2f %s\n", c.Task, c.Condition, c.Run, c.Outcome, c.CostUSD, c.Note)
		}()
	}
	wg.Wait()

	report := report{Model: cfg.model, Base: cfg.base, Head: cfg.head, Started: stamp, SpentUSD: spent, Cells: cells}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.out, "results.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.out, "RESULTS.md"), []byte(renderMarkdown(report)), 0o600); err != nil {
		return err
	}
	fmt.Printf("\nspent $%.2f; results in %s\n", spent, cfg.out)
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func command(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 10 * time.Second
	return cmd
}
