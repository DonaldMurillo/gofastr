package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/middleware"
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

func TestValidationSummaryRendersErrors(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{
			"email": "Invalid email",
			"name":  "Required",
		},
	}))
	for _, want := range []string{
		"ui-validation-summary",
		`role="alert"`,
		"Please fix the following errors:",
		"email: Invalid email",
		"name: Required",
		`href="#email"`,
		`href="#name"`,
		"<ul",
		"<li",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestValidationSummaryWithFieldLabels(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"email": "Invalid"},
		FieldLabels: map[string]string{"email": "Email Address"},
	}))
	if !strings.Contains(h, "Email Address: Invalid") {
		t.Errorf("expected label to be used: %s", h)
	}
}

func TestValidationSummaryEmptyErrors(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{},
	}))
	if h != "" {
		t.Errorf("empty errors should render nothing, got: %s", h)
	}
}

func TestValidationSummaryURLEncodesFieldName(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"user[email]": "Invalid"},
	}))
	if !strings.Contains(h, `href="#user[email]"`) {
		t.Errorf("href should contain raw field name for anchor link, got: %s", h)
	}
	// The field name with brackets must be a valid href
	if strings.Contains(h, "user%5Bemail%5D") {
		t.Errorf("href should NOT be URL-encoded (it's a fragment, not a URL path), got: %s", h)
	}
}

// A-2: ValidationSummary must support FieldIDs map so anchor links
// point to actual input element IDs (which may differ from FieldErrors keys).
func TestValidationSummaryUsesFieldIDs(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"val-name": "Name is required"},
		FieldIDs: map[string]string{"val-name": "f-name"},
	}))
	if !strings.Contains(h, `href="#f-name"`) {
		t.Errorf("expected href to use FieldID f-name, not the map key val-name:\n%s", h)
	}
}

// A-2: Without FieldIDs, fallback to the map key (backward compatible).
func TestValidationSummaryFallsBackToKeyWithoutFieldIDs(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"email": "Invalid"},
	}))
	if !strings.Contains(h, `href="#email"`) {
		t.Errorf("expected href to fallback to key email:\n%s", h)
	}
}

func TestValidationSummarySafeWithSpecialChars(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"a\"b": "X"},
	}))
	// The href must not contain unescaped quotes in the attribute
	idx := strings.Index(h, `href=`)
	if idx == -1 {
		t.Fatal("missing href")
	}
	seg := h[idx:]
	endQ := strings.Index(seg[6:], `"`)
	if endQ == -1 {
		t.Fatal("unclosed href value")
	}
	hrefVal := seg[6 : 6+endQ]
	if strings.Contains(hrefVal, `"`) {
		t.Errorf("href value should not contain raw quotes, got href=%s", hrefVal)
	}
}

// D-1: Form Method must be GET or POST — anything else silently produces
// invalid HTML that browsers treat as GET, potentially exposing sensitive data.
func TestFormPanicOnInvalidMethod(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Form with invalid Method should panic")
		}
	}()
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	Form(FormConfig{Action: "/x", Method: "PAST"}, FormField(FormFieldConfig{Label: "n", For: "n", Input: in}))
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

// TestFormAutoStampsCSRFInput pins V3 #6: a POST form rendered with a
// Ctx that carries a CSRF token auto-embeds the hidden _csrf input as
// the first child, so callers don't have to remember it (and don't
// 403 the moment they forget).
func TestFormAutoStampsCSRFInput(t *testing.T) {
	ctx, token := ctxWithCSRFToken(t)
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(Form(FormConfig{Action: "/save", Ctx: ctx},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: in}),
	))
	want := `<input type="hidden" name="_csrf" value="` + token + `">`
	if !strings.Contains(h, want) {
		t.Fatalf("expected auto-embedded CSRF input %q in:\n%s", want, h)
	}
	// Hidden input must precede the form-fields container, otherwise
	// some servers see fields without the token (multipart parse order).
	if strings.Index(h, want) > strings.Index(h, `ui-form__fields`) {
		t.Errorf("CSRF input rendered AFTER fields container — should be before:\n%s", h)
	}
}

// TestFormCSRFOmittedWithoutCtx guards backward compat: forms rendered
// without Ctx (legacy callers) emit no hidden input — same as today.
func TestFormCSRFOmittedWithoutCtx(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(Form(FormConfig{Action: "/save"},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: in}),
	))
	if strings.Contains(h, `name="_csrf"`) {
		t.Errorf("nil Ctx should not stamp CSRF input, got: %s", h)
	}
}

// TestFormCSRFOmittedOnGET pins that GET forms never get a CSRF input
// — they aren't behind CSRF middleware (safe method), and a hidden
// input in a search form would surface the token in URLs.
func TestFormCSRFOmittedOnGET(t *testing.T) {
	ctx, _ := ctxWithCSRFToken(t)
	in := html.Input(html.InputConfig{Type: "text", Name: "q", ID: "q"})
	h := string(Form(FormConfig{Action: "/search", Method: "GET", Ctx: ctx},
		FormField(FormFieldConfig{Label: "q", For: "q", Input: in}),
	))
	if strings.Contains(h, `name="_csrf"`) {
		t.Errorf("GET form should not stamp CSRF input, got: %s", h)
	}
}

// TestFormCSRFOmittedWhenNoTokenOnCtx is the safety net for routes
// that don't sit behind CSRF middleware: Form must not stamp an empty
// input that would break form decoding.
func TestFormCSRFOmittedWhenNoTokenOnCtx(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	bareCtx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	h := string(Form(FormConfig{Action: "/save", Ctx: bareCtx},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: in}),
	))
	if strings.Contains(h, `name="_csrf"`) {
		t.Errorf("token-less ctx should not stamp CSRF input, got: %s", h)
	}
}

// ctxWithCSRFToken runs the CSRF middleware once and captures the
// resulting (ctx, token) pair for use in form tests. Using the real
// middleware (rather than seeding a fake key on ctx) keeps the test
// honest about the integration contract.
func ctxWithCSRFToken(t *testing.T) (ctx context.Context, token string) {
	t.Helper()
	var got = struct {
		ctx context.Context
		tok string
	}{}
	mw := middleware.CSRF(middleware.CSRFConfig{})
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.ctx = r.Context()
		got.tok = middleware.TokenFromContext(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got.tok == "" {
		t.Fatal("middleware did not produce a token")
	}
	return got.ctx, got.tok
}

func TestFormHideSubmitOmitsButton(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(Form(FormConfig{
		Action:     "/x",
		HideSubmit: true,
	}, FormField(FormFieldConfig{Label: "n", For: "n", Input: in})))
	if strings.Contains(h, "ui-form__actions") {
		t.Errorf("HideSubmit should omit submit button and actions div, got: %s", h)
	}
	if strings.Contains(h, "<button") {
		t.Errorf("HideSubmit should omit all buttons, got: %s", h)
	}
}
