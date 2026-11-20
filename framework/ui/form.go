package ui

import (
	"context"
	"html/template"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// FieldErrors maps field names to messages, as ValidationRegistry.Validate returns.
type FieldErrors map[string]string

// FormConfig wraps a server-rendered <form>.
type FormConfig struct {
	// Action is the form's URL. Required; one the anchor policy refuses panics.
	Action string

	// Method is the native method, "POST" (default) or "GET".
	Method string

	// Errors renders a ValidationSummary above the fields; it requires ID.
	Errors FieldErrors

	// Summary is the summary's sentence that belongs to no one field.
	Summary string

	// FieldLabels, FieldIDs and FieldOrder are passed to the ValidationSummary.
	FieldLabels map[string]string
	FieldIDs    map[string]string
	FieldOrder  []string

	// SubmitLabel is the submit button's label; empty reads "Save".
	SubmitLabel string

	// HideSubmit omits the submit button.
	HideSubmit bool

	// SubmitFullWidth stretches the submit button to the form's width.
	SubmitFullWidth bool

	// Wide lifts the readable-measure cap so a FormFrame inside can split.
	Wide bool

	NoValidate bool

	// Multipart sets enctype="multipart/form-data".
	Multipart bool

	// Ctx, on a POST form, stamps the request's masked CSRF token as _csrf.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs lands on the <form>; wiring keys go through the request seam.
	ExtraAttrs html.Attrs

	// LeaveGuard is the sentence asked before unsaved edits are left.
	LeaveGuard string
	// LeaveGuardTitle and LeaveGuardAccept word the guard's dialog.
	LeaveGuardTitle  string
	LeaveGuardAccept string
}

// Form renders a <form>: an optional error summary, the fields, a submit button.
func Form(cfg FormConfig, fields ...render.HTML) render.HTML {
	if cfg.Action == "" {
		panic("ui: Form requires Action")
	}
	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	if method != "GET" && method != "POST" {
		panic("ui: Form Method must be GET or POST, got " + method)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	body := make([]render.HTML, 0, len(fields)+2)

	if method == "POST" && cfg.Ctx != nil {
		if tok := middleware.TokenFromContext(cfg.Ctx); tok != "" {
			body = append(body, csrfHiddenInput(middleware.MaskCSRFToken(tok)))
		}
	}
	body = append(body, fields...)

	var actions render.HTML
	if !cfg.HideSubmit {
		submitLabel := cfg.SubmitLabel
		if submitLabel == "" {
			submitLabel = i18nui.FormSave.Text(ctx)
		}
		actions = headless.Own(Button(ButtonConfig{Ctx: ctx, Label: submitLabel, Type: "submit"}))
	}

	var errorsHTML render.HTML
	if len(cfg.Errors) > 0 || cfg.Summary != "" {
		errorsHTML = formErrorsSummary(ctx, "Form", cfg.ID, cfg.Summary, cfg.Errors, cfg.FieldLabels, cfg.FieldIDs, cfg.FieldOrder)
	}

	rootClass := cfg.Class
	if cfg.SubmitFullWidth {
		rootClass = strings.TrimSpace(rootClass + " fui-form--block-actions")
	}
	if cfg.Wide {
		rootClass = strings.TrimSpace(rootClass + " fui-form--wide")
	}

	request, plain := splitFormAttrs(cfg.ExtraAttrs)
	return formStyle.WrapHTML(headless.Form(headless.FormProps{
		Action:           cfg.Action,
		Method:           method,
		Errors:           errorsHTML,
		Actions:          actions,
		NoValidate:       cfg.NoValidate,
		Multipart:        cfg.Multipart,
		LeaveGuard:       cfg.LeaveGuard,
		LeaveGuardTitle:  cfg.LeaveGuardTitle,
		LeaveGuardAccept: cfg.LeaveGuardAccept,
		Request:          request,
		ID:               cfg.ID,
		ExtraAttrs:       plain,
		Parts:            rootClassParts(rootClass),
	}, formClasses, body...))
}

func splitFormAttrs(extra html.Attrs) (request, plain html.Attrs) {
	request, plain = html.Attrs{}, html.Attrs{}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := request[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		if _, dup := plain[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		switch {
		case strings.HasPrefix(lk, "data-cui-"), strings.HasPrefix(lk, "data-fui-"), strings.HasPrefix(lk, "data-action-"):
			request[lk] = v
		default:
			plain[lk] = v
		}
	}
	return request, plain
}

const csrfFormField = "_csrf"

func csrfHiddenInput(tok string) render.HTML {
	//gofastr:allow(GOFASTR1403) the token goes through template.HTMLEscapeString
	return render.HTML(`<input type="hidden" name="` + csrfFormField +
		`" value="` + template.HTMLEscapeString(tok) + `">`)
}

// FormFieldFor renders a FormField whose Error is errs[name].
func FormFieldFor(errs FieldErrors, name string, cfg FormFieldConfig) render.HTML {
	if errs != nil {
		if msg, ok := errs[name]; ok {
			cfg.Error = msg
		}
	}
	return FormField(cfg)
}

// ValidationSummaryConfig configures a ValidationSummary.
type ValidationSummaryConfig struct {
	// ID names the summary's root. Required.
	ID     string
	Errors FieldErrors
	// General is a text row, after the field errors, for no one field.
	General string
	// FieldLabels names each field in its row; the field name otherwise.
	FieldLabels map[string]string
	// FieldIDs maps field names to control ids; nil targets the field name.
	FieldIDs map[string]string
	// FieldOrder orders the rows; the rest follow alphabetically.
	FieldOrder []string
	Title      string
	Class      string
	ExtraAttrs html.Attrs
	// Ctx resolves the default title; nil reads English.
	Ctx context.Context
}

// ValidationSummary renders a form's errors, each linking to its field.
func ValidationSummary(cfg ValidationSummaryConfig) render.HTML {
	if len(cfg.Errors) == 0 && cfg.General == "" {
		return ""
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	ordered := make([]string, 0, len(cfg.Errors))
	seen := make(map[string]bool, len(cfg.Errors))
	for _, name := range cfg.FieldOrder {
		if _, ok := cfg.Errors[name]; ok && !seen[name] {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	leftover := make([]string, 0)
	for name := range cfg.Errors {
		if !seen[name] {
			leftover = append(leftover, name)
		}
	}
	sort.Strings(leftover)
	ordered = append(ordered, leftover...)

	errs := make([]headless.FieldError, 0, len(ordered)+1)
	for _, field := range ordered {
		msg := cfg.Errors[field]
		label := field
		if l, ok := cfg.FieldLabels[field]; ok {
			label = l
		}
		target := field
		if cfg.FieldIDs != nil {
			if id, ok := cfg.FieldIDs[field]; ok {
				target = id
			} else {
				target = ""
			}
		}
		errs = append(errs, headless.FieldError{For: target, Message: label + ": " + msg})
	}
	if cfg.General != "" {
		errs = append(errs, headless.FieldError{Message: cfg.General})
	}

	titleText := cfg.Title
	if titleText == "" {
		titleText = i18nui.ValidationSummaryTitle.Text(ctx)
	}
	return validationSummaryStyle.WrapHTML(headless.ValidationSummary(headless.ValidationSummaryProps{
		Title:   titleText,
		Level:   2,
		Errors:  errs,
		ID:      cfg.ID,
		Strings: StringsFor(ctx),
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs,
			"role", "tabindex", "aria-labelledby"),
	}, withRootClass(validationSummaryClasses, cfg.Class)))
}

var validationSummaryStyle = registry.RegisterStyle("ui-validation-summary", validationSummaryCSS)

func validationSummaryCSS(_ style.Theme) string {
	return `.fui-validation-summary {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: calc(var(--spacing-sm, 4px) * 3) var(--spacing-lg, 16px);
  /* The destructive-alert shape Banner uses: the danger hue tints the
     hairline and the title, with no side stripe. */
  border: var(--stroke-thin, 1px) solid color-mix(in oklab, var(--color-danger, #B91C1C) 35%, var(--color-border, #E4E4E7));
  border-radius: var(--radii-lg, 10px);
  background: var(--color-surface, #FFFFFF);
}
.fui-validation-summary:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
.fui-validation-summary__title {
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  margin: 0;
  color: var(--color-danger, #B91C1C);
}
.fui-validation-summary__list {
  margin: 0;
  padding-inline-start: var(--spacing-lg, 16px);
  list-style: disc;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
}
.fui-validation-summary__list a {
  display: inline-flex;
  align-items: center;
  min-block-size: var(--ui-validation-summary-link-min-height, 24px);
  color: var(--color-danger, #B91C1C);
  text-decoration: underline;
}
.fui-validation-summary__list a:hover {
  color: var(--color-text, #09090B);
}`
}

// formErrorsSummary renders the ValidationSummary Form and StepWizard share
// when they carry errors or a summary. It panics when id is empty: the
// summary's id derives from it, and two summaries on one page would share
// one title id. component names the caller in that panic.
func formErrorsSummary(ctx context.Context, component, id, summary string, errs FieldErrors, fieldLabels, fieldIDs map[string]string, fieldOrder []string) render.HTML {
	if id == "" {
		panic("ui: " + component + " rendering Errors requires ID — the summary's id is derived from it (" + component + "Config.ID + \"-errors\"), and two summaries on one page would share one title id")
	}
	general := summary
	if general == "" {
		general = i18nui.FormErrorsSummary.Text(ctx)
	}
	return headless.Own(ValidationSummary(ValidationSummaryConfig{
		ID:          id + "-errors",
		Errors:      errs,
		General:     general,
		FieldLabels: fieldLabels,
		FieldIDs:    fieldIDs,
		FieldOrder:  fieldOrder,
		Title:       i18nui.FormHasErrors.Text(ctx),
		Ctx:         ctx,
	}))
}
