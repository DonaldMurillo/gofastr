package ui

import (
	"context"
	_ "embed"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FileDropzone ───────────────────────────────────────────────────
//
// A larger-surface variant of FileUpload for hero import pages,
// asset libraries, profile-picture uploads — anywhere the drop
// affordance is the main UI. Same form-POST semantics: a real
// <input type="file"> under a <label>, so the whole surface opens the
// picker with no script. The drop behaviour, the chosen-files list
// and the pick announcement come from the headless behaviour module
// on the data-hui-drop hooks (armDrop / showFiles), and the
// announcement sentences resolve per request through the strings
// bridge.
//
// The preview strip is this component's own: headless has no
// counterpart and thumbnails are a styling concern. It is driven by
// the filedropzone module this file registers on
// data-fui-dropzone-preview — which duplicates nothing, because the
// names, the drop forwarding and the sentence all belong to the
// headless module and this package's module touches none of them.

//go:embed filedropzone.js
var fileDropzoneJS string

var _ = registry.RegisterBehavior("filedropzone", fileDropzoneJS,
	registry.Markers("[data-fui-dropzone-preview]"))

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
	// dropzone after change. Only works for image MIME types: the
	// filedropzone runtime module FileReader-reads each file and
	// emits <img>.
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the dropzone's root div.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and every data-hui-* key — the drop hooks are
	// the runtime's contract, not a caller's to forge. A retargeted
	// data-hui-drop-input would send every drop on this zone to
	// another input.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the prompt,
	// the max-size help label and the announcement sentences. When
	// nil, English fallbacks apply.
	Ctx context.Context
}

// FileDropzone renders a hero file-drop surface.
func FileDropzone(cfg FileDropzoneConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FileDropzone requires Name")
	}
	if cfg.Label == "" {
		panic("ui: FileDropzone requires Label")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	prompt := cfg.Prompt
	if prompt == "" {
		if cfg.Multiple {
			prompt = i18nui.T(ctx, i18nui.KeyDropzoneDropFiles)
		} else {
			prompt = i18nui.T(ctx, i18nui.KeyDropzoneDropFile)
		}
	}
	words := StringsFor(ctx)

	inputAttrs := map[string]string{
		"type":       "file",
		"name":       cfg.Name,
		"id":         id,
		"class":      "fui-drop__input",
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
	// The preview wiring: ui-only surface with no headless
	// counterpart (thumbnails are styling, the names list and the
	// announcement are the headless module's), so it stays in this
	// package's own vocabulary and is bound by this package's own
	// module — it duplicates nothing the headless hooks already do.
	if cfg.ShowPreview {
		inputAttrs["data-fui-dropzone-preview"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
		inputAttrs["aria-describedby"] = id + "-error"
	} else if help := dropzoneHelp(cfg, ctx); help != "" {
		inputAttrs["aria-describedby"] = id + "-help"
	}

	zoneChildren := []render.HTML{
		render.Tag("input", inputAttrs),
		render.Tag("div", map[string]string{"class": "fui-drop__icon", "aria-hidden": "true"},
			render.HTML(dropzoneIcon())),
		html.Heading(html.HeadingConfig{Level: 3, Class: "fui-drop__label"},
			render.Text(cfg.Label)),
		html.Paragraph(html.TextConfig{Class: "fui-drop__prompt"}, render.Text(prompt)),
	}
	zone := render.Tag("label",
		map[string]string{"for": id, "class": "fui-drop__label-wrap"},
		render.Tag("div", map[string]string{
			"class": "fui-drop__zone",
			// role=region + aria-label so AT users hear "<Label>, region"
			// when the focus passes through the dropzone container,
			// distinct from the inner file input.
			"role":       "region",
			"aria-label": cfg.Label,
		}, zoneChildren...),
	)

	children := []render.HTML{zone}
	// The names list and the announcement: the headless module fills
	// these on every pick or drop (showFiles).
	children = append(children,
		render.Tag("ul", map[string]string{
			"class":              "fui-drop__list",
			"role":               "list",
			"data-hui-drop-list": "",
		}),
		render.Tag("span", map[string]string{
			"class":                "fui-drop__status",
			"role":                 "status",
			"data-hui-drop-status": "",
		}),
	)
	if cfg.ShowPreview {
		children = append(children, render.Tag("div", map[string]string{
			"class":                         "fui-drop__previews",
			"data-fui-dropzone-preview-for": id,
			"aria-live":                     "polite",
		}))
	}

	if help := dropzoneHelp(cfg, ctx); help != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id": id + "-help", "class": "fui-drop__help",
		}, render.Text(help)))
	}
	if cfg.Error != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id": id + "-error", "class": "fui-drop__error", "role": "alert",
		}, render.Text(cfg.Error)))
	}

	cls := "fui-drop"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// The headless drop hooks on the root: data-hui-drop arms the
	// drag listeners, -input resolves the input per event, and the
	// one/many sentences travel as attributes so a translated page
	// announces in its own language.
	rootAttrs := map[string]string{
		"class":               cls,
		"data-hui-drop":       "",
		"data-hui-drop-input": id,
		"data-hui-drop-one":   words.FileSelected,
		"data-hui-drop-many":  words.FilesSelected,
	}
	// The caller's extras go on first and the owned hooks over them,
	// so a forged data-hui-* key cannot retarget the drop or rewrite
	// the announcement. Every headless component resolves the same
	// collision the same way: what the component owns wins.
	merged := map[string]string{}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs) {
		if strings.HasPrefix(strings.ToLower(k), "data-hui-") {
			continue
		}
		merged[k] = v
	}
	for k, v := range rootAttrs {
		merged[k] = v
	}
	return dropzoneStyle.WrapHTML(render.Tag("div", merged, children...))
}

