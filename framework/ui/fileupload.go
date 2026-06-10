package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── FileUpload ─────────────────────────────────────────────────────
//
// A labelled file-picker with a drag-and-drop hot zone. The native
// <input type="file"> is the source of truth — keyboard, screen
// reader, and form-POST flows all work without JavaScript. The drag
// zone is progressive enhancement: data-fui-fileupload tells the
// runtime to wire dragover/dragleave/drop handlers that forward
// dropped File objects into the input's `files` property.
//
// The runtime emits a `change` event on the input after a drop so
// any form `data-fui-rpc-trigger="input"` listener gets fired
// uniformly whether the user clicked-to-pick or dragged-to-drop.

// FileUploadConfig configures a file upload.
type FileUploadConfig struct {
	// Name is the form-field name. Required.
	Name string

	// Label is the visible label. Required.
	Label string

	// ID is the input element's id. Defaults to Name.
	ID string

	// Accept is the MIME-type filter passed to the native input.
	// Example: "image/*", ".pdf,.docx"
	Accept string

	// Multiple allows selecting multiple files.
	Multiple bool

	// Required marks the field as required in form submission.
	Required bool

	// Disabled disables interaction.
	Disabled bool

	// MaxSizeMB, when > 0, is announced in the help text so users
	// understand the constraint. The native input doesn't enforce
	// it; server-side validation must.
	MaxSizeMB int

	// Help renders supporting text under the drop zone.
	Help string

	// Error overrides Help and switches the field to error state.
	Error string

	Class string
}

// FileUpload renders a drag-drop file picker.
//
// Markup shape:
//
//	<label class="ui-fileupload" for="…">
//	  <span class="ui-fileupload__label">…</span>
//	  <div class="ui-fileupload__zone" data-fui-fileupload>
//	    <input type="file" …>
//	    <p class="ui-fileupload__prompt">Drop files or click to browse</p>
//	    <p class="ui-fileupload__filename"></p>  ← runtime updates after change
//	  </div>
//	  <p class="ui-fileupload__help">…</p>
//	</label>
func FileUpload(cfg FileUploadConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FileUpload requires Name")
	}
	if cfg.Label == "" {
		panic("ui: FileUpload requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-fileupload"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := html.Attrs{
		"type":  "file",
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-fileupload__input",
	}
	if cfg.Accept != "" {
		inputAttrs["accept"] = cfg.Accept
	}
	if cfg.Multiple {
		inputAttrs["multiple"] = ""
	}
	if cfg.Required {
		inputAttrs["required"] = ""
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" || cfg.MaxSizeMB > 0 {
		inputAttrs["aria-describedby"] = id + "-help"
	}

	input := render.Tag("input", inputAttrs)

	zone := html.Div(html.DivConfig{
		Class:      "ui-fileupload__zone",
		ExtraAttrs: html.Attrs{"data-fui-fileupload": ""},
	},
		input,
		html.Paragraph(html.TextConfig{Class: "ui-fileupload__prompt"},
			render.Text(uploadPrompt(cfg))),
		html.Paragraph(html.TextConfig{
			Class:      "ui-fileupload__filename",
			ExtraAttrs: html.Attrs{"aria-live": "polite"},
		}),
	)

	help := helpText(cfg)
	children := []render.HTML{
		html.Span(html.TextConfig{Class: "ui-fileupload__label"}, render.Text(cfg.Label)),
		zone,
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:         id + "-error",
			Class:      "ui-fileupload__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-fileupload__help",
		}, render.Text(help)))
	}

	return fileUploadStyle.WrapHTML(render.Tag("label", map[string]string{
		"class": cls,
		"for":   id,
	}, children...))
}

func uploadPrompt(cfg FileUploadConfig) string {
	if cfg.Multiple {
		return "Drop files here, or click to browse"
	}
	return "Drop a file here, or click to browse"
}

func helpText(cfg FileUploadConfig) string {
	bits := []string{}
	if cfg.Help != "" {
		bits = append(bits, cfg.Help)
	}
	if cfg.MaxSizeMB > 0 {
		bits = append(bits, "Max "+itoa(cfg.MaxSizeMB)+" MB")
	}
	return strings.Join(bits, " · ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
