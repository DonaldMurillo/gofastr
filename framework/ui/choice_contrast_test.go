package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// controlBorder is the edge every form control draws: the knob a theme
// sets, falling back to text-subtle, the token that clears 3:1.
const controlBorder = "var(--ui-control-border, var(--color-text-subtle))"

// WCAG 1.4.11: a form control's boundary needs 3:1 against the surface.
// An unchecked checkbox or radio is drawn by its border alone, and a text
// field's edge on the hairline border token was 1.27:1 (#702). Every
// control boundary reads controlBorder, and the default theme's
// text-subtle clears 3:1 over surface in both schemes.
func TestControlBordersMeetNonTextContrast(t *testing.T) {
	th := theme.Default()
	for _, c := range []struct {
		style    *registry.Style
		selector string
	}{
		{toggleStyle, ".fui-choice__input {"},
		{toggleStyle, ".fui-switch__input {"},
		{formFieldStyle, ".fui-input {"},
		{textAreaStyle, ".fui-textarea {"},
		{selectStyle, ".fui-select {"},
		{comboboxStyle, ".fui-combobox__input {"},
		{inputGroupStyle, ".fui-input-group {"},
		{multiselectStyle, `[data-cui-comp="ui-multiselect"] details {`},
		{numberInputStyle, ".fui-number-input__row {"},
		{passwordInputStyle, ".fui-password {"},
		{searchInputStyle, ".fui-search {"},
		{tagInputStyle, ".fui-tag-input__zone {"},
		{colorFieldStyle, ".fui-color__text {"},
		{colorPickerStyle, ".fui-color-picker__input {"},
		{timePickerStyle, ".fui-time-picker__input {"},
		{globalSearchStyle, ".fui-global-search__input {"},
	} {
		css := c.style.Entry().CSSFor(th)
		i := strings.Index(css, c.selector)
		if i < 0 {
			t.Errorf("no %q rule", c.selector)
			continue
		}
		rule := css[i : i+strings.Index(css[i:], "}")]
		if !strings.Contains(rule, "solid "+controlBorder) {
			t.Errorf("%s border does not read %s:\n%s", c.selector, controlBorder, rule)
		}
	}
	for _, w := range style.ContrastWarnings(th) {
		if strings.Contains(w, "text-subtle") {
			t.Errorf("default theme control boundary: %s", w)
		}
	}
}
