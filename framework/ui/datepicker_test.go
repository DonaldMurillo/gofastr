package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A single date is a FormField: its label names the native input, its
// hint and error describe it, and the picker's sheet and the field's
// both load.
func TestDatePickerSingleIsAField(t *testing.T) {
	out := string(DatePicker(DatePickerConfig{ID: "due", Label: "Due date", Name: "due_on",
		Value: "2026-11-05", Endpoint: "/island/due", Help: "Weekdays only.", Error: "is required"}))
	for _, want := range []string{
		`data-cui-comp="ui-form-field"`, `data-cui-comp="ui-datepicker"`,
		`<label class="fui-field__label" data-cui-internal="" for="due">Due date</label>`,
		`aria-describedby="due-error due-hint"`, `aria-invalid="true"`,
		`class="fui-input"`, `type="date"`, `name="due_on"`,
		`data-cui-signal="due-calendar"`, `data-cui-rpc="/island/due?month=2026-12"`,
		`class="fui-visually-hidden"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	if strings.Count(out, "<label") != 1 {
		t.Error("the field's label is the input's only label")
	}
}

// A range is a fieldset named by Label: two labelled native inputs, the
// group's hint and error on the group.
func TestDatePickerRangeIsAFieldset(t *testing.T) {
	out := string(DatePicker(DatePickerConfig{ID: "stay", Label: "Your stay", Endpoint: "/island/stay",
		Range: &DatePickerRange{StartName: "check_in", EndName: "check_out", StartLabel: "Check-in"},
		Help:  "Two nights at least."}))
	for _, want := range []string{
		`<fieldset`, `<legend class="fui-field__label"`, `>Your stay</legend>`,
		`aria-describedby="stay-group-desc"`,
		`for="stay-start">Check-in</label>`, `for="stay-end">End date</label>`,
		`name="check_in"`, `name="check_out"`, `data-hui-datepicker-range-mode`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}

// The words are the reader's: the nav names and the pick sentences
// come through StringsFor, the dates and the week through the locale.
func TestDatePickerSaysItsWordsThroughCtx(t *testing.T) {
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	out := string(DatePickerMonth(DatePickerConfig{ID: "due", Label: "Échéance", Name: "due_on",
		Month: "2026-11", Endpoint: "/island/due", Ctx: ctx}))
	for _, want := range []string{`>novembre 2026<`, `aria-label="jeudi 5 novembre 2026"`, `<th abbr="lundi" class="fui-datepicker__weekday" scope="col">lun.</th>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}

func TestDatePickerRequiresEndpoint(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Endpoint") {
			t.Fatalf("panic = %v, want one naming Endpoint", r)
		}
	}()
	DatePicker(DatePickerConfig{ID: "due", Label: "Due", Name: "due_on"})
}
