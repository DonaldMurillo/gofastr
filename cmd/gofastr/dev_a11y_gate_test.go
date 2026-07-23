package main

import (
	"os/exec"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// The dev loop enforces the same static a11y gate `gofastr build` runs:
// a rebuild of a tree with findings must not start the server, and the
// same --no-a11y escape hatch must skip the gate entirely.

func TestDevRebuildBlocksOnA11yFindings(t *testing.T) {
	root := writeA11yTree(t, map[string]string{
		"app/screen.go": badScreen,
	})
	var (
		mu  sync.Mutex
		cmd *exec.Cmd
	)
	rt, err := isolation.Resolve(root)
	if err != nil {
		t.Fatalf("isolation: %v", err)
	}
	if buildAndServe(root, ".", "localhost:0", rt, &mu, &cmd, false) {
		t.Fatal("buildAndServe started a server despite a11y findings")
	}
}

func TestDevA11yGateSkippedWithNoA11y(t *testing.T) {
	root := writeA11yTree(t, map[string]string{
		"app/screen.go": badScreen,
	})
	if !devA11yGate(root, true) {
		t.Fatal("--no-a11y must skip the gate")
	}
	if devA11yGate(root, false) {
		t.Fatal("gate must block without --no-a11y")
	}
}
