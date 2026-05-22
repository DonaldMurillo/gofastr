package ui

import (
	"strings"
	"testing"
)

func TestDatePickerRendersInputs(t *testing.T) {
	h := DatePicker(DatePickerConfig{
		Name:  "birthday",
		Label: "Birthday",
	})
	for _, want := range []string{
		"ui-datepicker",
		"Birthday",
		`name="birthday"`,
		`type="hidden"`,
		`type="text"`,
		`readonly`,
		`data-fui-open`,
		`data-fui-popover-anchor`,
		"ui-datepicker-calendar",
	} {
		mustContain(t, h, want)
	}
}

func TestDatePickerUsesProvidedID(t *testing.T) {
	h := DatePicker(DatePickerConfig{
		ID:   "my-date",
		Name: "date",
	})
	mustContain(t, h, `id="my-date"`)
	mustContain(t, h, `id="my-date-calendar"`)
}

func TestDatePickerCalendarGrid(t *testing.T) {
	h := DatePicker(DatePickerConfig{Name: "d"})
	for _, want := range []string{
		"ui-datepicker-grid",
		`role="grid"`,
		"ui-datepicker-day-name",
		"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa",
		"ui-datepicker-prev",
		"ui-datepicker-next",
	} {
		mustContain(t, h, want)
	}
	// Should have 6 weeks × 7 days = 42 day buttons
	if got := strings.Count(string(h), "ui-datepicker-day-btn"); got != 42 {
		t.Fatalf("expected 42 day buttons, got %d", got)
	}
}

func TestDatePickerValuePassed(t *testing.T) {
	h := DatePicker(DatePickerConfig{
		Name:  "date",
		Value: "2026-05-21",
	})
	mustContain(t, h, `value="2026-05-21"`)
}

func TestDatePickerHelpText(t *testing.T) {
	h := DatePicker(DatePickerConfig{
		Name:     "date",
		HelpText: "Pick your birthday",
	})
	mustContain(t, h, "Pick your birthday")
	mustContain(t, h, "ui-help-text")
}

func TestDatePickerNoHelpTextWhenEmpty(t *testing.T) {
	h := DatePicker(DatePickerConfig{Name: "date"})
	if strings.Contains(string(h), "ui-help-text") {
		t.Fatalf("should not render help text when empty:\n%s", h)
	}
}

func TestDatePickerMinMax(t *testing.T) {
	h := DatePicker(DatePickerConfig{
		Name: "date",
		Min:  "2020-01-01",
		Max:  "2030-12-31",
	})
	mustContain(t, h, `min="2020-01-01"`)
	mustContain(t, h, `max="2030-12-31"`)
}
