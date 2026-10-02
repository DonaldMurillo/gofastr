package theme

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestDefaultHasCanonicalTokens(t *testing.T) {
	th := Default()
	// Every canonical typed color token must have a non-empty Name.
	colors := []struct {
		name string
		got  string
	}{
		{"Primary", th.Colors.Primary.Name},
		{"Background", th.Colors.Background.Name},
		{"Surface", th.Colors.Surface.Name},
		{"Border", th.Colors.Border.Name},
		{"Text", th.Colors.Text.Name},
		{"Danger", th.Colors.Danger.Name},
		{"Success", th.Colors.Success.Name},
		{"Warning", th.Colors.Warning.Name},
		{"Info", th.Colors.Info.Name},
		{"Accent", th.Colors.Accent.Name},
	}
	for _, c := range colors {
		if c.got == "" {
			t.Errorf("Default theme Color %s missing Name", c.name)
		}
	}
	if th.Spacing.MD.Name == "" || th.Spacing.XL.Name == "" {
		t.Errorf("Default theme missing canonical spacing tokens")
	}
}

func TestSinglePrimaryOverrideSwapsToken(t *testing.T) {
	indigo := Default().Colors.Primary.Value
	teal := Default(Overrides{Primary: "#14B8A6", Dark: &Overrides{}}).Colors.Primary.Value
	if teal != "#14B8A6" {
		t.Errorf("expected primary value=#14B8A6, got %q", teal)
	}
	if indigo == teal {
		t.Errorf("override did not change value")
	}
}

func TestDarkColorOverridesKeepBrandingAdaptive(t *testing.T) {
	th := Default(Overrides{
		Primary: "#0F766E",
		Dark:    &Overrides{Primary: "#5EEAD4", Surface: "#10201E"},
	})
	if th.Colors.Primary.Value != "#0F766E" {
		t.Fatalf("light primary override missing: %q", th.Colors.Primary.Value)
	}
	if th.DarkColors["primary"] != "#5EEAD4" || th.DarkColors["surface"] != "#10201E" {
		t.Fatalf("dark overrides missing: %#v", th.DarkColors)
	}
	if Default(Overrides{Primary: "#0F766E", Dark: &Overrides{}}).DarkColors["primary"] != Default().DarkColors["primary"] {
		t.Fatal("light override must not be copied into dark mode without an explicit contrast-safe value")
	}
}

// A Dark override must reach BOTH dark-scheme blocks the runtime can
// paint from: the explicit :root[data-color-scheme="dark"] the toggle
// sets and the prefers-color-scheme media block the OS preference
// drives. Compile it once (style.Theme.DarkColors); both blocks read
// that map, so a drop in the compile step empties the pair together.
func TestDarkOverrideLandsInBothDarkBlocks(t *testing.T) {
	css := Default(Overrides{
		Primary: "#0F766E",
		Dark:    &Overrides{Primary: "#5EEAD4", Surface: "#10201E"},
	}).CSSCustomProperties()
	explicit := strings.Index(css, `:root[data-color-scheme="dark"]`)
	prefer := strings.Index(css, `@media (prefers-color-scheme: dark)`)
	if explicit < 0 || prefer < 0 || prefer < explicit {
		t.Fatalf("dark-scheme blocks missing or out of order:\n%s", css)
	}
	for _, decl := range []string{`--color-primary: #5EEAD4;`, `--color-surface: #10201E;`} {
		if !strings.Contains(css[explicit:prefer], decl) {
			t.Errorf("explicit dark block missing %s:\n%s", decl, css)
		}
		if !strings.Contains(css[prefer:], decl) {
			t.Errorf("prefers-color-scheme block missing %s:\n%s", decl, css)
		}
	}
	// The light value must appear exactly once, in :root: copying a
	// light override into dark mode is the mistake the typed Dark
	// field exists to prevent.
	if n := strings.Count(css, `--color-primary: #0F766E;`); n != 1 {
		t.Errorf("light primary appears %d times, want exactly the :root one:\n%s", n, css)
	}
}

// Dark mode changes colours only. Any other field inside Dark is a
// configuration mistake the compiler used to accept silently; it must
// panic when the theme is built, naming the field and the fix.
func TestDarkRejectsNonColourFields(t *testing.T) {
	cases := []struct {
		name     string
		dark     Overrides
		wantPref string
	}{
		{"component options", Overrides{Components: ComponentOptions{Density: Compact}}, "theme: Dark.Components is set; dark mode changes colours only. Put Components on the top level."},
		{"font", Overrides{FontHeading: "Georgia"}, "theme: Dark.FontHeading is set; dark mode changes colours only. Put FontHeading on the top level."},
		{"radius", Overrides{RadiusLg: 12}, "theme: Dark.RadiusLg is set; dark mode changes colours only. Put RadiusLg on the top level."},
		{"nested dark", Overrides{Dark: &Overrides{}}, "theme: Dark.Dark is set; dark mode changes colours only."},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s: Default accepted a Dark override carrying it", c.name)
					return
				}
				msg, ok := r.(string)
				if !ok {
					t.Errorf("%s: panic value %v (%T) is not the message string", c.name, r, r)
					return
				}
				if !strings.HasPrefix(msg, c.wantPref) {
					t.Errorf("%s: panic message %q does not match %q", c.name, msg, c.wantPref)
				}
			}()
			_ = Default(Overrides{Primary: "#0F766E", Dark: &c.dark})
		}()
	}
}

