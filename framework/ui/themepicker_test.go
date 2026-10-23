package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A theme's label is drawn in the request's language.
func TestThemePickerLabelTranslates(t *testing.T) {
	brutal := testPhrase("Brutal look")
	cat := i18n.NewMapCatalog()
	cat.Set("es", brutal.Key(), i18n.Message{Text: "Aspecto brutal"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"}), i18n.NewTranslator(cat, "en"))
	out := string(ThemePicker(ThemePickerConfig{
		Ctx:    ctx,
		Themes: []ThemeChoice{{Label: brutal, Theme: style.RegisterThemeOverride(style.DefaultTheme())}},
	}))
	if !strings.Contains(out, ">Aspecto brutal</button>") || strings.Contains(out, "Brutal look") {
		t.Errorf("the theme's label is not translated:\n%s", out)
	}
}

// The picker is a radiogroup of one option per theme plus the default,
// each naming the class the module puts on <html>; the default names
// none. The server cannot know the visitor's stored choice, so it
// ships Default checked, a first visit's choice, and the module's
// arrival pass re-checks the stored one.
func TestThemePickerRendersOneOptionPerTheme(t *testing.T) {
	th := style.DefaultTheme()
	th.Colors.Primary = style.Color{Name: "primary", Value: "#123456"}
	ref := style.RegisterThemeOverride(th)
	out := string(ThemePicker(ThemePickerConfig{
		ID:         "tp",
		Themes:     []ThemeChoice{{Label: testPhrase("Brutal"), Theme: ref}},
		ExtraAttrs: map[string]string{"data-test": "hook", "data-hui-theme-pick": "evil"},
	}))
	for _, want := range []string{
		`data-hui-theme-picker=""`,
		`role="radiogroup"`,
		`aria-label="Theme"`,
		`id="tp"`,
		`data-test="hook"`,
		`data-hui-theme-pick="" role="radio" tabindex="0" type="button">Default</button>`,
		`data-hui-theme-pick="` + ref.Class() + `" role="radio" tabindex="-1" type="button">Brutal</button>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %q:\n%s", want, out)
		}
	}
	// Default is the first-visit state; the runtime re-checks from the
	// stored choice. TestThemeRadiosSSRCheckFirstVisit pins each option.
	if n := strings.Count(out, `aria-checked="true"`); n != 1 {
		t.Errorf("%d options ship checked, want Default alone:\n%s", n, out)
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
	ThemePicker(ThemePickerConfig{Themes: []ThemeChoice{{Label: testPhrase("Nope")}}})
}
