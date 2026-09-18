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

// FieldErrors maps form field names to user-visible error messages.
// It is the same shape returned by [framework.ValidationRegistry.Validate]
// so server-side validation results round-trip directly into FormField.
//
// Example flow:
//
//	errors := registry.Validate(ctx, formData)  // framework.ValidationRegistry
//	page := ui.Form(ui.FormConfig{
//	    Action: "/customers", ID: "customer-form",
//	    Errors: errors,
//	},
//	    ui.FormFieldFor(errors, "email", ...),
//	    ui.FormFieldFor(errors, "name",  ...),
//	)
type FieldErrors map[string]string

// FormConfig wraps a server-rendered <form>.
type FormConfig struct {
	// Action is the form's action URL. Required. An action the anchor
	// policy refuses is a panic at render, not a silent "#" — a form
	// pointed at a dangerous URL is worse than a no-op, and a dead
	// one is found where it was made.
	Action string

	// Method is "POST" (default) or "GET". This is the NATIVE method,
	// what a scriptless browser submits; the request seam's method
	// (see ExtraAttrs) is independent of it.
	Method string

	// Errors is an optional set of field-level errors. When non-empty
	// the form renders a ValidationSummary above its fields, marked so
	// the headless behaviour module moves focus to the summary after a
	// failed submit — which requires ID (the summary's id is derived
	// from it), and the per-field wiring reaches FormFieldFor'd fields
	// automatically.
	Errors FieldErrors

	// Summary is a sentence that belongs to no one field (a general
	// failure, a credentials mismatch). It renders as a text row in
	// the validation summary, after the field errors. Empty means
	// nothing when Errors is empty, and the framework default
	// ("Please fix the highlighted fields and try again.") when
	// Errors is not.
	Summary string

	// FieldLabels, FieldIDs and FieldOrder are passed to the
	// ValidationSummary: FieldIDs maps a field name to its control's
	// id (a link to #email misses a control whose id is f_email —
	// an error whose field has no known id renders as text, not as an
	// anchor to nothing), FieldLabels supplies each link's label, and
	// FieldOrder fixes the row order.
	FieldLabels map[string]string
	FieldIDs    map[string]string
	FieldOrder  []string

	// SubmitLabel is the visible submit button label. Defaults to "Save".
	SubmitLabel string

	// HideSubmit omits the submit button entirely when true.
	// Use when the caller renders its own submit button.
	HideSubmit bool

	// SubmitFullWidth stacks the actions row into a single full-width
	// column and stretches the submit button to the form's width. Use
	// for mobile-first auth and wizard forms where the primary action
	// should be a thumb target spanning the card.
	SubmitFullWidth bool

	// NoValidate turns off the browser's own validation bubbles, for a
	// form that validates on the server and reports through Errors.
	NoValidate bool

	// Ctx, when non-nil, lets Form auto-stamp the hidden CSRF input
	// (the framework's "_csrf" field) on unsafe-method submits. It
	// reads middleware.TokenFromContext(Ctx), i.e. the token the CSRF
	// middleware stashes on every request, so callers do not have to
	// remember `render.HTML(csrfInput(ctx))` as the first child of
	// every form. Nil-safe: a form rendered without Ctx omits the
	// hidden input (matches pre-v3 behavior so existing tests and
	// non-CSRF flows like Method:"GET" stay correct).
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <form>
	// element. Every data-fui-* and data-action-* key is runtime
	// wiring and goes through the typed request seam (attach island
	// wiring with interactive.Post(...).OnSuccess(...).Attrs() —
	// headless.FormProps.Request admits exactly that vocabulary and
	// panics on any wiring key outside it, where the old carrier
	// contract silently rendered a plain form that posts natively).
	// Everything else is decoration and passes through.
	ExtraAttrs html.Attrs
}

