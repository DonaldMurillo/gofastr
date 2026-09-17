package ui

// TestFieldMessageGolden pins the exact bytes fieldMessage (fieldmessage.go)
// emits through every component that still uses it (the families the
// headless rebuild has reached render their messages through their
// headless counterparts instead): Error set and Help set. The
// want-strings were captured from the pre-dedup renderers (one inline
// copy per component); the dedupe must not change a single byte.

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestFieldMessageGolden(t *testing.T) {
	rendered := map[string]render.HTML{
		"dropzone_error":    FileDropzone(FileDropzoneConfig{Name: "n", Label: "L", Error: "E!"}),
		"dropzone_help":     FileDropzone(FileDropzoneConfig{Name: "n", Label: "L", Help: "H?"}),
		"fileupload_error":  FileUpload(FileUploadConfig{Name: "n", Label: "L", Error: "E!"}),
		"fileupload_help":   FileUpload(FileUploadConfig{Name: "n", Label: "L", Help: "H?"}),
		"numberinput_error": NumberInput(NumberInputConfig{Name: "n", Label: "L", Error: "E!"}),
		"numberinput_help":  NumberInput(NumberInputConfig{Name: "n", Label: "L", Help: "H?"}),
		"textarea_error":    TextArea(TextAreaConfig{Name: "n", Label: "L", Error: "E!"}),
		"textarea_help":     TextArea(TextAreaConfig{Name: "n", Label: "L", Help: "H?"}),
		"timepicker_error":  TimePicker(TimePickerConfig{Name: "n", Label: "L", Error: "E!"}),
		"timepicker_help":   TimePicker(TimePickerConfig{Name: "n", Label: "L", Help: "H?"}),
	}
	want := map[string]string{
		"dropzone_error":    `<div class="ui-dropzone is-error" data-fui-comp="ui-dropzone"><label class="ui-dropzone__label-wrap" for="n"><div aria-label="L" class="ui-dropzone__zone" data-fui-fileupload="true" role="region"><input aria-describedby="n-error" aria-invalid="true" aria-label="L" class="ui-dropzone__input" id="n" name="n" type="file"></input><div aria-hidden="true" class="ui-dropzone__icon"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></div><h3 class="ui-dropzone__label" id="heading-l">L</h3><p class="ui-dropzone__prompt">Drop a file here or click to browse</p><p class="ui-dropzone__filename"></p></div></label><p class="ui-dropzone__error" id="n-error" role="alert">E!</p></div>`,
		"dropzone_help":     `<div class="ui-dropzone" data-fui-comp="ui-dropzone"><label class="ui-dropzone__label-wrap" for="n"><div aria-label="L" class="ui-dropzone__zone" data-fui-fileupload="true" role="region"><input aria-describedby="n-help" aria-label="L" class="ui-dropzone__input" id="n" name="n" type="file"></input><div aria-hidden="true" class="ui-dropzone__icon"><svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg></div><h3 class="ui-dropzone__label" id="heading-l">L</h3><p class="ui-dropzone__prompt">Drop a file here or click to browse</p><p class="ui-dropzone__filename"></p></div></label><p class="ui-dropzone__help" id="n-help">H?</p></div>`,
		"fileupload_error":  `<label class="ui-fileupload is-error" for="n" data-fui-comp="ui-fileupload"><span class="ui-fileupload__label">L</span><div class="ui-fileupload__zone" data-fui-fileupload=""><input aria-describedby="n-error" aria-invalid="true" class="ui-fileupload__input" id="n" name="n" type="file"></input><p class="ui-fileupload__prompt">Drop a file here, or click to browse</p><p aria-live="polite" class="ui-fileupload__filename"></p></div><p class="ui-fileupload__error" id="n-error" role="alert">E!</p></label>`,
		"fileupload_help":   `<label class="ui-fileupload" for="n" data-fui-comp="ui-fileupload"><span class="ui-fileupload__label">L</span><div class="ui-fileupload__zone" data-fui-fileupload=""><input aria-describedby="n-help" class="ui-fileupload__input" id="n" name="n" type="file"></input><p class="ui-fileupload__prompt">Drop a file here, or click to browse</p><p aria-live="polite" class="ui-fileupload__filename"></p></div><p class="ui-fileupload__help" id="n-help">H?</p></label>`,
		"numberinput_error": `<div class="ui-number-input is-error" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-error" aria-invalid="true" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__error" id="n-error" role="alert">E!</p></div>`,
		"numberinput_help":  `<div class="ui-number-input" data-fui-comp="ui-number-input"><label class="ui-number-input__label" for="n">L</label><div class="ui-number-input__row"><button aria-label="Decrement L" class="ui-number-input__step ui-number-input__step--minus" data-fui-number-for="n" data-fui-number-step="-1" type="button">−</button><input aria-describedby="n-help" class="ui-number-input__input" id="n" name="n" step="1" type="number" value="0"></input><button aria-label="Increment L" class="ui-number-input__step ui-number-input__step--plus" data-fui-number-for="n" data-fui-number-step="1" type="button">+</button></div><p class="ui-number-input__help" id="n-help">H?</p></div>`,
		"textarea_error":    `<div class="ui-textarea is-error" data-fui-comp="ui-textarea"><label class="ui-textarea__label" for="n">L</label><textarea aria-describedby="n-error" aria-invalid="true" class="ui-textarea__input" id="n" name="n" rows="3"></textarea><p class="ui-textarea__error" id="n-error" role="alert">E!</p></div>`,
		"textarea_help":     `<div class="ui-textarea" data-fui-comp="ui-textarea"><label class="ui-textarea__label" for="n">L</label><textarea aria-describedby="n-help" class="ui-textarea__input" id="n" name="n" rows="3"></textarea><p class="ui-textarea__help" id="n-help">H?</p></div>`,
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
