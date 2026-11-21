package ui

import (
	"context"
	_ "embed"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

//go:embed filedropzone.js
var fileDropzoneJS string

var _ = registry.RegisterBehavior("filedropzone", fileDropzoneJS,
	registry.Markers("[data-fui-dropzone-preview]"))

// FileDropzoneConfig configures a FileDropzone.
type FileDropzoneConfig struct {
	// Name and Label are required; Label names the input and heads the zone.
	Name  string
	Label string
	// Prompt overrides the localized call to action.
	Prompt   string
	Accept   string
	Multiple bool
	Required bool
	Disabled bool
	// ShowPreview adds a thumbnail strip for picked images.
	ShowPreview bool
	// MaxSizeMB is announced in the help text; the server enforces it.
	MaxSizeMB int
	Help      string
	// Error shows below Help (which stays visible), marks the input
	// invalid, and leads its aria-describedby: "<id>-error <id>-help".
	Error string
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root div; keys the
	// component owns, every data-hui-* key included, are dropped.
	ExtraAttrs html.Attrs

	// Ctx resolves the prompt, size hint and announcements. Nil means English.
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
			prompt = i18nui.DropzoneDropFiles.Text(ctx)
		} else {
			prompt = i18nui.DropzoneDropFile.Text(ctx)
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
	if cfg.ShowPreview {
		inputAttrs["data-fui-dropzone-preview"] = ""
	}
	if cfg.Error != "" {
		inputAttrs["aria-invalid"] = "true"
	}
	var described []string
	if cfg.Error != "" {
		described = append(described, id+"-error")
	}
	if dropzoneHelp(cfg, ctx) != "" {
		described = append(described, id+"-help")
	}
	if len(described) > 0 {
		inputAttrs["aria-describedby"] = strings.Join(described, " ")
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
		map[string]string{"for": id, "class": "fui-drop__label-wrap", "data-cui-internal": ""},
		render.Tag("div", map[string]string{
			"class":      "fui-drop__zone",
			"role":       "region",
			"aria-label": cfg.Label,
		}, zoneChildren...),
	)

	children := []render.HTML{zone}
	children = append(children,
		render.Tag("ul", map[string]string{
			"class":              "fui-drop__list",
			"role":               "list",
			"data-hui-drop-list": "",
			"data-cui-internal":  "",
		}),
		render.Tag("span", map[string]string{
			"class":                "fui-drop__status",
			"role":                 "status",
			"data-hui-drop-status": "",
			"data-cui-internal":    "",
		}),
	)
	if cfg.ShowPreview {
		children = append(children, render.Tag("div", map[string]string{
			"class":                         "fui-drop__previews",
			"data-fui-dropzone-preview-for": id,
			"aria-live":                     "polite",
			"data-cui-internal":             "",
		}))
	}

	if help := dropzoneHelp(cfg, ctx); help != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id": id + "-help", "class": "fui-drop__help", "data-cui-internal": "",
		}, render.Text(help)))
	}
	if cfg.Error != "" {
		children = append(children, render.Tag("p", map[string]string{
			"id": id + "-error", "class": "fui-drop__error", "data-cui-internal": "",
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

	rootAttrs := map[string]string{
		"class":               cls,
		"data-hui-drop":       "",
		"data-hui-drop-input": id,
		"data-hui-drop-one":   words.FileSelected,
		"data-hui-drop-many":  words.FilesSelected,
	}
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
	if cfg.Help == "" {
		return i18nui.DropzoneMaxSize.Text(ctx, map[string]any{"n": cfg.MaxSizeMB})
	}
	return cfg.Help + i18nui.DropzoneMaxSizeSuffix.Text(ctx, map[string]any{"n": cfg.MaxSizeMB})
}

func dropzoneIcon() string {
	return string(Icon("upload", IconConfig{Size: "40"}))
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
  border: var(--stroke-thick, 2px) dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  background: var(--color-surface, #FFFFFF);
  text-align: center;
  transition: border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
.fui-drop[data-hui-drop-over] .fui-drop__zone,
.fui-drop__label-wrap:hover .fui-drop__zone {
  border-color: var(--color-primary, #18181B);
  background: color-mix(in oklab, var(--color-primary, #18181B) 10%, var(--color-surface, #FFFFFF));
  border-style: solid;
}
/* Slight lift for tactile feedback while a drag is over. */
.fui-drop[data-hui-drop-over] .fui-drop__icon {
  transform: translateY(-2px);
  transition: transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
.fui-drop__input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
.fui-drop__input:focus-visible + .fui-drop__icon {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(2 * var(--stroke-focus-offset, 2px));
  border-radius: var(--radii-sm, 6px);
}
.fui-drop__icon {
  color: var(--color-primary, #18181B);
}
.fui-drop__label {
  margin: 0;
  font-size: var(--text-base, 1rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
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
  font-weight: var(--font-weight-semibold);
  color: var(--color-primary, #18181B);
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
  width: var(--ui-drop-preview-size, 72px);
  height: var(--ui-drop-preview-size, 72px);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #F4F4F5);
  object-fit: cover;
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
.fui-drop__help {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
.fui-drop__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #B91C1C);
}
.fui-drop.is-error .fui-drop__zone {
  border-color: var(--color-danger, #B91C1C);
}
.fui-drop.is-disabled .fui-drop__zone {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}`
}