// Form renders a complete <form> with an optional error summary above
// the fields and a submit button below, through the headless form
// dressed with this package's class map.
//
// Pass FormFieldFor(errors, ...) inside as fields so the per-field
// error wiring is automatic.
func Form(cfg FormConfig, fields ...render.HTML) render.HTML {
	if cfg.Action == "" {
		panic("ui: Form requires Action")
	}
	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	// D-1: Reject invalid methods to prevent silent HTML bugs.
	if method != "GET" && method != "POST" {
		panic("ui: Form Method must be GET or POST, got " + method)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	body := make([]render.HTML, 0, len(fields)+2)

	// Auto-embed the hidden CSRF input on unsafe-method submits when a
	// request ctx is available. POST is the only unsafe method Form
	// accepts (panic above), so the check is simply "POST + ctx + token
	// on ctx". GET forms get nothing. They aren't behind CSRF.
	if method == "POST" && cfg.Ctx != nil {
		if tok := middleware.TokenFromContext(cfg.Ctx); tok != "" {
			body = append(body, csrfHiddenInput(tok))
		}
	}
	body = append(body, fields...)

	var actions render.HTML
	if !cfg.HideSubmit {
		submitLabel := cfg.SubmitLabel
		if submitLabel == "" {
			submitLabel = i18nui.T(ctx, i18nui.KeyFormSave)
		}
		actions = Button(ButtonConfig{Label: submitLabel, Type: "submit"})
	}

	// The summary, when there is one, renders above the body and marks
	// the form so the behaviour module moves focus to it on arrival.
	// A general sentence with no field errors is still a failure the
	// reader has to see: a save refused by a guard or a conflict names
	// no field, and gating the summary on Errors alone rendered
	// nothing at all for it.
	var errorsHTML render.HTML
	if len(cfg.Errors) > 0 || cfg.Summary != "" {
		if cfg.ID == "" {
			panic("ui: Form rendering Errors requires ID — the summary's id is derived from it (FormConfig.ID + \"-errors\"), and two summaries on one page would share one title id")
		}
		general := cfg.Summary
		if general == "" {
			general = i18nui.T(ctx, i18nui.KeyFormErrorsSummary)
		}
		errorsHTML = ValidationSummary(ValidationSummaryConfig{
			ID:          cfg.ID + "-errors",
			Errors:      cfg.Errors,
			General:     general,
			FieldLabels: cfg.FieldLabels,
			FieldIDs:    cfg.FieldIDs,
			FieldOrder:  cfg.FieldOrder,
			Title:       i18nui.T(ctx, i18nui.KeyFormHasErrors),
			Ctx:         ctx,
		})
	}

	rootClass := cfg.Class
	if cfg.SubmitFullWidth {
		rootClass = strings.TrimSpace(rootClass + " fui-form--block-actions")
	}

	request, plain := splitFormAttrs(cfg.ExtraAttrs)
	// The refusal headless applies to a rejected Action is this
	// package's too: today's "#" substitution shipped a form whose
	// submit went nowhere, which is worse than a panic at render.
	return formStyle.WrapHTML(headless.Form(headless.FormProps{
		Action:     cfg.Action,
		Method:     method,
		Errors:     errorsHTML,
		Actions:    actions,
		NoValidate: cfg.NoValidate,
		Request:    request,
		ID:         cfg.ID,
		ExtraAttrs: plain,
		Parts:      rootClassParts(rootClass),
	}, formClasses, body...))
}

// splitFormAttrs splits a Form's ExtraAttrs at the seam: every
// data-fui-* and data-action-* key is runtime wiring and travels
// through the typed Request, where headless admits exactly the request
// vocabulary a form reads and panics on anything else, naming the key;
// everything else is decoration and travels through headless's
// ExtraAttrs, whose Safe drops the keys the component owns.
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
		case strings.HasPrefix(lk, "data-fui-"), strings.HasPrefix(lk, "data-action-"):
			request[lk] = v
		default:
			plain[lk] = v
		}
	}
	return request, plain
}

// csrfFormField is the hidden-input name Form emits when Ctx carries a
// CSRF token. It matches the framework's default (battery/auth.CSRFFormField
// + middleware.CSRFConfig.FormField when unset). Hosts that override the
// form field name in their CSRF config should NOT use Form's auto-embed.
// Render their own hidden input via auth.CSRFInputFromCtx instead.
const csrfFormField = "_csrf"

// csrfHiddenInput renders the same markup as battery/auth.CSRFInputFromCtx
// but without the battery/auth dependency (which would create a layering
// cycle since framework/ui sits below battery/*). Token values are
// base64url + "." + base64url(HMAC) so HTMLEscapeString is defense-in-depth.
func csrfHiddenInput(tok string) render.HTML {
	return render.HTML(`<input type="hidden" name="` + csrfFormField +
		`" value="` + template.HTMLEscapeString(tok) + `">`)
}

