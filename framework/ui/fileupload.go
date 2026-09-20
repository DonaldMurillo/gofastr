package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FileUpload ─────────────────────────────────────────────────────
//
// A labelled file picker rendered through headless.FileUpload: the
// drop zone is a <label> for the real <input type="file">, so the
// whole target opens the picker with no script at all (WCAG 2.5.7 —
// dragging is an enhancement, never the only route). The headless
// behaviour module owns the drag-and-drop on data-hui-drop: it
// forwards dropped files into the input, lists the chosen names in
// the role="list" region, and announces the pick through the
// role="status" span, saying the FileSelected / FilesSelected
// sentences this component resolves per request through the strings
// bridge.

// FileUploadConfig configures a file upload.
type FileUploadConfig struct {
	// Name is the form-field name. Required.
	Name string

	// Label is the visible label inside the drop zone. Required.
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

	// MaxSizeMB, when > 0, is announced in the hint so users
	// understand the constraint. The native input doesn't enforce
	// it; server-side validation must.
	MaxSizeMB int

	// Help renders supporting text inside the drop zone (beside the
	// size hint, joined with " · ").
	Help string

	// Error overrides Help's described-by slot and switches the field
	// to error state: the input is marked aria-invalid and the
	// message renders below the zone as a role="alert" paragraph the
	// input's aria-describedby names.
	Error string

	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the component's root
	// element. Keys the component owns are dropped: class (use
	// Class), id, and data-fui-*.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the zone's
	// drop sentence and the announcement sentences. When nil, English
	// fallbacks apply.
	Ctx context.Context
}

// FileUpload renders a drag-drop file picker.
//
// Markup shape:
//
//	<div class="fui-upload-field" data-fui-comp="ui-fileupload">
//	  <div class="fui-upload" data-hui-drop …>
//	    <label class="fui-upload__zone" for="…">
//	      <span class="fui-upload__label">…</span>
//	      <span class="fui-upload__cta">Drop a file here, or click to browse</span>
//	      <span class="fui-upload__hint">…</span>
//	    </label>
//	    <input type="file" class="fui-upload__input" …>
//	    <ul class="fui-upload__list" role="list" data-hui-drop-list></ul>
//	    <span class="fui-upload__status" role="status" data-hui-drop-status></span>
//	  </div>
//	  <p class="fui-upload__error" role="alert">…</p>  ← only when Error is set
//	</div>
//
// The list and the status are filled by the headless module after a
// pick or a drop, so the chosen names are on screen as well as
// announced.
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
	prompt := i18nui.T(ctx, i18nui.KeyFileUploadDrop)
	if !cfg.Multiple {
		prompt = i18nui.T(ctx, i18nui.KeyFileUploadDropSingle)
	}

	children := []render.HTML{headless.FileUpload(headless.FileUploadProps{
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
	}, uploadClasses)}
	if cfg.Error != "" {
		children = append(children, render.Tag("p", html.Attrs{
			"id":    id + "-error",
			"class": "fui-upload__error",
			"role":  "alert",
		}, render.Text(cfg.Error)))
	}

	cls := "fui-upload-field"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// The field wrapper takes the caller's extras, minus the runtime's
	// own vocabulary: a forged data-hui-drop here would arm a second,
	// mostly inert drop root around the real one. The hooks belong to
	// the zone the component renders.
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
		bits = append(bits, i18nui.TVars(ctx, i18nui.KeyFileMaxSize, map[string]string{"n": strconv.Itoa(cfg.MaxSizeMB)}))
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
  opacity: 0.6;
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
  border: 2px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text-muted, #52525B);
  text-align: center;
  cursor: pointer;
  transition: border-color var(--duration-fast, 150ms) ease,
              background var(--duration-fast, 150ms) ease;
  min-block-size: calc(var(--spacing-touch-target, 44px) * 2);
}
.fui-upload__zone:hover,
.fui-upload[data-hui-drop-over] .fui-upload__zone {
  border-color: var(--color-primary, #4F46E5);
}
.fui-upload[data-hui-drop-over] .fui-upload__zone {
  background: color-mix(in oklab, var(--color-primary, #4F46E5) 10%, var(--color-surface, #FFFFFF) 90%);
}
.fui-upload-field:has(.fui-upload__input[aria-invalid="true"]) .fui-upload__zone {
  border-color: var(--color-danger, #DC2626);
}
.fui-upload__label {
  font-weight: 600;
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
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
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 4px;
}
.fui-upload__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 1px;
  font-size: var(--text-sm, 0.875rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
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
  color: var(--color-danger, #DC2626);
}`
}
