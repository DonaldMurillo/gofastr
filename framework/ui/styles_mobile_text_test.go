package ui

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Text controls draw at text-sm on desktop, but iOS Safari zooms the
// page into any focused control whose text is under 16px. Each sheet
// that owns a text control restores text-base below the md breakpoint.
func TestTextControlsAreBaseSizeOnPhones(t *testing.T) {
	phoneBase := regexp.MustCompile(`@media \(max-width: 767\.98px\) \{\s*[^{}]+\{ font-size: var\(--text-base, 1rem\); \}`)
	want := []string{
		"ui-form-field", "ui-textarea", "ui-select", "ui-combobox",
		"ui-tag-input", "ui-input-group", "ui-password-input",
		"ui-number-input", "ui-time-picker", "ui-search-input",
	}
	thm := style.DefaultTheme()
	sheets := map[string]string{}
	for _, e := range registry.All() {
		sheets[e.Name] = e.CSSFor(thm)
	}
	for _, name := range want {
		css, ok := sheets[name]
		if !ok {
			t.Errorf("sheet %s is not registered", name)
			continue
		}
		if !phoneBase.MatchString(css) {
			t.Errorf("%s has no phone rule restoring var(--text-base): iOS zooms into its focused control", name)
		}
	}
}
