package analyzers_test

import (
	"os"
	"testing"
)

// TestMain drops the coverage-dir overrides. Every test here runs the
// analyzers over a fixture whose own .gofastr/ holds the manifest it means;
// cmd/affected sets GOFASTR_AXE_COVERAGE_DIR for the whole sweep, and the
// testing analyzer would read the repo's real manifest from there instead.
func TestMain(m *testing.M) {
	os.Unsetenv("GOFASTR_AXE_COVERAGE_DIR")
	os.Unsetenv("GOFASTR_SEMANTIC_COVERAGE_DIR")
	os.Exit(m.Run())
}
