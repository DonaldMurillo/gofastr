package ui

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/html"
)

func TestFormRequiresAction(t *testing.T) {
	defer func() { recover() }()
	Form(FormConfig{})
	t.Fatal("expected panic without Action")
}

func TestFormRendersDefaultsAndSubmitButton(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(Form(FormConfig{Action: "/x"},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: in}),
	))
	for _, want := range []string{
		`<form`, `action="/x"`, `method="POST"`, `ui-form__fields`,
		`ui-form__actions`, `>Save<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestFormErrorsRenderSummaryCallout(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "email", Name: "e", ID: "e"})
	h := string(Form(FormConfig{
		Action: "/x",
		Errors: FieldErrors{"e": "Invalid email"},
	},
		FormFieldFor(FieldErrors{"e": "Invalid email"}, "e",
			FormFieldConfig{Label: "Email", For: "e", Input: in}),
	))
	for _, want := range []string{
		"ui-callout--danger", "Form has errors",
		`role="alert"`, `is-error`, "Invalid email",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestFormFieldForPullsErrorByName(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	errs := FieldErrors{"n": "Required"}
	h := string(FormFieldFor(errs, "n",
		FormFieldConfig{Label: "Name", For: "n", Input: in}))
	if !strings.Contains(h, "is-error") || !strings.Contains(h, "Required") {
		t.Errorf("expected error wired in: %s", h)
	}
}

func TestFormFieldForNoErrorWhenNotInMap(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	errs := FieldErrors{"other": "X"}
	h := string(FormFieldFor(errs, "n",
		FormFieldConfig{Label: "Name", For: "n", Input: in}))
	if strings.Contains(h, "is-error") {
		t.Errorf("expected no error class, got: %s", h)
	}
}

func TestFormCustomMethodAndSubmitLabel(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(Form(FormConfig{
		Action: "/x", Method: "GET", SubmitLabel: "Search",
	}, FormField(FormFieldConfig{Label: "n", For: "n", Input: in})))
	if !strings.Contains(h, `method="GET"`) {
		t.Errorf("expected method=GET, got: %s", h)
	}
	if !strings.Contains(h, ">Search<") {
		t.Errorf("expected custom submit label, got: %s", h)
	}
}
