package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

var testFieldControl = headless.FieldControl{
	ID:          "f1",
	DescribedBy: "f1-error f1-hint",
	Invalid:     true,
	Required:    true,
}

// The text input holds the raw value verbatim — the whole point of the
// component — and a value the picker cannot show marks the shell
// rather than degrading (headless.Color's own handling).
func TestColorFieldTextInputHoldsTheRawValue(t *testing.T) {
	for _, raw := range []string{"#4F46E5", "transparent", "var(--brand)", "rgb(1 2 3 / 50%)"} {
		out := string(ColorField(ColorFieldConfig{
			Name:        "brand",
			Value:       raw,
			SwatchLabel: "brand colour",
		}))
		if !strings.Contains(out, `value="`+raw+`"`) {
			t.Errorf("Value %q not held verbatim in the text input:\n%s", raw, out)
		}
		hex := out[strings.Index(out, `data-hui-affix-input`):]
		if !strings.Contains(hex[:strings.Index(hex, ">")], `value="`+raw+`"`) {
			t.Errorf("Value %q must be the TEXT input's value (the source of truth), not the swatch's:\n%s", raw, out)
		}
		pickable := strings.HasPrefix(raw, "#") && (len(raw) == 4 || len(raw) == 7)
		if pickable && !strings.Contains(out, `data-invalid`) {
			continue // fine
		} else if !pickable && !strings.Contains(out, `data-invalid`) {
			t.Errorf("unpickable Value %q must mark the shell data-invalid:\n%s", raw, out)
		}
	}
}

// The swatch never overwrites the text input's value, and a non-hex
// value falls the swatch back to black while the text keeps the truth.
func TestColorFieldSwatchAndTextAreSeparateValues(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name:        "brand",
		Value:       "var(--brand)",
		SwatchLabel: "brand colour",
	}))
	if !strings.Contains(out, `value="#000000"`) {
		t.Errorf("the swatch must fall back to #000000 for an unpickable value:\n%s", out)
	}
	if strings.Count(out, `value="var(--brand)"`) != 1 {
		t.Errorf("the text input alone holds the true value:\n%s", out)
	}
}

func TestColorFieldRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ColorField accepted an empty Name — the hex field is what submits")
		}
	}()
	_ = ColorField(ColorFieldConfig{Value: "#fff", SwatchLabel: "x"})
}

func TestColorFieldRequiresAnAccessibleSwatchName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ColorField accepted an empty SwatchLabel — a control would be announced as nothing")
		}
	}()
	_ = ColorField(ColorFieldConfig{Name: "n", Value: "#fff"})
}

// The swatch names itself through PickColor + Name; the TEXT input is
// named from SwatchLabel (or TextLabel). Leaving the text input
// unnamed is the worse omission — it carries the value — and the easy
// one to miss, because a caller wrapping this in its own label sees a
// labelled control.
func TestColorFieldNamesBothInputs(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name: "accent", Value: "#fff", SwatchLabel: "Brand colour",
	}))
	if !strings.Contains(out, `aria-label="Pick accent"`) {
		t.Errorf("the swatch's name comes from PickColor + Name:\n%s", out)
	}
	if !strings.Contains(out, `aria-label="Brand colour"`) {
		t.Errorf("the text input defaults its name to SwatchLabel:\n%s", out)
	}

	out = string(ColorField(ColorFieldConfig{
		Name: "accent", Value: "#fff", SwatchLabel: "swatch", TextLabel: "hex value",
	}))
	if !strings.Contains(out, `aria-label="hex value"`) {
		t.Errorf("TextLabel overrides the text input's name:\n%s", out)
	}
}

// A caller-supplied name on the text input always wins.
func TestColorFieldDoesNotOverrideACallerSuppliedName(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name: "accent", Value: "#fff", SwatchLabel: "swatch", TextLabel: "ignored",
		TextAttrs: map[string]string{"aria-labelledby": "my-label"},
	}))
	if !strings.Contains(out, `aria-labelledby="my-label"`) {
		t.Fatalf("caller's aria-labelledby lost:\n%s", out)
	}
	if strings.Contains(out, `aria-label="ignored"`) {
		t.Fatalf("TextLabel must not also ship beside the caller's name:\n%s", out)
	}
}