// FormFieldFor is a convenience wrapper that pre-fills FormFieldConfig.Error
// from a FieldErrors map. Use it inside Form() so error round-tripping
// is one line per field.
func FormFieldFor(errs FieldErrors, name string, cfg FormFieldConfig) render.HTML {
	if errs != nil {
		if msg, ok := errs[name]; ok {
			cfg.Error = msg
		}
	}
	return FormField(cfg)
}

// ─── ValidationSummary ───────────────────────────────────────────────
//
// The summary that goes above a form: role="alert", focusable by
// script (tabindex="-1", never a tab stop), one link per field error
// so the value of the summary is getting to the field. Rendered
// through headless.ValidationSummary dressed with this package's class
// map.

// ValidationSummaryConfig configures a ValidationSummary.
type ValidationSummaryConfig struct {
	// ID names the summary's root. Required: the title's id is derived
	// from it, and two summaries on one page without ids would share
	// one title id — breaking both labels and both announcements.
	ID string
	// Errors maps field names to error messages. Required together
	// with General for anything to render.
	Errors FieldErrors
	// General is a sentence that belongs to no one field (a general
	// failure, a credentials mismatch). It renders as a text row,
	// after the field errors: text rather than a link to nothing.
	General string
	// FieldLabels maps field names to human-readable labels. The link
	// text is "<label>: <message>"; falls back to the field name.
	FieldLabels map[string]string
	// FieldIDs maps field names to actual control element IDs. When
	// the map is non-nil and a field is missing from it, the error
	// renders as text — a link to #email misses a control whose id is
	// f_email. When the map is nil, the field name itself is the
	// target (the typed fields' default: an un-set ID is the Name).
	FieldIDs map[string]string
	// FieldOrder controls the order of error rows. Entries that aren't
	// in Errors are silently skipped, so it's safe to pass the full
	// form field list. Without FieldOrder, rows fall back to
	// alphabetical-by-field-name so the rendered HTML is deterministic
	// across requests (Go map iteration is randomized).
	FieldOrder []string
	// Title overrides the default heading. Empty → "Please fix the
	// following errors:".
	Title string
	// Class adds extra CSS classes to the wrapper.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the summary's root. Keys the component
	// owns are dropped: class and id (use Class / ID), data-fui-*,
	// role, tabindex and aria-labelledby.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the i18n
	// title. When nil, English fallbacks apply.
	Ctx context.Context
}

// ValidationSummary renders an inline summary of form validation errors
// with anchor links to each field. Output ordering is deterministic:
// FieldOrder first if provided, then any leftover field names
// alphabetically, then the General row.
func ValidationSummary(cfg ValidationSummaryConfig) render.HTML {
	if len(cfg.Errors) == 0 && cfg.General == "" {
		return ""
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Deterministic order: FieldOrder first, then alphabetical leftover.
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
		// The target: the mapped control id when the map knows the
		// field, the field name when no map was given (the typed
		// fields' own default), and NO link when a map was given and
		// does not know the field — an anchor to nothing is worse
		// than text.
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
		titleText = i18nui.T(ctx, i18nui.KeyValidationSummaryTitle)
	}
	return validationSummaryStyle.WrapHTML(headless.ValidationSummary(headless.ValidationSummaryProps{
		Title:  titleText,
		Level:  2,
		Errors: errs,
		ID:     cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs,
			"role", "tabindex", "aria-labelledby"),
	}, withRootClass(validationSummaryClasses, cfg.Class)))
}

var validationSummaryStyle = registry.RegisterStyle("ui-validation-summary", validationSummaryCSS)

func validationSummaryCSS(_ style.Theme) string {
	return `.fui-validation-summary {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-inline-start: 4px solid var(--color-danger, #DC2626);
  border-radius: var(--fui-field-radius);
  background: color-mix(in oklab, var(--color-danger, #DC2626) 8%, var(--color-surface, #FFFFFF) 92%);
}
.fui-validation-summary:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
.fui-validation-summary__title {
  font-size: var(--text-sm, 0.875rem);
  font-weight: 700;
  margin: 0;
  color: var(--color-danger, #DC2626);
}
.fui-validation-summary__list {
  margin: 0;
  padding-left: var(--spacing-lg, 16px);
  list-style: disc;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
}
.fui-validation-summary__list a {
  display: inline-flex;
  align-items: center;
  min-block-size: 24px;
  color: var(--color-danger, #DC2626);
  text-decoration: underline;
}
.fui-validation-summary__list a:hover {
  color: var(--color-text, #18181B);
}`
}
