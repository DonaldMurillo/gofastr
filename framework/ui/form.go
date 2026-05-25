package ui

import (
	"context"
	"fmt"
	"html/template"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FieldErrors maps form field names to user-visible error messages.
// It is the same shape returned by [framework.ValidationRegistry.Validate]
// so server-side validation results round-trip directly into FormField.
//
// Example flow:
//
//	errors := registry.Validate(ctx, formData)  // framework.ValidationRegistry
//	page := ui.Form(ui.FormConfig{
//	    Action: "/customers",
//	    Errors: errors,
//	},
//	    ui.FormFieldFor(errors, "email", ...),
//	    ui.FormFieldFor(errors, "name",  ...),
//	)
type FieldErrors map[string]string

// FormConfig wraps a server-rendered <form>.
type FormConfig struct {
	// Action is the form's action URL. Required.
	Action string

	// Method is "POST" (default) or "GET".
	Method string

	// Errors is an optional set of field-level errors. They are
	// applied to FormFieldFor() calls so re-rendering after a failed
	// submit re-applies error styling automatically.
	Errors FieldErrors

	// Summary is an optional Callout displayed above the fields when
	// the form has errors. If empty and Errors is non-empty, a default
	// "Please fix the highlighted fields and try again." is rendered.
	Summary string

	// SubmitLabel is the visible submit button label. Defaults to "Save".
	SubmitLabel string

	// HideSubmit omits the submit button entirely when true.
	// Use when the caller renders its own submit button.
	HideSubmit bool

	// Ctx, when non-nil, lets Form auto-stamp the hidden CSRF input
	// (the framework's "_csrf" field) on unsafe-method submits. It
	// reads middleware.TokenFromContext(Ctx) — i.e. the token the CSRF
	// middleware stashes on every request — so callers do not have to
	// remember `render.HTML(csrfInput(ctx))` as the first child of
	// every form. Nil-safe: a form rendered without Ctx omits the
	// hidden input (matches pre-v3 behavior so existing tests and
	// non-CSRF flows like Method:"GET" stay correct).
	Ctx context.Context

	ID    string
	Class string
}

// Form renders a complete <form> with optional error summary above the
// fields and a submit button below.
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

	cls := "ui-form"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	children := []render.HTML{}

	// Auto-embed the hidden CSRF input on unsafe-method submits when a
	// request ctx is available. POST is the only unsafe method Form
	// accepts (panic above), so the check is simply "POST + ctx + token
	// on ctx". GET forms get nothing — they aren't behind CSRF.
	if method == "POST" && cfg.Ctx != nil {
		if tok := middleware.TokenFromContext(cfg.Ctx); tok != "" {
			children = append(children, csrfHiddenInput(tok))
		}
	}

	// Error summary callout.
	if len(cfg.Errors) > 0 {
		summary := cfg.Summary
		if summary == "" {
			summary = "Please fix the highlighted fields and try again."
		}
		children = append(children, Callout(
			CalloutConfig{Variant: StatusDanger, Title: "Form has errors"},
			render.Text(summary),
		))
	}

	// Fields go in a vertical stack.
	children = append(children,
		render.Tag("div", map[string]string{"class": "ui-form__fields"}, fields...))

	// Submit button.
	if !cfg.HideSubmit {
		submitLabel := cfg.SubmitLabel
		if submitLabel == "" {
			submitLabel = "Save"
		}
		children = append(children,
			render.Tag("div", map[string]string{"class": "ui-form__actions"},
				html.Button(html.ButtonConfig{
					Label: submitLabel,
					Type:  "submit",
					Class: "ui-button",
				}),
			))
	}

	return formStyle.WrapHTML(html.Form(html.FormConfig{
		Method: method,
		Action: cfg.Action,
		Class:  cls,
		ID:     cfg.ID,
	}, children...))
}

