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
// release.yml's only gate was a changelog section existing. These two tests
// pin the closed hole. First: the release gate consults check runs on the
// tag's commit SHA, and those come from the main-push CI run (a release tag
// points at a merge commit on main), so ci.yml must run on main pushes —
// and must NOT also trigger on v* tags, which re-ran the identical pipeline
// on the same SHA (v0.62.0 ran twice, concurrently). A tag on a commit with
// no CI run finds zero check runs and the gate times out red, so dropping
// the tag trigger keeps the gate fail-closed.
func TestMainPushCICoversReleaseTags(t *testing.T) {
	ci := workflowSource(t, "ci.yml")
	if !strings.Contains(ci, "branches: [main]") {
		t.Fatal("ci.yml does not run on main pushes — the merge commit a release tag points at gets no check runs and the release gate has nothing to consult")
	}
	if strings.Contains(ci, "tags:") {
		t.Fatal("ci.yml triggers on tag pushes — that duplicates the main-push run on the same SHA; the release gate already consults those checks")
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
