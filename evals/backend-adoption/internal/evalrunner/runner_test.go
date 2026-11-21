package evalrunner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Regression: more trial failures than cfg.Concurrency used to deadlock
// Run — workers blocked sending on the errs buffer while the main loop
// waited for results to close before draining it.
func TestRunReportsAllTrialFailuresBeyondConcurrency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/bin/echo stands in for the codex binary")
	}
	repoRoot := t.TempDir()
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		RepoRoot: repoRoot,
		// No evals/backend-adoption/TASK.md under this root: every trial
		// fails at the task copy, long before codex runs.
		ArtifactDir: t.TempDir(),
		Codex:       "/bin/echo",
		CodexHome:   codexHome,
		Runs:        2,
		Concurrency: 2,
		Timeout:     time.Minute,
		Frameworks:  []string{"gin", "stdlib"},
	}
	done := make(chan *Aggregate, 1)
	failures := make(chan error, 1)
	go func() {
		aggregate, err := Run(context.Background(), cfg)
		done <- aggregate
		failures <- err
	}()
	select {
	case err := <-failures:
		if err == nil {
			t.Fatal("every trial failed, want the failure report")
		}
		if !strings.Contains(err.Error(), "4 trial(s) failed") {
			t.Fatalf("failure report = %q, want all 4 trials counted", err)
		}
		if aggregate := <-done; aggregate == nil || len(aggregate.Trials) != 0 {
			t.Fatalf("aggregate = %+v, want it returned with no trials", aggregate)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Run deadlocked: trial failures beyond the Concurrency buffer never reached the report")
	}
}
