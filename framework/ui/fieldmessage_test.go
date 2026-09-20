package ui

// TestFieldMessageGolden pins the exact bytes fieldMessage (fieldmessage.go)
// emits through every component that still uses it (the families the
// headless rebuild has reached render their messages through their
// headless counterparts instead — with the bespoke-behaviour family,
// FileUpload carries its hint inside the zone and its error as a
// described-by paragraph, FileDropzone and TextArea render through
// headless.Field): Error set and Help set. The want-strings were
// captured from the pre-dedup renderers (one inline copy per
// component); the dedupe must not change a single byte.

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestFieldMessageGolden(t *testing.T) {
	rendered := map[string]render.HTML{
		"numberinput_error": NumberInput(NumberInputConfig{Name: "n", Label: "L", Error: "E!"}),
		"numberinput_help":  NumberInput(NumberInputConfig{Name: "n", Label: "L", Help: "H?"}),
		"timepicker_error":  TimePicker(TimePickerConfig{Name: "n", Label: "L", Error: "E!"}),
		"timepicker_help":   TimePicker(TimePickerConfig{Name: "n", Label: "L", Help: "H?"}),
	}
	want := map[string]string{
		"numberinput_error": `<div class="ui-number-input is-error" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-error" aria-invalid="true" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__error" id="n-error" role="alert">E!</p></div>`,
		"numberinput_help":  `<div class="ui-number-input" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-help" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__help" id="n-help">H?</p></div>`,
		"timepicker_error":  `<div class="ui-time-picker is-error" data-fui-comp="ui-time-picker"><label class="ui-time-picker__label" for="n">L</label><input aria-describedby="n-error" aria-invalid="true" aria-label="L" class="ui-time-picker__input" id="n" name="n" type="time"></input><p class="ui-time-picker__error" id="n-error" role="alert">E!</p></div>`,
		"timepicker_help":   `<div class="ui-time-picker" data-fui-comp="ui-time-picker"><label class="ui-time-picker__label" for="n">L</label><input aria-describedby="n-help" aria-label="L" class="ui-time-picker__input" id="n" name="n" type="time"></input><p class="ui-time-picker__help" id="n-help">H?</p></div>`,
	}
	for name, h := range rendered {
		t.Run(name, func(t *testing.T) {
			if string(h) != want[name] {
				t.Errorf("rendered HTML drifted from the pre-fieldMessage bytes:\n got: %q\nwant: %q", string(h), want[name])
			}
		})
	}
}
