package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The text input is the source of truth, so it must carry the value VERBATIM.
// A native colour input cannot represent "transparent", "inherit" or a var()
// reference: it silently falls back to black, which is the whole reason this
// component exists alongside ColorPicker.
func TestColorFieldTextInputHoldsTheRawValue(t *testing.T) {
	for _, raw := range []string{"#4F46E5", "transparent", "var(--brand)", "rgb(1 2 3 / 50%)"} {
		out := string(ColorField(ColorFieldConfig{
			Value:       raw,
			SwatchValue: "#000000",
			SwatchLabel: "brand colour",
		}))
		if !strings.Contains(out, `value="`+escapeForAttr(raw)+`"`) {
			t.Fatalf("the text input lost the raw value %q:\n%s", raw, out)
		}
	}
}

// The swatch shows a resolved colour that may differ from the value; both must
// appear, and the swatch must never overwrite the text input's value attribute.
func TestColorFieldSwatchAndTextAreSeparateValues(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Value:       "var(--brand)",
		SwatchValue: "#4F46E5",
		SwatchLabel: "brand colour",
	}))
	if !strings.Contains(out, `value="#4F46E5"`) {
		t.Fatalf("the swatch did not get its resolved display colour:\n%s", out)
	}
	if !strings.Contains(out, `value="var(--brand)"`) {
		t.Fatalf("the text input did not keep the authoritative value:\n%s", out)
	}
}

// A colour input has no visible label of its own, so an unlabelled one is
// announced as nothing. Requiring the name at construction is the only point
// where it can be enforced.
func TestColorFieldRequiresAnAccessibleSwatchName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ColorField accepted an empty SwatchLabel — the swatch would be announced as nothing")
		}
	}()
	_ = ColorField(ColorFieldConfig{Value: "#fff"})
}

// BOTH inputs must have an accessible name. Naming only the swatch shipped a
// critical axe violation on every page that did not wrap the field in its own
// <label for=…>, which the theme editor did, so the omission was invisible
// until the component appeared standalone in the gallery.
func TestColorFieldNamesBothInputs(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Value: "#fff", SwatchLabel: "Brand colour",
	}))
	if strings.Count(out, `aria-label="Brand colour"`) != 2 {
		t.Fatalf("both inputs should fall back to SwatchLabel for their name:\n%s", out)
	}

	out = string(ColorField(ColorFieldConfig{
		Value: "#fff", SwatchLabel: "swatch", TextLabel: "hex value",
	}))
	if !strings.Contains(out, `aria-label="hex value"`) {
		t.Fatalf("TextLabel did not name the text input:\n%s", out)
	}
}

// A caller pointing the text input at its own visible label must not be
// overridden. An aria-label would win over the <label> and announce the wrong
// thing.
func TestColorFieldDoesNotOverrideACallerSuppliedName(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Value: "#fff", SwatchLabel: "swatch", TextLabel: "ignored",
		TextAttrs: map[string]string{"aria-labelledby": "my-label"},
	}))
	if !strings.Contains(out, `aria-labelledby="my-label"`) {
		t.Fatalf("caller's aria-labelledby was dropped:\n%s", out)
	}
	if strings.Contains(out, `aria-label="ignored"`) {
		t.Fatalf("component overrode the caller's own labelling:\n%s", out)
	}
}

// Callers wire the two inputs together with their own JS, which needs its
// attributes to survive on both.
func TestColorFieldPassesAttrsToBothInputs(t *testing.T) {
	out := string(ColorField(ColorFieldConfig{
		Value:       "#fff",
		SwatchLabel: "x",
		SwatchAttrs: map[string]string{"data-token": "color-primary", "data-type": "color-swatch"},
		TextAttrs:   map[string]string{"data-token": "color-primary", "data-type": "color"},
	}))
	if strings.Count(out, `data-token="color-primary"`) != 2 { // not-a-secret: a DOM attribute selector, not a credential
		t.Fatalf("data-token did not reach both inputs:\n%s", out)
	}
	if !strings.Contains(out, `data-type="color-swatch"`) || !strings.Contains(out, `data-type="color"`) {
		t.Fatalf("per-input attrs were merged into one another:\n%s", out)
	}
}

// The row is a row. A stacked swatch-over-input doubles the height of every
// control, which is how the theme editor's rail became 2300px tall.
func TestColorFieldLaysOutAsARow(t *testing.T) {
	css := colorFieldCSS(style.Theme{})
	root := sectionOf(t, css, `[data-fui-comp="ui-color-field"] {`)
	if !strings.Contains(root, "display: flex") {
		t.Fatalf("the field does not lay out as a row:\n%s", root)
	}
	text := sectionOf(t, css, `[data-fui-comp="ui-color-field"] .ui-color-field__text`)
	if !strings.Contains(text, "min-inline-size: 0") {
		t.Fatalf("the text input keeps its intrinsic width and will overflow the rail:\n%s", text)
	}
}

// escapeForAttr mirrors the escaping render applies to attribute values, so the
// assertions above compare like with like.
func escapeForAttr(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `"`, "&#34;", `<`, "&lt;", `>`, "&gt;", `'`, "&#39;")
	return r.Replace(s)
}
