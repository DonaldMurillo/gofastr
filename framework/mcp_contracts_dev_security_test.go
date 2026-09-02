package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contracts_fix is the agent-reachable write: it runs the pass over the
// working directory and writes Report.Apply's edits to disk, with
// containment from containedPath, which is lexical only. A discovered
// file that is a symlink to somewhere OUTSIDE the analysed root passes
// that check, and the fix is written through to the target. This drives
// the real MCP entry point (toolContractsFix) end to end: discovery,
// analysis, and the write all operate on the outside file while every
// path the tool reports stays inside the root.
func TestContractsFixRefusesSymlinkEscape(t *testing.T) {
	const src = `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("post", "/orders", nil)
}
`
	// The victim lives outside the analysed root and is never written to
	// by anything but the escape under test.
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.go")
	if err := os.WriteFile(victim, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	root := writeTreeAndChdir(t, map[string]string{})
	if err := os.Symlink(victim, filepath.Join(root, "main.go")); err != nil {
		t.Skipf("symlink creation refused: %v", err)
	}

	app := NewApp()

	// Premise: the pass's discovery reads through the file symlink, so
	// the violation in the outside victim is genuinely part of this
	// run. If it is not, the fix path below is vacuous and the finding
	// downgrades to helper-level (Report.Apply only).
	sawFinding := false
	if got, err := app.toolContractsVerify(context.Background(), map[string]any{}); err == nil {
		if blob, mErr := json.Marshal(got); mErr == nil && strings.Contains(string(blob), "GOFASTR1005") {
			sawFinding = true
		}
	}
	if !sawFinding {
		t.Logf("discovery did NOT read through the file symlink: contracts_verify sees no GOFASTR1005 finding, so the fix below cannot reach the victim (finding stays helper-level)")
	}

	fixRes, fixErr := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"})

	body, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if fixErr == nil && sawFinding && strings.Contains(string(body), `"POST"`) {
		blob, _ := json.Marshal(fixRes)
		t.Fatalf("SECURITY: [path-escape] contracts_fix (GOFASTR1005) wrote through root/main.go to %s outside the analysed root %s: %s", victim, root, blob)
	}
}
