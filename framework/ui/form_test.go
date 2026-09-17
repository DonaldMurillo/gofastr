package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

func TestFormRequiresAction(t *testing.T) {
	defer func() { recover() }()
	Form(FormConfig{})
	t.Fatal("expected panic without Action")
}

// An action the anchor policy refuses used to be substituted with "#",
// shipping a form whose submit went nowhere. The refusal is the
// contract now: a dangerous action is a programming error, said at
// render.
func TestFormRefusesAnUnsafeAction(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected a panic for a javascript: action")
		}
	}()
	Form(FormConfig{Action: "javascript:alert(1)"})
}

// A small builder the tests share: a field whose control carries the
// wiring the field hands it.
func testControl(name string) func(headless.FieldControl) render.HTML {
	return func(c headless.FieldControl) render.HTML {
		return Control(ControlConfig{Field: c, Type: "text", Name: name})
	}
}

func TestFormRendersDefaultsAndSubmitButton(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x"},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")}),
	))
	for _, want := range []string{
		`<form`, `action="/x"`, `method="POST"`, `fui-form__body`,
		`fui-form__actions`, `>Save<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestFormErrorsRenderTheSummary(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/x",
		ID:     "f",
		Errors: FieldErrors{"e": "Invalid email"},
	},
		FormFieldFor(FieldErrors{"e": "Invalid email"}, "e",
			FormFieldConfig{Label: "Email", For: "e", Input: testControl("e")}),
	))
	for _, want := range []string{
		// The form is marked so the behaviour module moves focus to
		// the summary after a failed submit.
		`data-hui-form-errors`,
		// The summary's id is derived from the form's.
		`id="f-errors"`,
		// Focusable by script, never a tab stop.
		`tabindex="-1"`, `role="alert"`,
		"Invalid email",
		// The per-field error reaches the field too.
		`aria-invalid="true"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

// A form rendering errors without an id has no way to derive a stable
// summary id; two such forms on one page would share a title id. The
// panic names the config field.
func TestFormErrorsRequireAnID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic for Errors without ID")
		}
		if !strings.Contains(string(r.(string)), "FormConfig.ID") {
			t.Fatalf("panic should name FormConfig.ID, got: %v", r)
		}
	}()
	Form(FormConfig{Action: "/x", Errors: FieldErrors{"e": "bad"}})
}

// Two errored forms on one page keep distinct summary ids — the whole
// reason the id is required.
func TestTwoErroredFormsKeepDistinctSummaryIDs(t *testing.T) {
	a := string(Form(FormConfig{Action: "/x", ID: "form-a", Errors: FieldErrors{"e": "a"}}))
	b := string(Form(FormConfig{Action: "/x", ID: "form-b", Errors: FieldErrors{"e": "b"}}))
	if !strings.Contains(a, `id="form-a-errors"`) || !strings.Contains(b, `id="form-b-errors"`) {
		t.Errorf("summary ids should be derived from each form's id:\n%s\n%s", a, b)
	}
	if strings.Contains(a, "form-b-errors") || strings.Contains(b, "form-a-errors") {
		t.Errorf("each form should carry only its own summary id")
	}
}

// The summary's general sentence (FormConfig.Summary) renders as a text
// row, after the field errors: it belongs to no field, so it links to
// nothing.
func TestFormSummaryGeneralRowRendersAsText(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/x", ID: "f",
		Errors:  FieldErrors{"e": "Invalid"},
		Summary: "Those credentials do not match.",
	}))
	if !strings.Contains(h, "Those credentials do not match.") {
		t.Errorf("the general summary sentence is missing:\n%s", h)
	}
	// The general row must not be a link: there is no field to link to.
	if strings.Contains(h, `href="#">Those credentials`) {
		t.Errorf("the general sentence rendered as an anchor:\n%s", h)
	}
}