// Caller attributes survive on both inputs (the theme editor's
// data-token wiring depends on it), except what the component owns.
func TestColorFieldPassesAttrsToBothInputs(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name:        "accent",
		Value:       "#fff",
		SwatchLabel: "x",
		SwatchAttrs: map[string]string{"data-token": "color-primary", "data-type": "color-swatch"},
		TextAttrs:   map[string]string{"data-token": "color-primary", "data-type": "color"},
	}))
	swatch := out[strings.Index(out, `data-hui-affix-swatch`):]
	swatch = swatch[:strings.Index(swatch, ">")]
	if !strings.Contains(swatch, `data-token="color-primary"`) || !strings.Contains(swatch, `data-type="color-swatch"`) { // not-a-secret: a selector for the editor's colour-primary row; data-token is the theme TOKEN NAME, not a credential
		t.Errorf("swatch attrs lost:\n%s", swatch)
	}
	hex := out[strings.Index(out, `data-hui-affix-input`):]
	hex = hex[:strings.Index(hex, ">")]
	if !strings.Contains(hex, `data-token="color-primary"`) || !strings.Contains(hex, `data-type="color"`) { // not-a-secret: a selector for the editor's colour-primary row; data-token is the theme TOKEN NAME, not a credential
		t.Errorf("text attrs lost:\n%s", hex)
	}
}

// The headless hooks are what the behaviour module binds: the shell's
// data-hui-color, the swatch's tab-order exclusion, the text input's
// data-hui-affix-input.
func TestColorFieldShipsTheHeadlessHooks(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name: "accent", Value: "#4F46E5", SwatchLabel: "x",
	}))
	for _, want := range []string{
		`data-hui-affix`, `data-hui-color`, `data-hui-affix-swatch`, `data-hui-affix-input`,
		`tabindex="-1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// The swatch is out of the tab order; the TEXT input is the one
	// focus target and keeps its tabindex at 0 (absent).
	swatch := out[strings.Index(out, `data-hui-affix-swatch`):]
	swatch = swatch[:strings.Index(swatch, ">")]
	if !strings.Contains(swatch, `tabindex="-1"`) {
		t.Errorf("the swatch must be out of the tab order:\n%s", swatch)
	}
}

// The field's wiring reaches the text input: the id the outer label
// points at, the described-by chain, the invalid state — and it wins
// over TextID.
func TestColorFieldCarriesTheFieldsWiring(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Name:        "accent",
		Value:       "#fff",
		TextID:      "ignored",
		SwatchLabel: "x",
		Field:       testFieldControl,
	}))
	// The hex input is the second <input>; its attributes sort
	// before its own data-hui-affix-input hook, so slice the whole
	// tag around the hook.
	idx := strings.Index(out, `data-hui-affix-input`)
	start := strings.LastIndex(out[:idx], "<")
	hex := out[start : idx+strings.Index(out[idx:], ">")+1]
	for _, want := range []string{`id="f1"`, `aria-describedby="f1-error f1-hint"`, `aria-invalid="true"`} {
		if !strings.Contains(hex, want) {
			t.Errorf("text input missing %q:\n%s", want, hex)
		}
	}
}

// The row lays out as a flex row and the text input keeps its
// intrinsic width from pushing the row wider than its container —
// which is how the theme editor's rail became 2300px tall.
func TestColorFieldLaysOutAsARow(t *testing.T) {
	css := colorFieldCSS(style.Theme{})
	root := sectionOf(t, css, ".fui-color {")
	if !strings.Contains(root, "display: flex") {
		t.Fatalf("the field does not lay out as a row:\n%s", root)
	}
	text := sectionOf(t, css, ".fui-color__text")
	if !strings.Contains(text, "min-inline-size: 0") {
		t.Fatalf("the text input keeps its intrinsic width and will overflow the rail:\n%s", text)
	}
}

func TestColorFieldExtraAttrsOnRoot(t *testing.T) {
	h := ColorField(ColorFieldConfig{
		Name:        "n",
		SwatchLabel: "Colour",
		ExtraAttrs:  map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}
