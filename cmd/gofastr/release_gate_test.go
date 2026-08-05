package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workflowSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// v0.61.0 was published from a commit whose blocking CI check had FAILED:
// ci.yml never ran on tag pushes and release.yml's only gate was a changelog
// section existing. These two tests pin the closed hole. First: pushing a
// v* tag must start the CI workflow, so the tag SHA gets its own blocking
// check runs for the release gate to consult.
func TestCIRunsOnReleaseTags(t *testing.T) {
	ci := workflowSource(t, "ci.yml")
	if !strings.Contains(ci, "tags: ['v*']") {
		t.Fatal("ci.yml does not trigger on v* tag pushes — a tag SHA gets no check runs of its own and the release gate has nothing to consult")
	}
}

// Second: release.yml must refuse to create the GitHub release until every
// blocking check run on the exact tag commit has completed successfully.
func TestReleaseGatesOnGreenBlockingChecks(t *testing.T) {
	rel := workflowSource(t, "release.yml")
	for _, want := range []string{
		"check-runs",             // queries the tag commit's check runs
		`\\(blocking\\)`,         // filters to the blocking jobs
		"blocking checks failed", // red conclusion aborts the release
	} {
		if !strings.Contains(rel, want) {
			t.Fatalf("release.yml does not gate on the tag SHA's blocking check runs (missing %q) — a tag cut from a red commit publishes anyway", want)
		}
	}
}
