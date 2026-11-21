package ui_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestToggleGroupOptionTypesAreDistinct(t *testing.T) {
	kind := func(option any) string {
		switch option.(type) {
		case ui.RadioGroupOption:
			return "radio"
		case ui.CheckboxGroupOption:
			return "checkbox"
		default:
			return ""
		}
	}
	if got := kind(ui.RadioGroupOption{}); got != "radio" {
		t.Errorf("RadioGroupOption matched %q, want radio", got)
	}
	if got := kind(ui.CheckboxGroupOption{}); got != "checkbox" {
		t.Errorf("CheckboxGroupOption matched %q, want checkbox", got)
	}
}
