package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FormRepeater drew its row Remove (a red outline) and its Add (a
// neutral outline) with its own button rules in the ui-form-repeater
// sheet, a second button look beside the kit's. They wear the kit's
// button classes now — danger for the destructive row remove,
// secondary for the add — and every fui-button token they carry is
// one the ui-button sheet actually draws.
func TestFormRepeaterControlsWearKitButtonClasses(t *testing.T) {
	h := string(FormRepeater(FormRepeaterConfig{
		Name:  "links",
		Items: [][]render.HTML{{render.Text("a")}, {render.Text("b")}},
	}))
	sheet := buttonCSS(style.DefaultTheme())
	for _, c := range []struct {
		marker string
		want   []string
	}{
		{`data-hui-repeater-action="remove"`, []string{"fui-button", "fui-button--danger", "fui-form-repeater__remove"}},
		{`data-hui-repeater-action="add"`, []string{"fui-button", "fui-button--secondary", "fui-form-repeater__add"}},
	} {
		got := classTokens(buttonTag(t, h, c.marker))
		for _, w := range c.want {
			if !containsToken(got, w) {
				t.Errorf("%s: class %q missing from %v", c.marker, w, got)
			}
			if strings.HasPrefix(w, "fui-button") && !strings.Contains(sheet, "."+w+" {") {
				t.Errorf("%s: class %q has no rule in the ui-button sheet", c.marker, w)
			}
		}
	}
}

// The ui-form-repeater sheet owns the group's layout only: no rule of
// its own may draw the remove or add control (a border, background,
// colour, padding, font, cursor, min size or focus ring), because the
// kit button classes draw them. Layout placement (justify-self) is the
// one thing it may still say about a control. The items list drops
// its UA bullets and indent.
func TestFormRepeaterSheetCarriesNoButtonRules(t *testing.T) {
	css := formRepeaterCSS(style.DefaultTheme())
	rule := regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)
	drawing := regexp.MustCompile(`(?m)^\s*(border|background|color|padding|font|cursor|min-block-size|min-height|outline|opacity|display)\b`)
	for _, m := range rule.FindAllStringSubmatch(css, -1) {
		sel, body := m[1], m[2]
		if !strings.Contains(sel, "fui-form-repeater__remove") && !strings.Contains(sel, "fui-form-repeater__add") {
			continue
		}
		if d := drawing.FindString(body); d != "" {
			t.Errorf("ui-form-repeater draws its own button: %s { …%s… }", strings.TrimSpace(sel), strings.TrimSpace(d))
		}
	}
	if strings.Contains(css, "--ui-form-repeater-action-min-height") {
		t.Error("ui-form-repeater still carries its own action min-height knob; the button sheet's density owns it")
	}
	items := regexp.MustCompile(`(?s)\.fui-form-repeater__items\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if items == nil {
		t.Fatal("no .fui-form-repeater__items rule")
	}
	for _, want := range []string{"list-style: none", "margin: 0", "padding: 0"} {
		if !strings.Contains(items[1], want) {
			t.Errorf(".fui-form-repeater__items lacks %q (the UA list bullets/indent show)", want)
		}
	}
}
