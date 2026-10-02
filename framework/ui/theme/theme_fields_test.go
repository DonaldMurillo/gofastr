package theme

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Every colour field lands on its own light token and its own dark
// key. The colorFields table pairs each field with a getter, a setter
// and a CSS name by hand, so a setter pointed at the wrong token
// compiles; this walks every row through Default.
func TestEveryColourFieldReachesItsToken(t *testing.T) {
	for i, f := range colorFields {
		light := "#0101" + string(rune('a'+i%26)) + "0"
		dark := "#0a0b" + string(rune('a'+i%26)) + "0"
		o := Overrides{Dark: &Overrides{}}
		reflect.ValueOf(&o).Elem().FieldByName(f.field).SetString(light)
		reflect.ValueOf(o.Dark).Elem().FieldByName(f.field).SetString(dark)

		th := Default(o)
		got := reflect.ValueOf(th.Colors).FieldByName(f.field).FieldByName("Value").String()
		if got != light {
			t.Errorf("%s: light token is %q, want %q", f.field, got, light)
		}
		if th.DarkColors[f.css] != dark {
			t.Errorf("%s: dark key %q is %q, want %q", f.field, f.css, th.DarkColors[f.css], dark)
		}
	}
}

// Radii and fonts apply only when set.
func TestRadiusAndFontOverridesApply(t *testing.T) {
	base := Default()
	th := Default(Overrides{RadiusSm: 2, RadiusMd: 5, RadiusLg: 9, FontBody: "Inter, sans-serif"})
	if th.Radii.SM.Value != 2 || th.Radii.MD.Value != 5 || th.Radii.LG.Value != 9 {
		t.Errorf("radii = %v/%v/%v, want 2/5/9", th.Radii.SM.Value, th.Radii.MD.Value, th.Radii.LG.Value)
	}
	if th.Fonts.Body.Value != "Inter, sans-serif" {
		t.Errorf("body font = %q", th.Fonts.Body.Value)
	}
	if th.Fonts.Mono.Value != base.Fonts.Mono.Value || th.Fonts.Heading.Value != base.Fonts.Heading.Value {
		t.Errorf("unset fonts changed: heading %q, mono %q", th.Fonts.Heading.Value, th.Fonts.Mono.Value)
	}
}

// A theme with no dark palette yet gets one when a Dark override
// carries a colour.
func TestApplyDarkMakesThePalette(t *testing.T) {
	var th style.Theme
	applyDark(&th, &Overrides{Primary: "#123456"})
	if th.DarkColors["primary"] != "#123456" {
		t.Errorf("DarkColors = %v, want primary #123456", th.DarkColors)
	}
}

// A light colour with no dark value for its token warns nothing:
// there is no framework dark value for dark mode to keep painting.
func TestLightOnlyQuietWithoutDarkValue(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	warnLightOnly(&style.Theme{}, Overrides{Primary: "#123456"})
	if strings.Contains(buf.String(), "Primary") {
		t.Errorf("warned with no dark value to keep:\n%s", buf.String())
	}
}

// Each parser refuses a value outside its vocabulary.
func TestOptionParsersRefuseUnknown(t *testing.T) {
	for name, parse := range map[string]func(string) error{
		"ButtonTreatment": func(s string) error { _, err := ParseButtonTreatment(s); return err },
		"ButtonRadius":    func(s string) error { _, err := ParseButtonRadius(s); return err },
		"FieldLayout":     func(s string) error { _, err := ParseFieldLayout(s); return err },
		"FieldRadius":     func(s string) error { _, err := ParseFieldRadius(s); return err },
	} {
		if err := parse("sparkly"); err == nil {
			t.Errorf("Parse%s(%q) accepted an unknown value", name, "sparkly")
		}
	}
}

// Complete fills the field options too, not only density and button.
func TestCompleteFillsFieldOptions(t *testing.T) {
	got := ComponentOptions{Density: DefaultOptions.Density, Button: DefaultOptions.Button}.Complete()
	if got.Field != DefaultOptions.Field {
		t.Errorf("Complete left Field = %+v, want %+v", got.Field, DefaultOptions.Field)
	}
}
