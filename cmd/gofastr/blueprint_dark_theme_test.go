package main

import (
	"strings"
	"testing"
)

// appGoFor renders bp and returns the generated app.go.
func appGoFor(t *testing.T, bp Blueprint) string {
	t.Helper()
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("validateBlueprint: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	for _, f := range files {
		if f.name == "app.go" {
			return f.content
		}
	}
	t.Fatal("no app.go rendered")
	return ""
}

// A partial app.theme.dark map is an overlay. Emitted as the whole of
// theme.DarkColors, every token it did not name kept its LIGHT value under the
// dark scheme (DarkPaletteGaps): a dark override of primary alone painted the
// light background and text under the toggle. The palette is seeded from the
// framework's complete dark palette first, then the blueprint's keys win.
func TestDarkOverlayKeepsDefaultPalette(t *testing.T) {
	appGo := appGoFor(t, bpTheme(map[string]string{"primary": "#2563EB"}, map[string]string{"primary": "#ff0000"}))
	seed := strings.Index(appGo, "theme.DarkColors = uitheme.Default().DarkColors")
	if seed < 0 {
		t.Fatalf("appTheme does not seed the dark palette from the framework default:\n%s", firstLineWith(appGo, "DarkColors"))
	}
	over := strings.Index(appGo, `theme.DarkColors["primary"] = "#ff0000"`)
	if over < seed {
		t.Fatalf("dark override missing or applied before the seed (seed@%d, override@%d)", seed, over)
	}
	// The seeded ink was chosen for the framework's dark primary, not this one.
	if ink := strings.Index(appGo, `theme.DarkColors["primary-fg"] = theme.Colors.PrimaryFg.Value`); ink < over {
		t.Fatalf("overridden dark primary does not keep its light ink after the seed:\n%s", appGo[seed:])
	}
	if !strings.Contains(appGo, `uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"`) {
		t.Fatal("app.go does not import framework/ui/theme as uitheme")
	}
}

// app.theme with only a dark: map decoded to an empty light map, and every
// appTheme guard tested the light map alone: no appTheme, no WithTheme, so the
// dark palette was dropped while the header still showed a theme toggle.
func TestDarkOnlyThemeEmitsAppTheme(t *testing.T) {
	appGo := appGoFor(t, bpTheme(map[string]string{}, map[string]string{"primary": "#ff0000"}))
	for _, want := range []string{"func appTheme() style.Theme", "site.WithTheme(appTheme())", `theme.DarkColors["primary"] = "#ff0000"`} {
		if !strings.Contains(appGo, want) {
			t.Errorf("app.go is missing %q", want)
		}
	}
}
