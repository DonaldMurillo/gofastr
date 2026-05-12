package main

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/style"
)

// TestThemeStarterPassesValidation guards against the regression where
// gofastr theme init wrote a Theme literal missing one of the required
// sub-structs (Layout, Shadows, …). The generated file is what new
// projects boot with — Validate() panicking on app start is the worst
// possible first impression.
//
// We can't `exec` the binary inside go test cheaply, so we parse the
// const themeStarter directly: it's the literal that ends up on disk
// 1:1. Each top-level field of style.Theme that has a value type
// MUST appear in the literal — otherwise AutoFillNames will leave it
// zero-valued and Validate will reject it.
func TestThemeStarterPassesValidation(t *testing.T) {
	// Every required top-level Theme field. Keep this list in lockstep
	// with style.Theme — adding a field to Theme without updating the
	// scaffold IS the bug this test catches.
	required := []string{
		"Colors:",
		"Spacing:",
		"Radii:",
		"Fonts:",
		"Breakpoints:",
		"Shadows:",
		"ZIndex:",
		"Durations:",
		"Typography:",
		"Layout:",
	}
	for _, f := range required {
		if !strings.Contains(themeStarter, f) {
			t.Errorf("themeStarter is missing required Theme field %q — a fresh `gofastr theme init` will boot with a zero-valued sub-struct and Validate() will panic", f)
		}
	}

	// Smoke: types referenced in the starter must actually exist in
	// the public style package — catches removed-but-still-scaffolded
	// type drift. We touch the symbols at compile time below; if any
	// vanish, the file won't compile.
	_ = style.LayoutSet{}
	_ = style.Spacing{}
	_ = style.Theme{}
}
