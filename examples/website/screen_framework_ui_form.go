package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

type FormDemoScreen struct{}

func (s *FormDemoScreen) ScreenTitle() string        { return "Form & validation" }
func (s *FormDemoScreen) ScreenDescription() string  { return "FormField + Errors round-trip pattern." }
func (s *FormDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *FormDemoScreen) Render() render.HTML {
	// "Clean" form — no errors yet.
	cleanForm := ui.Form(ui.FormConfig{Action: "/framework-ui/form"},
		ui.FormSection(ui.FormSectionConfig{Heading: "Profile",
			Description: "Public information shown on your account page.",
		},
			ui.FormField(ui.FormFieldConfig{
				Label: "Display name", For: "u-name", Required: true,
				Help:  "Shown next to your messages.",
				Input: html.Input(html.InputConfig{Type: "text", Name: "name", ID: "u-name"}),
			}),
			ui.FormField(ui.FormFieldConfig{
				Label: "Email", For: "u-email", Required: true,
				Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "u-email"}),
			}),
		),
	)

	// Failed-validation form — errors come from a (mocked) server.
	errs := ui.FieldErrors{
		"name":  "Display name must be at least 2 characters.",
		"email": "Please enter a valid email address.",
	}
	errorForm := ui.Form(ui.FormConfig{
		Action: "/framework-ui/form",
		Errors: errs,
	},
		ui.FormSection(ui.FormSectionConfig{Heading: "Profile"},
			ui.FormFieldFor(errs, "name", ui.FormFieldConfig{
				Label: "Display name", For: "e-name", Required: true,
				Input: html.Input(html.InputConfig{Type: "text", Name: "name", ID: "e-name"}),
			}),
			ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
				Label: "Email", For: "e-email", Required: true,
				Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "e-email"}),
			}),
		),
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui", Title: "Form & validation",
			Subtitle: "FormField shows error styling and an aria-live message when the parent Form passes a FieldErrors map. The shape matches framework.ValidationRegistry.Validate so server-side validation results round-trip without translation.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Pristine state",
			Description: "First render — no errors. Required fields show a red asterisk; help text sits below the input.",
		}, cleanForm),
		ui.Section(ui.SectionConfig{
			Heading:     "Validation failed",
			Description: "Re-render after a server validation. The Form callout summarises, FormField error rows replace help text, and is-error styles the input border.",
		}, errorForm),
		ui.Section(ui.SectionConfig{
			Heading: "End-to-end pattern",
			Description: "Server: errors := registry.Validate(ctx, formData). Render: ui.Form(ui.FormConfig{Errors: errors}, ui.FormFieldFor(errors, \"name\", …), …). One map both feeds the summary callout and selects per-field error rows.",
		}),
	)
}