func TestFormFieldForPullsErrorByName(t *testing.T) {
	errs := FieldErrors{"n": "Required"}
	h := string(FormFieldFor(errs, "n",
		FormFieldConfig{Label: "Name", For: "n", Input: testControl("n")}))
	if !strings.Contains(h, `aria-invalid="true"`) || !strings.Contains(h, "Required") {
		t.Errorf("expected error wired in: %s", h)
	}
}

func TestFormFieldForNoErrorWhenNotInMap(t *testing.T) {
	errs := FieldErrors{"other": "X"}
	h := string(FormFieldFor(errs, "n",
		FormFieldConfig{Label: "Name", For: "n", Input: testControl("n")}))
	if strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("expected no invalid state, got: %s", h)
	}
}

func TestValidationSummaryRendersErrors(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID: "sum",
		Errors: FieldErrors{
			"email": "Invalid email",
			"name":  "Required",
		},
	}))
	for _, want := range []string{
		"fui-validation-summary",
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

func TestValidationSummaryLinksMeetTouchTargetFloor(t *testing.T) {
	css := validationSummaryCSS(style.Theme{})
	for _, want := range []string{"display: inline-flex", "min-block-size: 24px"} {
		if !strings.Contains(css, want) {
			t.Errorf("ValidationSummary CSS missing %q", want)
		}
	}
}

func TestValidationSummaryWithFieldLabels(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID:          "sum",
		Errors:      FieldErrors{"email": "Invalid"},
		FieldLabels: map[string]string{"email": "Email Address"},
	}))
	if !strings.Contains(h, "Email Address: Invalid") {
		t.Errorf("expected label to be used: %s", h)
	}
}

func TestValidationSummaryEmptyErrors(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID: "sum", Errors: FieldErrors{},
	}))
	if h != "" {
		t.Errorf("empty errors should render nothing, got: %s", h)
	}
}

func TestValidationSummaryURLEncodesFieldName(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID: "sum", Errors: FieldErrors{"user[email]": "Invalid"},
	}))
	if !strings.Contains(h, `href="#user[email]"`) {
		t.Errorf("href should contain raw field name for anchor link, got: %s", h)
	}
	if strings.Contains(h, "user%5Bemail%5D") {
		t.Errorf("href should NOT be URL-encoded (it's a fragment, not a URL path), got: %s", h)
	}
}

// A-2: ValidationSummary must support FieldIDs map so anchor links
// point to actual input element IDs (which may differ from FieldErrors keys).
func TestValidationSummaryUsesFieldIDs(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID:       "sum",
		Errors:   FieldErrors{"val-name": "Name is required"},
		FieldIDs: map[string]string{"val-name": "f-name"},
	}))
	if !strings.Contains(h, `href="#f-name"`) {
		t.Errorf("expected href to use FieldID f-name, not the map key val-name:\n%s", h)
	}
}

// A-2: Without FieldIDs, fallback to the map key (backward compatible).
func TestValidationSummaryFallsBackToKeyWithoutFieldIDs(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID: "sum", Errors: FieldErrors{"email": "Invalid"},
	}))
	if !strings.Contains(h, `href="#email"`) {
		t.Errorf("expected href to fallback to key email:\n%s", h)
	}
}

// An error whose field has no known id renders as text, not as an
// anchor to nothing. The admin battery's shape: errors keyed by field
// name, controls carrying f_<name> ids — a miss in the map means the
// link would point nowhere.
func TestValidationSummaryUnknownFieldRendersAsText(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID:       "sum",
		Errors:   FieldErrors{"mystery": "No control has this name"},
		FieldIDs: map[string]string{"email": "f-email"},
	}))
	if !strings.Contains(h, "No control has this name") {
		t.Errorf("the message is missing:\n%s", h)
	}
	if strings.Contains(h, "<a") {
		t.Errorf("an unknown field must not render as a link:\n%s", h)
	}
}

