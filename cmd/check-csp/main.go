//check-csp:ignore-file
// The help text names <script>, the thing it forbids.

// check-csp [root] exits 1 when Go source under root emits an inline
// <script> or style="...", and 2 when the walk fails.
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
	scriptRes, styleRes, err := check.LintNoInlineRecursive(root)
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
