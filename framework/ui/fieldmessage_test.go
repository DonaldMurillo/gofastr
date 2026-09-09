package ui

// TestFieldMessageGolden pins the exact bytes fieldMessage (fieldmessage.go)
// emits through every component that uses it: Error set, Help set, and
// PasswordInput's plain state. The want-strings were captured from the
// pre-dedup renderers (one inline copy per component); the dedupe must not
// change a single byte.

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestFieldMessageGolden(t *testing.T) {
	rendered := map[string]render.HTML{
		"checkbox_error": Checkbox(ToggleConfig{Name: "n", Value: "v", Label: "L", Error: "E!"}),
		"checkbox_help":  Checkbox(ToggleConfig{Name: "n", Value: "v", Label: "L", Help: "H?"}),
		"checkboxgroup_error": CheckboxGroup(CheckboxGroupConfig{
			Name: "n", Legend: "L", Options: []CheckboxGroupOption{{Value: "v", Label: "V"}}, Error: "E!",
		}),
		"checkboxgroup_help": CheckboxGroup(CheckboxGroupConfig{
			Name: "n", Legend: "L", Options: []CheckboxGroupOption{{Value: "v", Label: "V"}}, Help: "H?",
		}),
		"dropzone_error":    FileDropzone(FileDropzoneConfig{Name: "n", Label: "L", Error: "E!"}),
		"dropzone_help":     FileDropzone(FileDropzoneConfig{Name: "n", Label: "L", Help: "H?"}),
		"fileupload_error":  FileUpload(FileUploadConfig{Name: "n", Label: "L", Error: "E!"}),
		"fileupload_help":   FileUpload(FileUploadConfig{Name: "n", Label: "L", Help: "H?"}),
		"numberinput_error": NumberInput(NumberInputConfig{Name: "n", Label: "L", Error: "E!"}),
		"numberinput_help":  NumberInput(NumberInputConfig{Name: "n", Label: "L", Help: "H?"}),
		"passwordinput_error": PasswordInput(PasswordInputConfig{
			Name: "n", ID: "n", Error: "E!",
		}),
		"passwordinput_plain": PasswordInput(PasswordInputConfig{
			Name: "n", ID: "n",
		}),
		"radiogroup_error": RadioGroup(RadioGroupConfig{
			Name: "n", Legend: "L", Options: []RadioGroupOption{{Value: "v", Label: "V"}}, Error: "E!",
		}),
		"radiogroup_help": RadioGroup(RadioGroupConfig{
			Name: "n", Legend: "L", Options: []RadioGroupOption{{Value: "v", Label: "V"}}, Help: "H?",
		}),
		"select_error":     Select(SelectConfig{Name: "n", Label: "L", Options: []SelectOption{{Value: "v", Text: "V"}}, Error: "E!"}),
		"select_help":      Select(SelectConfig{Name: "n", Label: "L", Options: []SelectOption{{Value: "v", Text: "V"}}, Help: "H?"}),
		"switch_error":     Switch(ToggleConfig{Name: "n", Value: "v", Label: "L", Error: "E!"}),
		"switch_help":      Switch(ToggleConfig{Name: "n", Value: "v", Label: "L", Help: "H?"}),
		"textarea_error":   TextArea(TextAreaConfig{Name: "n", Label: "L", Error: "E!"}),
		"textarea_help":    TextArea(TextAreaConfig{Name: "n", Label: "L", Help: "H?"}),
		"timepicker_error": TimePicker(TimePickerConfig{Name: "n", Label: "L", Error: "E!"}),
		"timepicker_help":  TimePicker(TimePickerConfig{Name: "n", Label: "L", Help: "H?"}),
	}
	want := map[string]string{
		"checkbox_error":      `<label class="ui-toggle ui-toggle--checkbox is-error" for="n" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input aria-describedby="n-error" aria-invalid="true" class="ui-toggle__input" id="n" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">L</span><p class="ui-toggle__error" id="n-error" role="alert">E!</p></label>`,
		"checkbox_help":       `<label class="ui-toggle ui-toggle--checkbox" for="n" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input aria-describedby="n-help" class="ui-toggle__input" id="n" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">L</span><p class="ui-toggle__help" id="n-help">H?</p></label>`,
		"checkboxgroup_error": `<fieldset aria-describedby="n-group-error" class="ui-toggle-group is-error" id="n-group" role="group" data-fui-comp="ui-toggle"><legend class="ui-toggle-group__legend">L</legend><label class="ui-toggle ui-toggle--checkbox" for="n-group-v" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input class="ui-toggle__input" id="n-group-v" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">V</span></label><p class="ui-toggle-group__error" id="n-group-error" role="alert">E!</p></fieldset>`,
		"checkboxgroup_help":  `<fieldset aria-describedby="n-group-help" class="ui-toggle-group" id="n-group" role="group" data-fui-comp="ui-toggle"><legend class="ui-toggle-group__legend">L</legend><label class="ui-toggle ui-toggle--checkbox" for="n-group-v" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input class="ui-toggle__input" id="n-group-v" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">V</span></label><p class="ui-toggle-group__help" id="n-group-help">H?</p></fieldset>`,
		"dropzone_error":      `<div class="ui-dropzone is-error" data-fui-comp="ui-dropzone"><label class="ui-dropzone__label-wrap" for="n"><div aria-label="L" class="ui-dropzone__zone" data-fui-fileupload="true" role="region"><input aria-describedby="n-error" aria-invalid="true" aria-label="L" class="ui-dropzone__input" id="n" name="n" type="file"></input><div aria-hidden="true" class="ui-dropzone__icon"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></div><h3 class="ui-dropzone__label" id="heading-l">L</h3><p class="ui-dropzone__prompt">Drop a file here or click to browse</p><p class="ui-dropzone__filename"></p></div></label><p class="ui-dropzone__error" id="n-error" role="alert">E!</p></div>`,
		"dropzone_help":       `<div class="ui-dropzone" data-fui-comp="ui-dropzone"><label class="ui-dropzone__label-wrap" for="n"><div aria-label="L" class="ui-dropzone__zone" data-fui-fileupload="true" role="region"><input aria-describedby="n-help" aria-label="L" class="ui-dropzone__input" id="n" name="n" type="file"></input><div aria-hidden="true" class="ui-dropzone__icon"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></div><h3 class="ui-dropzone__label" id="heading-l">L</h3><p class="ui-dropzone__prompt">Drop a file here or click to browse</p><p class="ui-dropzone__filename"></p></div></label><p class="ui-dropzone__help" id="n-help">H?</p></div>`,
		"fileupload_error":    `<label class="ui-fileupload is-error" for="n" data-fui-comp="ui-fileupload"><span class="ui-fileupload__label">L</span><div class="ui-fileupload__zone" data-fui-fileupload=""><input aria-describedby="n-error" aria-invalid="true" class="ui-fileupload__input" id="n" name="n" type="file"></input><p class="ui-fileupload__prompt">Drop a file here, or click to browse</p><p aria-live="polite" class="ui-fileupload__filename"></p></div><p class="ui-fileupload__error" id="n-error" role="alert">E!</p></label>`,
		"fileupload_help":     `<label class="ui-fileupload" for="n" data-fui-comp="ui-fileupload"><span class="ui-fileupload__label">L</span><div class="ui-fileupload__zone" data-fui-fileupload=""><input aria-describedby="n-help" class="ui-fileupload__input" id="n" name="n" type="file"></input><p class="ui-fileupload__prompt">Drop a file here, or click to browse</p><p aria-live="polite" class="ui-fileupload__filename"></p></div><p class="ui-fileupload__help" id="n-help">H?</p></label>`,
		"numberinput_error":   `<div class="ui-number-input is-error" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-error" aria-invalid="true" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__error" id="n-error" role="alert">E!</p></div>`,
		"numberinput_help":    `<div class="ui-number-input" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-help" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__help" id="n-help">H?</p></div>`,
		"passwordinput_error": `<div class="ui-password-input is-error" data-fui-comp="ui-password-input"><input aria-describedby="n-error" aria-invalid="true" class="ui-password-input__input" id="n" name="n" type="password"><button aria-label="Show password" aria-pressed="false" class="ui-password-input__toggle" type="button">⊙</button><p class="ui-password-input__error" id="n-error" role="alert">E!</p></div>`,
		"passwordinput_plain": `<div class="ui-password-input" data-fui-comp="ui-password-input"><input class="ui-password-input__input" id="n" name="n" type="password"><button aria-label="Show password" aria-pressed="false" class="ui-password-input__toggle" type="button">⊙</button></div>`,
		"radiogroup_error":    `<fieldset aria-describedby="n-group-error" class="ui-toggle-group is-error" id="n-group" role="radiogroup" data-fui-comp="ui-toggle"><legend class="ui-toggle-group__legend">L</legend><label class="ui-toggle ui-toggle--radio" for="n-group-v" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input class="ui-toggle__input" id="n-group-v" name="n" type="radio" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">V</span></label><p class="ui-toggle-group__error" id="n-group-error" role="alert">E!</p></fieldset>`,
		"radiogroup_help":     `<fieldset aria-describedby="n-group-help" class="ui-toggle-group" id="n-group" role="radiogroup" data-fui-comp="ui-toggle"><legend class="ui-toggle-group__legend">L</legend><label class="ui-toggle ui-toggle--radio" for="n-group-v" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input class="ui-toggle__input" id="n-group-v" name="n" type="radio" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">V</span></label><p class="ui-toggle-group__help" id="n-group-help">H?</p></fieldset>`,
		"select_error":        `<div class="ui-select is-error" data-fui-comp="ui-select"><label class="ui-select__label" for="n">L</label><select aria-describedby="n-error" aria-invalid="true" class="ui-select__input" id="n" name="n"><option value="v">V</option></select><p class="ui-select__error" id="n-error" role="alert">E!</p></div>`,
		"select_help":         `<div class="ui-select" data-fui-comp="ui-select"><label class="ui-select__label" for="n">L</label><select aria-describedby="n-help" class="ui-select__input" id="n" name="n"><option value="v">V</option></select><p class="ui-select__help" id="n-help">H?</p></div>`,
		"switch_error":        `<label class="ui-toggle ui-toggle--switch is-error" for="n" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input aria-describedby="n-error" aria-invalid="true" class="ui-toggle__input" id="n" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">L</span><p class="ui-toggle__error" id="n-error" role="alert">E!</p></label>`,
		"switch_help":         `<label class="ui-toggle ui-toggle--switch" for="n" data-fui-comp="ui-toggle"><span class="ui-toggle__control"><input aria-describedby="n-help" class="ui-toggle__input" id="n" name="n" type="checkbox" value="v"></input><span aria-hidden="true" class="ui-toggle__indicator"></span></span><span class="ui-toggle__label">L</span><p class="ui-toggle__help" id="n-help">H?</p></label>`,
		"textarea_error":      `<div class="ui-textarea is-error" data-fui-comp="ui-textarea"><label class="ui-textarea__label" for="n">L</label><textarea aria-describedby="n-error" aria-invalid="true" class="ui-textarea__input" id="n" name="n" rows="3"></textarea><p class="ui-textarea__error" id="n-error" role="alert">E!</p></div>`,
		"textarea_help":       `<div class="ui-textarea" data-fui-comp="ui-textarea"><label class="ui-textarea__label" for="n">L</label><textarea aria-describedby="n-help" class="ui-textarea__input" id="n" name="n" rows="3"></textarea><p class="ui-textarea__help" id="n-help">H?</p></div>`,
		"timepicker_error":    `<div class="ui-time-picker is-error" data-fui-comp="ui-time-picker"><label class="ui-time-picker__label" for="n">L</label><input aria-describedby="n-error" aria-invalid="true" aria-label="L" class="ui-time-picker__input" id="n" name="n" type="time"></input><p class="ui-time-picker__error" id="n-error" role="alert">E!</p></div>`,
		"timepicker_help":     `<div class="ui-time-picker" data-fui-comp="ui-time-picker"><label class="ui-time-picker__label" for="n">L</label><input aria-describedby="n-help" aria-label="L" class="ui-time-picker__input" id="n" name="n" type="time"></input><p class="ui-time-picker__help" id="n-help">H?</p></div>`,
	}
	for name, h := range rendered {
		t.Run(name, func(t *testing.T) {
			if string(h) != want[name] {
				t.Errorf("rendered HTML drifted from the pre-fieldMessage bytes:\n got: %q\nwant: %q", string(h), want[name])
			}
		})
	}
}
