package testcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── test_cmd.go ───────────────────────────────────────────────────────

func TestColorizeTestLineAllBranches(t *testing.T) {
	var p, f int
	clitest.CovT_withTTY(func() {
		lines := []string{
			"=== RUN   TestX",
			"--- PASS: TestX (0.00s)",
			"--- FAIL: TestY (0.00s)",
			"ok  pkg 0.1s",
			"FAIL pkg",
			"PASS",
			"random line",
		}
		for _, l := range lines {
			_ = colorizeTestLine(l, &p, &f)
		}
	})
	if p != 1 || f != 1 {
		t.Fatalf("pass=%d fail=%d", p, f)
	}
}
