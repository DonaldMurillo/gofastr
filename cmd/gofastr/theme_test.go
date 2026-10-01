package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The starter's DarkColors block is generated from the canonical framework
// palette (framework/ui/theme.Default), not hand-maintained; this pins the
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
