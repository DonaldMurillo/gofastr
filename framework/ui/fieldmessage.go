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
// TextArea, FileDropzone, FileUpload and TimePicker. The families the
// headless rebuild has reached render their messages through their
// headless counterparts instead (the field owns both, the choice group
// renders its own either/or paragraph, the affix shells carry none).
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
