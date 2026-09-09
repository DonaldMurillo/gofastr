package ui

// ─── Field message paragraph ─────────────────────────────────────────
//
// One home for the error/help paragraph pair every form component
// appends after its control: Error wins and announces itself with
// role="alert", otherwise Help renders as muted supporting text.
//
// Spelled with render.Tag rather than html.Paragraph (timepicker.go's
// existing precedent): render.Tag writes attributes sorted, so the two
// spellings emit byte-identical markup — pinned by
// TestFieldMessageGolden in fieldmessage_test.go.

import "github.com/DonaldMurillo/gofastr/core/render"

// fieldMessage returns the error-or-help paragraph a form component
// appends below its control. errText wins: when set, the paragraph gets
// id "<id>-error", class "<base>__error", and role="alert"; otherwise
// helpText renders with id "<id>-help" and class "<base>__help". When
// both are empty the result is nil and appends nothing.
//
// It replaces the twelve-line if/else-if paragraph pair that was copied
// verbatim (differing only in the BEM base class) in NumberInput,
// Select, TextArea, Checkbox/Radio/Switch (renderToggle), RadioGroup
// and CheckboxGroup (renderToggleGroup), FileDropzone, FileUpload,
// TimePicker, and the error-only variant in PasswordInput. FormField
// (components.go) intentionally keeps its own spelling: it renders help
// and error as two independent paragraphs instead of either/or.
func fieldMessage(id, base, errText, helpText string) []render.HTML {
	if errText != "" {
		return []render.HTML{render.Tag("p", map[string]string{
			"id":    id + "-error",
			"class": base + "__error",
			"role":  "alert",
		}, render.Text(errText))}
	}
	if helpText != "" {
		return []render.HTML{render.Tag("p", map[string]string{
			"id":    id + "-help",
			"class": base + "__help",
		}, render.Text(helpText))}
	}
	return nil
}