// dropzoneHelp joins the caller's Help with the localized size hint.
func dropzoneHelp(cfg FileDropzoneConfig, ctx context.Context) string {
	if cfg.MaxSizeMB <= 0 {
		return cfg.Help
	}
	n := strconv.Itoa(cfg.MaxSizeMB)
	if cfg.Help == "" {
		return i18nui.TVars(ctx, i18nui.KeyDropzoneMaxSize, map[string]string{"n": n})
	}
	return cfg.Help + i18nui.TVars(ctx, i18nui.KeyDropzoneMaxSizeSuffix, map[string]string{"n": n})
}

func dropzoneIcon() string {
	return `<svg width="40" height="40" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 16V4M12 4l-4 4m4-4l4 4M4 20h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

var dropzoneStyle = registry.RegisterStyle("ui-dropzone", dropzoneCSS)

func dropzoneCSS(_ style.Theme) string {
	return `.fui-drop {
  display: grid;
  gap: var(--spacing-md, 8px);
}
.fui-drop__label-wrap {
  display: block;
  cursor: pointer;
}
.fui-drop__zone {
  display: grid;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xl, 24px) var(--spacing-lg, 16px);
  border: 2px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
  background: var(--color-surface, #FFFFFF);
  text-align: center;
  transition: border-color 120ms ease, background 120ms ease;
}
.fui-drop[data-hui-drop-over] .fui-drop__zone,
.fui-drop__label-wrap:hover .fui-drop__zone {
  border-color: var(--color-primary, #4F46E5);
  background: color-mix(in srgb, var(--color-primary, #4F46E5) 10%, var(--color-surface, #FFFFFF));
  border-style: solid;
}
/* Slight lift for tactile feedback while a drag is over. */
.fui-drop[data-hui-drop-over] .fui-drop__icon {
  transform: translateY(-2px);
  transition: transform 120ms ease;
}
.fui-drop__input {
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
.fui-drop__input:focus-visible + .fui-drop__icon {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 4px;
  border-radius: var(--radii-sm, 4px);
}
.fui-drop__icon {
  color: var(--color-primary, #4F46E5);
}
.fui-drop__label {
  margin: 0;
  font-size: var(--text-base, 1rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
}
.fui-drop__prompt {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
.fui-drop__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 1px;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 600;
  color: var(--color-primary, #4F46E5);
  justify-items: start;
}
.fui-drop__list:empty { display: none; }
.fui-drop__status {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
.fui-drop__status:empty { display: none; }
.fui-drop__previews {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 4px);
}
.fui-drop__preview {
  width: 72px;
  height: 72px;
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface-soft, #F4F4F5);
  object-fit: cover;
  border: 1px solid var(--color-border, #E4E4E7);
}
.fui-drop__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
.fui-drop__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #DC2626);
}
.fui-drop.is-error .fui-drop__zone {
  border-color: var(--color-danger, #DC2626);
}
.fui-drop.is-disabled .fui-drop__zone {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
