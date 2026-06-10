package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── FileDropzone ───────────────────────────────────────────────────
//
// A larger-surface variant of FileUpload — same form-POST semantics
// (native <input type="file"> under the hood, runtime drag-drop
// handler via data-fui-fileupload), but a more prominent drop area
// and an optional thumbnail preview strip for image uploads.
//
// Use FileUpload for inline form fields; use FileDropzone for hero
// import pages, asset libraries, profile-picture uploads where the
// drop affordance is the main UI.

// FileDropzoneConfig configures a FileDropzone.
type FileDropzoneConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required, used as the input's
	// aria-label and the visible heading inside the dropzone).
	Label string
	// Prompt overrides the default "Drop files here or click to
	// browse" call-to-action text.
	Prompt string
	// Accept is the MIME-type filter (e.g. "image/*", ".csv").
	Accept string
	// Multiple allows selecting multiple files.
	Multiple bool
	// Required marks the input required.
	Required bool
	// Disabled disables interaction.
	Disabled bool
	// ShowPreview opts into a thumbnail strip rendered below the
	// dropzone after change. Only works for image MIME types — the
	// runtime FileReader-reads each file and emits <img>.
	ShowPreview bool
	// MaxSizeMB is announced in the help text. Server is still
	// authoritative.
	MaxSizeMB int
	// Help renders supporting text under the dropzone.
	Help string
	// Error overrides Help and switches to error state.
	Error string
	ID    string
	Class string
}

// FileDropzone renders a hero file-drop surface.
func FileDropzone(cfg FileDropzoneConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FileDropzone requires Name")
	}
	if cfg.Label == "" {
		panic("ui: FileDropzone requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	prompt := cfg.Prompt
	if prompt == "" {
		if cfg.Multiple {
			prompt = "Drop files here or click to browse"
		} else {
			prompt = "Drop a file here or click to browse"
		}
	}

	cls := "ui-dropzone"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":       "file",
		"name":       cfg.Name,
		"id":         id,
		"class":      "ui-dropzone__input",
		"aria-label": cfg.Label,
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
	if cfg.ShowPreview {
		inputAttrs["data-fui-dropzone-preview"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" || cfg.MaxSizeMB > 0 {
		inputAttrs["aria-describedby"] = id + "-help"
	}

	zoneAttrs := map[string]string{
		"class":               "ui-dropzone__zone",
		"data-fui-fileupload": "true", // reuse the existing drag-drop runtime hook
		// role=region + aria-label so AT users hear "<Label>, region"
		// when the focus passes through the dropzone container,
		// distinct from the inner file input.
		"role":       "region",
		"aria-label": cfg.Label,
	}

	zoneChildren := []render.HTML{
		render.Tag("input", inputAttrs),
		render.Tag("div", map[string]string{"class": "ui-dropzone__icon", "aria-hidden": "true"},
			render.HTML(dropzoneIcon())),
		html.Heading(html.HeadingConfig{Level: 3, Class: "ui-dropzone__label"},
			render.Text(cfg.Label)),
		html.Paragraph(html.TextConfig{Class: "ui-dropzone__prompt"}, render.Text(prompt)),
		html.Paragraph(html.TextConfig{Class: "ui-dropzone__filename"}, render.Text("")),
	}
	zone := render.Tag("label",
		map[string]string{"for": id, "class": "ui-dropzone__label-wrap"},
		render.Tag("div", zoneAttrs, zoneChildren...),
	)

	children := []render.HTML{zone}
	if cfg.ShowPreview {
		children = append(children, render.Tag("div", map[string]string{
			"class":                         "ui-dropzone__previews",
			"data-fui-dropzone-preview-for": id,
			"aria-live":                     "polite",
		}))
	}

	help := cfg.Help
	if cfg.MaxSizeMB > 0 {
		if help == "" {
			help = "Max " + strconv.Itoa(cfg.MaxSizeMB) + " MB."
		} else {
			help += " (max " + strconv.Itoa(cfg.MaxSizeMB) + " MB)"
		}
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:         id + "-error",
			Class:      "ui-dropzone__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-dropzone__help",
		}, render.Text(help)))
	}

	return dropzoneStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

func dropzoneIcon() string {
	return `<svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

var dropzoneStyle = registry.RegisterStyle("ui-dropzone", dropzoneCSS)

func dropzoneCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-dropzone"] {
  display: grid;
  gap: var(--spacing-md, 12px);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__label-wrap {
  display: block;
  cursor: pointer;
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__zone {
  display: grid;
  justify-items: center;
  gap: var(--spacing-xs, 6px);
  padding: var(--spacing-xl, 32px) var(--spacing-lg, 24px);
  border: 2px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
  background: var(--color-surface, #FFFFFF);
  text-align: center;
  transition: border-color 120ms ease, background 120ms ease;
}
[data-fui-comp="ui-dropzone"].is-dragover .ui-dropzone__zone,
[data-fui-comp="ui-dropzone"] .ui-dropzone__zone.is-dragover,
[data-fui-comp="ui-dropzone"] .ui-dropzone__label-wrap:hover .ui-dropzone__zone {
  border-color: var(--color-primary, #4F46E5);
  background: color-mix(in srgb, var(--color-primary, #4F46E5) 10%, var(--color-surface, #FFFFFF));
  border-style: solid;
}
/* Slight scale-in for tactile feedback. */
[data-fui-comp="ui-dropzone"].is-dragover .ui-dropzone__icon,
[data-fui-comp="ui-dropzone"] .ui-dropzone__zone.is-dragover .ui-dropzone__icon {
  transform: translateY(-2px);
  transition: transform 120ms ease;
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0,0,0,0);
  white-space: nowrap;
  border: 0;
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__input:focus-visible + .ui-dropzone__icon {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 4px;
  border-radius: 4px;
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__icon {
  color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__label {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__prompt {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__filename {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-primary, #4F46E5);
  font-weight: 500;
  min-block-size: 1.2em;
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__previews {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 8px);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__preview {
  width: 72px;
  height: 72px;
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface-soft, #F4F4F5);
  object-fit: cover;
  border: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-dropzone"] .ui-dropzone__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-dropzone"].is-error .ui-dropzone__zone {
  border-color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-dropzone"].is-disabled .ui-dropzone__zone {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