// Every Overrides field is either a colour in colorFields (compiled
// into the dark palette) or refused inside Dark. A field added later
// must pick one: a new colour missing from the table, or a new
// non-colour field the refusal list forgot, fails here instead of
// being silently ignored inside Dark.
func TestEveryOverridesFieldIsColourOrRefused(t *testing.T) {
	colours := map[string]bool{}
	for _, f := range colorFields {
		colours[f.field] = true
	}
	typ := reflect.TypeFor[Overrides]()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		if colours[name] {
			continue
		}
		d := &Overrides{}
		setNonZero(t, name, reflect.ValueOf(d).Elem().Field(i))
		refused := func() (r bool) {
			defer func() { r = recover() != nil }()
			d.mustBeColourOnly()
			return false
		}()
		if !refused {
			t.Errorf("Dark.%s is neither a colour in colorFields nor refused inside Dark", name)
		}
	}
	for _, f := range colorFields {
		d := &Overrides{}
		v := reflect.ValueOf(d).Elem().FieldByName(f.field)
		if !v.IsValid() || !v.CanSet() || v.Kind() != reflect.String {
			t.Fatalf("colorFields names %s, which is not a string field of Overrides", f.field)
		}
		v.SetString("#123456")
		if f.get(d) != "#123456" {
			t.Errorf("colorFields %s reads a different field", f.field)
		}
	}
}

// setNonZero gives v a non-zero value, descending into the first field
// of a struct.
func setNonZero(t *testing.T, name string, v reflect.Value) {
	t.Helper()
	if !v.CanSet() {
		t.Fatalf("%s: cannot set", name)
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(-1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Struct:
		setNonZero(t, name, v.Field(0))
	default:
		t.Fatalf("%s: no non-zero value for kind %s", name, v.Kind())
	}
}

// A light colour override with no dark twin leaves dark mode painting
// the framework's colour. That is worth one warning naming the field
// and both values; a non-nil Dark — even empty — says the dark palette
// is deliberate and silences it.
func TestLightOnlyOverrideWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	Default(Overrides{Primary: "#0E7490"})
	out := buf.String()
	want := `theme: Primary is set for light (#0E7490) but not in Dark; dark mode keeps the framework's #A5B4FC. Set Dark.Primary, or Dark: &theme.Overrides{} to silence this.`
	if !strings.Contains(out, want) {
		t.Errorf("light-only override warning missing or wrong:\ngot:  %s\nwant: %s", out, want)
	}
	if n := strings.Count(out, "is set for light ("); n != 1 {
		t.Errorf("one light-only field must log one warning, got %d:\n%s", n, out)
	}

	buf.Reset()
	Default(Overrides{Primary: "#0E7490", Dark: &Overrides{}})
	if out := buf.String(); strings.Contains(out, "is set for light (") {
		t.Errorf("a non-nil Dark must silence the warning:\n%s", out)
	}

	buf.Reset()
	Default()
	if out := buf.String(); out != "" {
		t.Errorf("no overrides must log nothing, got:\n%s", out)
	}
}

func TestEmptyOverridesUnchanged(t *testing.T) {
	a := Default()
	b := Default(Overrides{})
	if a.Colors.Primary.Value != b.Colors.Primary.Value {
		t.Errorf("empty overrides should not change tokens")
	}
}

func TestCSSCustomPropertiesEmitsTokens(t *testing.T) {
	css := Default().CSSCustomProperties()
	for _, want := range []string{
		"--color-primary", "--color-surface", "--color-danger",
		"--spacing-md", "--radii-md", "--font-body",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSSCustomProperties missing %q", want)
		}
	}
}

func TestDefaultShipsAdaptiveDarkPalette(t *testing.T) {
	th := Default()
	for _, token := range []string{
		"background", "surface", "surface-soft", "border", "border-strong",
		"text", "text-muted", "text-subtle", "primary", "primary-fg",
		"secondary", "secondary-fg", "accent", "success", "warning", "danger", "info",
	} {
		if strings.TrimSpace(th.DarkColors[token]) == "" {
			t.Errorf("Default theme missing dark token %q", token)
		}
	}
	css := th.CSSCustomProperties()
	for _, want := range []string{
		`:root[data-color-scheme="dark"]`,
		`--color-background: #111113;`,
		`--color-primary: #A5B4FC;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("adaptive theme CSS missing %q\n%s", want, css)
		}
	}
}

// The framework default must stay silent under the host's dark-gap boot
// check (#215): every color token needs a dark value, including the
// code-surface trio that deliberately reuses its light values.
func TestDefaultDarkPaletteComplete(t *testing.T) {
	if gaps := style.DarkPaletteGaps(Default()); len(gaps) != 0 {
		t.Errorf("Default() dark palette gaps: %v", gaps)
	}
}