// csrfFormField is the hidden-input name Form emits when Ctx carries a
// CSRF token. It matches the framework's default (battery/auth.CSRFFormField
// + middleware.CSRFConfig.FormField when unset). Hosts that override the
// form field name in their CSRF config should NOT use Form's auto-embed
// — render their own hidden input via auth.CSRFInputFromCtx instead.
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
// Inline summary of all form validation errors rendered as a danger
// callout with links to each erroneous field. Pure SSR — no runtime JS.

// ValidationSummaryConfig configures a ValidationSummary.
type ValidationSummaryConfig struct {
	// Errors maps field names to error messages (required).
	Errors FieldErrors
	// FieldLabels maps field names to human-readable labels.
	// Falls back to the field name when not provided.
	FieldLabels map[string]string
	// FieldIDs maps field names to actual input element IDs.
	// When set, anchor links use these IDs so they point to the
	// correct input. Falls back to the field name when not provided.
	FieldIDs map[string]string
	// FieldOrder controls the order of error rows. Entries that aren't
	// in Errors are silently skipped, so it's safe to pass the full
	// form field list. Without FieldOrder, rows fall back to
	// alphabetical-by-field-name so the rendered HTML is deterministic
	// across requests (Go map iteration is randomized).
	FieldOrder []string
	// Title overrides the default banner heading. Empty → "Please fix
	// the following errors:".
	Title string
	// Class adds extra CSS classes to the wrapper.
	Class string
}

// ValidationSummary renders an inline summary of form validation errors
// as a danger callout with anchor links to each field. Output ordering
// is deterministic: FieldOrder first if provided, then any leftover
// field names alphabetically.
func ValidationSummary(cfg ValidationSummaryConfig) render.HTML {
	if len(cfg.Errors) == 0 {
		return ""
	}

	cls := "ui-validation-summary"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	titleText := cfg.Title
	if titleText == "" {
		titleText = "Please fix the following errors:"
	}
	title := html.Strong(
		html.TextConfig{Class: "ui-validation-summary__title"},
		render.Text(titleText),
	)

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

	items := make([]render.HTML, 0, len(ordered))
	for _, field := range ordered {
		msg := cfg.Errors[field]
		label := field
		if cfg.FieldLabels != nil {
			if l, ok := cfg.FieldLabels[field]; ok {
				label = l
			}
		}
		linkText := fmt.Sprintf("%s: %s", label, msg)
		hrefID := field
		if cfg.FieldIDs != nil {
			if id, ok := cfg.FieldIDs[field]; ok {
				hrefID = id
			}
		}
		items = append(items, render.Tag("li", nil,
			render.Tag("a", map[string]string{
				"href": "#" + hrefID,
			}, render.Text(linkText)),
		))
	}

	list := render.Tag("ul", map[string]string{
		"class": "ui-validation-summary__list",
	}, items...)

	return validationSummaryStyle.WrapHTML(render.Tag("div", map[string]string{
		"class":     cls,
		"role":      "alert",
		"aria-live": "assertive",
	}, title, list))
}

var validationSummaryStyle = registry.RegisterStyle("ui-validation-summary", validationSummaryCSS)

func validationSummaryCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-validation-summary"] {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-inline-start: 4px solid var(--color-danger, #DC2626);
  border-radius: var(--radii-md, 8px);
  background: color-mix(in oklab, var(--color-danger, #DC2626) 8%, var(--color-surface, #FFFFFF) 92%);
}
[data-fui-comp="ui-validation-summary"] .ui-validation-summary__title {
  font-size: 0.9rem;
  font-weight: 700;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-validation-summary"] .ui-validation-summary__list {
  margin: 0;
  padding-left: var(--spacing-lg, 16px);
  list-style: disc;
  font-size: 0.85rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-validation-summary"] .ui-validation-summary__list a {
  color: var(--color-danger, #DC2626);
  text-decoration: underline;
}
[data-fui-comp="ui-validation-summary"] .ui-validation-summary__list a:hover {
  color: var(--color-text, #18181B);
}`
}
