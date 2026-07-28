package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/evals/backend-adoption/internal/evalrunner"
)

type frameworkFlags []string

func (f *frameworkFlags) String() string { return strings.Join(*f, ",") }
func (f *frameworkFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func main() {
	var frameworks frameworkFlags
	runs := flag.Int("runs", 2, "independent trials per framework")
	concurrency := flag.Int("concurrency", 2, "maximum concurrent Codex processes")
	timeout := flag.Duration("timeout", 25*time.Minute, "timeout per Codex phase")
	model := flag.String("model", "", "Codex model; empty uses the CLI default")
	codex := flag.String("codex", "codex", "Codex CLI program")
	artifacts := flag.String("artifacts", "", "artifact root (default dist/backend-adoption)")
	regrade := flag.String("regrade-maintenance", "", "completed run directory to regrade without new agent calls")
	flag.Var(&frameworks, "framework", "framework to evaluate; repeat for multiple")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	if err != nil {
		fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *regrade != "" {
		result, regradeErr := evalrunner.RegradeMaintenance(ctx, *regrade)
		if result != nil {
			fmt.Print(evalrunner.RenderMarkdown(result))
		}
		if regradeErr != nil {
			fatal(regradeErr)
		}
		return
	}
	result, err := evalrunner.Run(ctx, evalrunner.Config{
		RepoRoot: repoRoot, ArtifactDir: *artifacts, Codex: *codex,
		Model: *model, Runs: *runs, Concurrency: *concurrency,
		Timeout: *timeout, Frameworks: frameworks,
	})
	if result != nil {
		resultPath := filepath.Join(defaultArtifactDir(repoRoot, *artifacts), result.RunID, "RESULTS.md")
		fmt.Printf("Results: %s\n", resultPath)
		fmt.Print(evalrunner.RenderMarkdown(result))
	}
	if err != nil {
		fatal(err)
	}
}

func findRepoRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not find repository root")
		}
		current = parent
	}
}

func defaultArtifactDir(repoRoot, configured string) string {
	if configured != "" {
		return configured
	}
	return filepath.Join(repoRoot, "dist", "backend-adoption")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "backend-eval:", err)
	os.Exit(1)
}
