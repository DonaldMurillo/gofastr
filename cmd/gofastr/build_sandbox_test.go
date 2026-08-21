package main

import "testing"

// TestBuildSandboxGateBlocksUiGoViolations pins that the build/dev gate
// enforces the .ui.go hydration sandbox (check.LintFile: no goroutines,
// channels, or forbidden imports). LintFile/LintPackage implemented the
// sandbox but had ZERO non-test callers: `gofastr build` and `gofastr dev`
// ran only LintA11yFile, which explicitly skips the sandbox rules, so a
// .ui.go file with `go func(){}()` + `import "os"` shipped unchecked.
func TestBuildSandboxGateBlocksUiGoViolations(t *testing.T) {
	root := writeA11yTree(t, map[string]string{
		"app/screen.ui.go": `package app

import "os"

func view() {
	go func() { _ = os.Args }()
}
`,
	})
	if buildSandboxGate(root) {
		t.Fatal("sandbox gate passed a .ui.go file with a goroutine + forbidden import")
	}
}

// TestBuildSandboxGatePassesCleanUiGo ensures the sandbox gate does not
// false-positive on a clean .ui.go file that only uses allowed imports.
func TestBuildSandboxGatePassesCleanUiGo(t *testing.T) {
	root := writeA11yTree(t, map[string]string{
		"app/screen.ui.go": `package app

import (
	"fmt"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func view() any {
	return html.Div(html.DivConfig{Text: fmt.Sprintf("hi")})
}
`,
	})
	if !buildSandboxGate(root) {
		t.Fatal("sandbox gate blocked a clean .ui.go file")
	}
}
