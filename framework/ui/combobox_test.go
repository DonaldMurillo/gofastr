package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

func TestComboboxRendersStyledFieldWithHiddenStatus(t *testing.T) {
	h := string(Combobox(ComboboxConfig{ID: "c", Name: "q", Label: "Filter components",
		Options: []headless.ComboboxOption{{Label: "Card"}}}))
	// The style marker rides the wrapper this adapter renders, so the
	// sheet's root rules are compound: a descendant-form root selector
	// would match nothing and the field look would silently die (the
	// review's item 20 shape).
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `class="fui-combobox"`) || !strings.Contains(root, `data-fui-comp="ui-combobox"`) {
		t.Errorf("the wrapper must carry both the class and the style marker:\n%s", root)
	}
	for _, want := range []string{
		`class="fui-combobox__label"`,
		`class="fui-combobox__input"`,
		`class="fui-combobox__listbox"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("combobox missing %q:\n%s", want, h)
		}
	}
	// The result-count region stays in the accessibility tree
	// (role=status, clipped — never display:none) and is not seen.
	// Attributes render sorted, so the recipe class sits directly
	// before the status hooks on the span.
	if !strings.Contains(h, `<span class="fui-visually-hidden" data-hui-combobox-no-results`) {
		t.Errorf("the status region must carry the visually-hidden recipe:\n%s", h)
	}
}
func TestComboboxLabelHiddenUsesRecipe(t *testing.T) {
	h := string(Combobox(ComboboxConfig{ID: "c", Name: "q", Label: "Filter",
		LabelHidden: true, Options: []headless.ComboboxOption{{Label: "Card"}}}))
	if !strings.Contains(h, `class="fui-combobox__label fui-visually-hidden"`) {
		t.Errorf("LabelHidden must append the recipe to the label's own class:\n%s", h)
	}
}

func TestComboboxCSSStylesFieldHidesStatusAndHonoursHidden(t *testing.T) {
	css := comboboxCSS(style.Theme{})
	for _, want := range []string{
		// The field look item 20 names: a bordered, padded input.
		".fui-combobox__input {",
		"border: 1px solid var(--color-border",
		// The live region is clipped on a page that loads only this
		// sheet, and stays in the tree.
		"[data-fui-comp=\"ui-combobox\"] .fui-visually-hidden {",
		// The module filters static options by setting [hidden]; the
		// author display rules must not defeat the attribute (#337's
		// shape, carried from the retired pattern).
		"[data-fui-comp=\"ui-combobox\"] .fui-combobox__option[hidden]",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("comboboxCSS lost %q:\n%s", want, css)
		}
	}
}
