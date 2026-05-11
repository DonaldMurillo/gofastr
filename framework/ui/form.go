package ui

import (
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
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
	// Set to empty to omit the button (caller renders their own).
	SubmitLabel string

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

	cls := "ui-form"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	children := []render.HTML{}

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
	submitLabel := cfg.SubmitLabel
	if submitLabel == "" && cfg.SubmitLabel == "" {
		submitLabel = "Save"
	}
	if submitLabel != "" {
		children = append(children,
			render.Tag("div", map[string]string{"class": "ui-form__actions"},
				html.Button(html.ButtonConfig{
					Label: submitLabel,
					Type:  "submit",
					Class: "ui-button",
				}),
			))
	}

	return html.Form(html.FormConfig{
		Method: method,
		Action: cfg.Action,
		Class:  cls,
		ID:     cfg.ID,
	}, children...)
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
