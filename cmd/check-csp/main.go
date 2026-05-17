//check-csp:ignore-file
// The CLI references <script> in its own help text; exempt from
// the rule the CLI enforces.

// check-csp is a build-time linter that fails when production Go
// source emits inline <script> blocks. The framework's default
// strict Content-Security-Policy is `default-src 'self'`, which
// forbids inline JS; violations silently break pages in production
// (the browser blocks the script with a CSP error).
//
// Usage:
//
//	go run ./cmd/check-csp           # walks the current directory
//	go run ./cmd/check-csp ./path    # walks a specific path
//
// Exits 0 when clean, 1 when violations were found, 2 on infrastructure
// error. Wired into `make build` via a `csp-check` prerequisite so the
// app refuses to build when an inline <script> sneaks in.
//
// Test files (_test.go) are skipped — fixtures may legitimately
// contain known-bad strings for assertion purposes.
package main

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	scriptRes, err := check.LintNoInlineScriptsRecursive(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-csp: %v\n", err)
		os.Exit(2)
	}
	styleRes, err := check.LintNoInlineStylesRecursive(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-csp: %v\n", err)
		os.Exit(2)
	}
	exitCode := 0
	if scriptRes.HasErrors() {
		fmt.Fprintf(os.Stderr, "check-csp: %d inline-script violation(s):\n\n%s\n",
			len(scriptRes.Violations), scriptRes.Error())
		fmt.Fprintln(os.Stderr, "Fix: move the script body to an external file and reference it via <script src=\"…\">.")
		exitCode = 1
	}
	if styleRes.HasErrors() {
		fmt.Fprintf(os.Stderr, "check-csp: %d inline-style violation(s):\n\n%s\n",
			len(styleRes.Violations), styleRes.Error())
		fmt.Fprintln(os.Stderr, "Fix: move the rules into a registered stylesheet and reference via a class name.")
		exitCode = 1
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	fmt.Println("  ✓ no inline <script> blocks or style=\"…\" attributes")
}