func TestValidationSummarySafeWithSpecialChars(t *testing.T) {
	h := string(ValidationSummary(ValidationSummaryConfig{
		ID:     "sum",
		Errors: FieldErrors{"x": "<script>alert(1)</script> & \"quotes\""},
	}))
	if strings.Contains(h, "<script>") {
		t.Errorf("script not escaped: %s", h)
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("expected escaped script: %s", h)
	}
}

// D-1: Form Method must be GET or POST. Anything else silently produces
// invalid HTML that browsers treat as GET, potentially exposing sensitive data.
func TestFormPanicOnInvalidMethod(t *testing.T) {
	defer func() { recover() }()
	Form(FormConfig{Action: "/x", Method: "DELETE"})
	t.Fatal("expected panic on invalid method")
}

func TestFormCustomMethodAndSubmitLabel(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x", Method: "GET", SubmitLabel: "Go"},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	if !strings.Contains(h, `method="GET"`) || !strings.Contains(h, ">Go<") {
		t.Errorf("expected custom method and label: %s", h)
	}
}

// TestFormAutoStampsCSRFInput pins V3 #6: a POST form rendered with a
// Ctx that carries a CSRF token auto-embeds the hidden _csrf input as
// the first child, so callers don't have to remember it (and don't
// 403 the moment they forget).
func TestFormAutoStampsCSRFInput(t *testing.T) {
	ctx, token := ctxWithCSRFToken(t)
	h := string(Form(FormConfig{Action: "/x", Ctx: ctx},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	want := `<input type="hidden" name="_csrf" value="` + token + `">`
	if !strings.Contains(h, want) {
		t.Errorf("missing CSRF hidden input %q in: %s", want, h)
	}
	// The token input is the FIRST child of the body, before the fields.
	body := strings.Index(h, `fui-form__body`)
	csrf := strings.Index(h, want)
	if body == -1 || csrf < body {
		t.Errorf("CSRF input should be inside the body before the fields: %s", h)
	}
}

// TestFormCSRFOmittedWithoutCtx guards backward compat: forms rendered
// without Ctx (legacy callers) emit no hidden input, same as today.
func TestFormCSRFOmittedWithoutCtx(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x"},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	if strings.Contains(h, "_csrf") {
		t.Errorf("CSRF input without Ctx: %s", h)
	}
}

// TestFormCSRFOmittedOnGET pins that GET forms never get a CSRF input
// they aren't behind CSRF middleware (safe method), and a hidden
// input in a search form would surface the token in URLs.
func TestFormCSRFOmittedOnGET(t *testing.T) {
	ctx, _ := ctxWithCSRFToken(t)
	h := string(Form(FormConfig{Action: "/x", Method: "GET", Ctx: ctx},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	if strings.Contains(h, "_csrf") {
		t.Errorf("CSRF input on GET form: %s", h)
	}
}

// TestFormCSRFOmittedWhenNoTokenOnCtx is the safety net for routes
// that don't sit behind CSRF middleware: Form must not stamp an empty
// input that would break form decoding.
func TestFormCSRFOmittedWhenNoTokenOnCtx(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x", Ctx: context.Background()},
		FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	if strings.Contains(h, "_csrf") {
		t.Errorf("CSRF input with no token on ctx: %s", h)
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
	h := string(Form(FormConfig{
		Action:     "/x",
		HideSubmit: true,
	}, FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	if strings.Contains(h, "fui-form__actions") {
		t.Errorf("HideSubmit should omit submit button and actions div, got: %s", h)
	}
	if strings.Contains(h, "<button") {
		t.Errorf("HideSubmit should omit all buttons, got: %s", h)
	}
}

// ─── The request seam ─────────────────────────────────────────────
//
// Form's ExtraAttrs routes every data-fui-* and data-action-* key
// through the typed Request seam (the way Button's Action does), so
// the wiring survives headless's Safe instead of rendering a plain
// form that posts natively.

// The newsletter's shape: a POST whose 200 body lands in a signal.
func TestFormRequestNewsletterShape(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/__site/headless/subscribe",
		Method: "POST",
		ID:     "hl-subscribe",
		ExtraAttrs: html.MergeAttrs(html.Attrs{"novalidate": ""},
			interactive.Post("/__site/headless/subscribe").
				OnSuccess(interactive.SetSignal("hl-subscribe")).Attrs()),
	}, FormField(FormFieldConfig{Label: "Email", For: "e", Input: testControl("e")})))
	for _, want := range []string{
		`data-fui-rpc="/__site/headless/subscribe"`,
		`data-fui-rpc-method="POST"`,
		`data-fui-rpc-signal="hl-subscribe"`,
		// The native method and action stay for no script.
		`method="POST"`, `action="/__site/headless/subscribe"`,
		// novalidate is decoration and passes through.
		`novalidate`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

// The resource engine's shape: an edit form that natively POSTs but
// PUTs over RPC, then navigates.
func TestFormRequestResourcePUTShape(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/api/customers/42",
		Method: "POST",
		ExtraAttrs: interactive.Put("/api/customers/42").
			OnSuccess(interactive.Navigate("/app/customers/42")).Attrs(),
	}, FormField(FormFieldConfig{Label: "Name", For: "n", Input: testControl("n")})))
	if !strings.Contains(h, `data-fui-rpc-method="PUT"`) {
		t.Errorf("the RPC method must be independent of the native one:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-rpc-navigate="/app/customers/42"`) {
		t.Errorf("the navigate effect is missing:\n%s", h)
	}
	if !strings.Contains(h, `method="POST"`) {
		t.Errorf("the native method stays for no script:\n%s", h)
	}
}

// The generator's shape: a create form with reset and a relation
// mount. data-action-mount is refused by Safe like every data-action-*
// key — riding the seam is what keeps it rendered.
func TestFormRequestGeneratorShape(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/api/products",
		Method: "POST",
		ExtraAttrs: html.MergeAttrs(
			html.Attrs{
				"data-entity-form":  "products",
				"data-entity-mode":  "create",
				"data-action-mount": "productFormMount",
			},
			interactive.Post("/api/products").
				OnSuccess(interactive.ResetForm()).Attrs()),
	}, FormField(FormFieldConfig{Label: "Name", For: "n", Input: testControl("n")})))
	for _, want := range []string{
		`data-fui-rpc-reset`,
		`data-action-mount="productFormMount"`,
		// The entity markers are plain data attrs and survive.
		`data-entity-form="products"`, `data-entity-mode="create"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

// A key outside the vocabulary panics, naming the key and the seam.
func TestFormRequestRefusesUnknownKeys(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic for an unknown wiring key")
		}
		msg := r.(string)
		if !strings.Contains(msg, "data-fui-rpc-body") || !strings.Contains(msg, "Request") {
			t.Fatalf("the panic should name the key and the seam, got: %v", r)
		}
	}()
	Form(FormConfig{
		Action:     "/x",
		ExtraAttrs: html.Attrs{"data-fui-rpc-body": `{"a":1}`},
	}, FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")}))
}

// The widget-effect family and the live-search pair all ride the seam.
func TestFormRequestAdmitsCloseOpenRefreshTrigger(t *testing.T) {
	h := string(Form(FormConfig{
		Action: "/x",
		ExtraAttrs: html.MergeAttrs(
			interactive.Post("/x").
				OnSuccess(interactive.CloseWidget(), interactive.ResetForm(), interactive.OpenWidget("results")).Attrs(),
			html.Attrs{
				// The refresh pair has no typed constructor; the raw
				// keys ride the seam and take their checks there.
				"data-fui-rpc-refresh":     "panel",
				"data-fui-rpc-trigger":     "input",
				"data-fui-rpc-debounce-ms": "150",
			}),
	}, FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})))
	for _, want := range []string{
		"data-fui-rpc-close", "data-fui-rpc-reset", `data-fui-rpc-open="results"`,
		`data-fui-rpc-refresh="panel"`, `data-fui-rpc-trigger="input"`, `data-fui-rpc-debounce-ms="150"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}
