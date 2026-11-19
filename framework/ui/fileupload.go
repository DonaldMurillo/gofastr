package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// FileUploadConfig configures a file upload.
type FileUploadConfig struct {
	// Name and Label are required; Label is shown inside the drop zone.
	Name  string
	Label string
	// ID is the input's id. Defaults to Name.
	ID       string
	Accept   string
	Multiple bool
	Required bool
	Disabled bool
	// MaxSizeMB is announced in the hint; the server enforces it.
	MaxSizeMB int
	// Help is shown in the zone beside the size hint.
	Help string
	// Error marks the input invalid and renders below the zone as an
	// alert the input's aria-describedby names.
	Error string
	Class string
	// ExtraAttrs forwards attributes to the root; keys the component
	// owns, every data-hui-* key included, are dropped.
	ExtraAttrs html.Attrs
	// Ctx resolves the drop sentence and announcements. Nil means English.
	Ctx context.Context
}

// FileUpload renders a drag-drop file picker whose zone is a label
// for the real file input.
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
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	prompt := i18nui.FileUploadDrop.Text(ctx)
	if !cfg.Multiple {
		prompt = i18nui.FileUploadDropSingle.Text(ctx)
	}

	field := headless.FileUpload(headless.FileUploadProps{
		Name:     cfg.Name,
		ID:       id,
		Label:    cfg.Label,
		CTA:      prompt,
		Hint:     uploadHelpText(cfg),
		Accept:   cfg.Accept,
		Multiple: cfg.Multiple,
		Required: cfg.Required,
		Disabled: cfg.Disabled,
		Invalid:  cfg.Error != "",
		Strings:  StringsFor(ctx),
		DescribedBy: func() string {
			if cfg.Error != "" {
				return id + "-error"
			}
			return ""
		}(),
	}, uploadClasses)
	children := []render.HTML{headless.Own(field)}
	if cfg.Error != "" {
		children = append(children, render.Tag("p", html.Attrs{
			"id":                id + "-error",
			"class":             "fui-upload__error",
			"role":              "alert",
			"data-cui-internal": "",
		}, render.Text(cfg.Error)))
	}

	cls := "fui-upload-field"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs) {
		if strings.HasPrefix(strings.ToLower(k), "data-hui-") {
			continue
		}
		attrs[k] = v
	}
	attrs["class"] = cls
	return fileUploadStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// uploadHelpText joins the caller's Help with the localized size hint.
func uploadHelpText(cfg FileUploadConfig) string {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	bits := []string{}
	if cfg.Help != "" {
		bits = append(bits, cfg.Help)
	}
	if cfg.MaxSizeMB > 0 {
		bits = append(bits, i18nui.FileMaxSize.Text(ctx, map[string]any{"n": cfg.MaxSizeMB}))
	}
	return strings.Join(bits, " · ")
}

var fileUploadStyle = registry.RegisterStyle("ui-fileupload", fileUploadCSS)

func fileUploadCSS(_ style.Theme) string {
	return `.fui-upload-field {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
.fui-upload-field:has(.fui-upload__input:disabled) {
  opacity: var(--opacity-muted, 0.6);
}
.fui-upload {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
.fui-upload__zone {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xl, 24px);
  border: var(--stroke-thick, 2px) dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text-muted, #52525B);
  text-align: center;
  cursor: pointer;
  transition: border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease),
              background var(--duration-fast, 150ms) ease;
  min-block-size: calc(var(--spacing-touch-target, 44px) * 2);
}
.fui-upload__zone:hover,
.fui-upload[data-hui-drop-over] .fui-upload__zone {
  border-color: var(--color-primary, #18181B);
}
.fui-upload[data-hui-drop-over] .fui-upload__zone {
  background: color-mix(in oklab, var(--color-primary, #18181B) 10%, var(--color-surface, #FFFFFF) 90%);
}
.fui-upload-field:has(.fui-upload__input:is(:user-invalid, [aria-invalid="true"])) .fui-upload__zone {
  border-color: var(--color-danger, #B91C1C);
}
.fui-upload__label {
  font-weight: var(--font-weight-semibold);
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #09090B);
}
.fui-upload__cta,
.fui-upload__hint {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
/* The input is fully present and focusable, only unseen: it must stay
   in the accessibility tree, so it is clipped, never display:none. */
.fui-upload__input {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0,0,0,0);
  white-space: nowrap;
  border: 0;
}
/* The zone is the visual control, so it is what the focus ring drawn
   for the input's focus has to sit on. */
.fui-upload:has(.fui-upload__input:focus-visible) .fui-upload__zone {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(2 * var(--stroke-focus-offset, 2px));
}
.fui-upload__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 1px;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
  justify-items: start;
}
.fui-upload__list:empty { display: none; }
.fui-upload__status {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
.fui-upload__status:empty { display: none; }
.fui-upload__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #B91C1C);
}`
}
