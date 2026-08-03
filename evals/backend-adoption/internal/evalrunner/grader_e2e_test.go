package evalrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGradeExercisesBuiltHTTPBoundary pins the evaluator's most important
// behavior: it must compile and launch the candidate, then distinguish a real
// HTTP success from an unimplemented route. This is deliberately a tiny,
// incomplete candidate rather than a mock of grader internals.
func TestGradeExercisesBuiltHTTPBoundary(t *testing.T) {
	workspace := t.TempDir()
	resultDir := filepath.Join(workspace, "results")
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod": "module eval.test/minimal\n\ngo 1.24.2\n",
		"main.go": `package main

import (
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = http.ListenAndServe(os.Getenv("PORT"), nil)
}
`,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Windows may be compiling other repository packages concurrently. Keep
	// enough room for the candidate build/test subprocesses to start without
	// turning a scheduler delay into a process-tree cleanup failure.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result := Grade(ctx, workspace, resultDir, false)
	if !result.BuildOK || !result.TestOK {
		t.Fatalf("minimal candidate did not pass compile gates: %+v", result)
	}
	if !checkPassed(result.Checks, "health") {
		t.Fatalf("real /healthz response was not accepted: %+v", result.Checks)
	}
	if checkPassed(result.Checks, "register") {
		t.Fatalf("missing /auth/register route was incorrectly accepted: %+v", result.Checks)
	}
	if result.Score >= result.Maximum {
		t.Fatalf("incomplete candidate received a perfect score: %d/%d", result.Score, result.Maximum)
	}
}

func checkPassed(checks []Check, id string) bool {
	for _, check := range checks {
		if check.ID == id {
			return check.Passed
		}
	}
	return false
}
