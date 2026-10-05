package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The picker is a radiogroup of one option per theme plus the default,
// each naming the class the module puts on <html>; the default names
// none. The server cannot know the visitor's stored choice, so every
// option ships unchecked and the module's arrival pass checks one.
func TestThemePickerRendersOneOptionPerTheme(t *testing.T) {
	th := style.DefaultTheme()
	th.Colors.Primary = style.Color{Name: "primary", Value: "#123456"}
	ref := style.RegisterThemeOverride(th)
	out := string(ThemePicker(ThemePickerConfig{
		ID:         "tp",
		Themes:     []ThemeChoice{{Label: "Brutal", Theme: ref}},
		ExtraAttrs: map[string]string{"data-test": "hook", "data-hui-theme-pick": "evil"},
	}))
	for _, want := range []string{
		`data-hui-theme-picker=""`,
		`role="radiogroup"`,
		`aria-label="Theme"`,
		`id="tp"`,
		`data-test="hook"`,
		`data-hui-theme-pick="" role="radio" type="button">Default</button>`,
		`data-hui-theme-pick="` + ref.Class() + `" role="radio" type="button">Brutal</button>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `aria-checked="true"`) {
		t.Errorf("an option ships checked; the stored choice lives in the browser:\n%s", out)
	}
	if strings.Contains(out, "evil") {
		t.Errorf("ExtraAttrs overrode a hook the component owns:\n%s", out)
	}
}

// A choice with no theme fails at render, never as a second option
// that names no class and so silently means Default.
func TestThemePickerRefusesZeroTheme(t *testing.T) {
	defer func() {
		r := recover()
		if s, _ := r.(string); !strings.Contains(s, "ThemeRef") {
			t.Fatalf("recover() = %v, want the zero-ThemeRef panic", r)
		}
	}()
	ThemePicker(ThemePickerConfig{Themes: []ThemeChoice{{Label: "Nope"}}})
}
