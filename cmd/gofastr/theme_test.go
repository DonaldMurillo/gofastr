package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
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
		"DarkColors:",
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

// The starter's DarkColors block is generated from the canonical framework
// palette (framework/ui/theme.Default), not hand-maintained — this pins the
// two against drifting apart.
func TestThemeStarterDarkColorsMatchCanonical(t *testing.T) {
	dark := uitheme.Default().DarkColors
	if len(dark) == 0 {
		t.Fatal("canonical theme has no DarkColors")
	}
	for name, value := range dark {
		re := regexp.MustCompile(fmt.Sprintf(`%q:\s*%q`, name, value))
		if !re.MatchString(themeStarter) {
			t.Errorf("themeStarter DarkColors missing %q: %q", name, value)
		}
	}
	if strings.Count(themeStarter, `"#`) < len(dark) {
		t.Error("themeStarter lost its color literals")
	}
}
