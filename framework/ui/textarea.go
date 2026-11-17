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

// TextAreaConfig configures a TextArea.
type TextAreaConfig struct {
	// Name is the form-field name. Required.
	Name string
	// Label is the accessible label. Required.
	Label       string
	Value       string
	Placeholder string
	// Rows is the initial row count; zero is 3.
	Rows int
	// Autogrow grows the height with the content as it is typed.
	Autogrow bool
	// Monospace draws the text in the mono font token.
	Monospace bool
	// JSON checks the text as JSON in the browser; the server still validates.
	JSON bool
	// Ctx resolves the JSON check's sentence; nil reads English.
	Ctx       context.Context
	Required  bool
	Disabled  bool
	Help      string
	Error     string
	MinLength int
	MaxLength int
	// ID is the control's id; empty is Name.
	ID    string
	Class string
	// ExtraAttrs lands on the <textarea>; keys the component owns are dropped.
	ExtraAttrs html.Attrs
}

// TextArea renders a labelled multi-line text input.
func TextArea(cfg TextAreaConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: TextArea requires Name")
	}
	if cfg.Label == "" {
		panic("ui: TextArea requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	rows := cfg.Rows
	if rows == 0 {
		rows = 3
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"name", "rows", "placeholder", "disabled", "required", "minlength", "maxlength",
		"aria-invalid", "aria-describedby")
	if extra == nil {
		extra = html.Attrs{}
	}
	if cfg.MinLength > 0 {
		extra["minlength"] = strconv.Itoa(cfg.MinLength)
	}
	if cfg.MaxLength > 0 {
		extra["maxlength"] = strconv.Itoa(cfg.MaxLength)
	}

	control := func(c headless.FieldControl) render.HTML {
		return headless.Own(textAreaStyle.WrapHTML(headless.Textarea(headless.TextareaProps{
			Name:        cfg.Name,
			DescribedBy: c.DescribedBy,
			Value:       cfg.Value,
			Placeholder: cfg.Placeholder,
			Rows:        rows,
			Required:    c.Required,
			Disabled:    cfg.Disabled,
			Invalid:     c.Invalid,
			Autogrow:    cfg.Autogrow,
			JSON:        jsonSentence(cfg.JSON, cfg.Ctx),
			ID:          c.ID,
			Extra:       extra,
		}, textAreaClasses)))
	}
	class := cfg.Class
	if cfg.Monospace {
		class = strings.TrimSpace("fui-textarea--mono " + class)
	}
	return formFieldStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label:    cfg.Label,
		For:      id,
		Hint:     cfg.Help,
		Error:    cfg.Error,
		Required: cfg.Required,
		Parts:    rootClassParts(class),
	}, fieldClasses, control))
}

func jsonSentence(on bool, ctx context.Context) string {
	if !on {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return i18nui.TextAreaInvalidJSON.Text(ctx)
}

var textAreaStyle = registry.RegisterStyle("ui-textarea", textAreaCSS)

func textAreaCSS(_ style.Theme) string {
	return `.fui-textarea {
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  padding: var(--ui-control-padding-y, 10px) calc(var(--spacing-sm, 4px) * 3);
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #09090B);
  box-shadow: var(--shadow-xs);
  resize: vertical;
  min-block-size: var(--fui-density-control-h);
  line-height: var(--leading-normal, 1.5);
}
.fui-textarea[data-cui-autogrow] {
  /* Autogrow rules the height; user resize would fight the JS. */
  resize: none;
  overflow: hidden;
}
.fui-textarea::placeholder { color: var(--color-text-subtle); }
.fui-textarea:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
.fui-textarea[aria-invalid="true"] {
  border-color: var(--color-danger, #B91C1C);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-danger, #B91C1C);
}
.fui-textarea--mono .fui-textarea {
  /* JSON and code: the mono token every code-ish component reads.
     The variant class rides the FIELD root (TextArea's Class), so it
     reaches the control as a descendant. */
  font-family: var(--font-mono, ui-monospace, 'SF Mono', Menlo, Consolas, monospace);
  tab-size: 2;
}
.fui-textarea:disabled {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) { .fui-textarea { font-size: var(--text-base, 1rem); } }`
}
