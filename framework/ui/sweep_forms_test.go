package ui

import (
	"context"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func sweepFormsPanics(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected a panic", what)
		}
	}()
	fn()
}

func sweepFormsField() render.HTML {
	return FormField(FormFieldConfig{Label: "n", For: "n", Input: testControl("n")})
}

func TestSweepFormsDefaultSummarySentence(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x", ID: "f", Errors: FieldErrors{"n": "Required"}}, sweepFormsField()))
	want := i18nui.FormErrorsSummary.Text(context.Background())
	if want == "" || !strings.Contains(h, want) {
		t.Fatalf("summary should carry the default sentence %q: %s", want, h)
	}
}

func TestSweepFormsWiringKeysTakeTheRequestSeam(t *testing.T) {
	h := string(Form(FormConfig{Action: "/x", ExtraAttrs: html.Attrs{"Data-Cui-Rpc": "/save"}}, sweepFormsField()))
	if !strings.Contains(h, `data-cui-rpc="/save"`) {
		t.Fatalf("a mixed-case wiring key should reach the request seam: %s", h)
	}
	sweepFormsPanics(t, "data-fui-* outside the request vocabulary", func() {
		Form(FormConfig{Action: "/x", ExtraAttrs: html.Attrs{"data-fui-x": "1"}}, sweepFormsField())
	})
}

func TestSweepFormsOneKeyTwoSpellingsPanics(t *testing.T) {
	sweepFormsPanics(t, "wiring key twice", func() {
		Form(FormConfig{Action: "/x", ExtraAttrs: html.Attrs{"data-cui-rpc": "/a", "DATA-CUI-RPC": "/b"}}, sweepFormsField())
	})
	sweepFormsPanics(t, "plain key twice", func() {
		Form(FormConfig{Action: "/x", ExtraAttrs: html.Attrs{"title": "a", "TITLE": "b"}}, sweepFormsField())
	})
}

func TestSweepFormsCSRFStampPassesTheDefaultMiddleware(t *testing.T) {
	mw := middleware.CSRF(middleware.CSRFConfig{})
	var page string
	get := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page = string(Form(FormConfig{Action: "/x", Ctx: r.Context()}, sweepFormsField()))
	})).ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/", nil))
	m := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]+)">`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no hidden CSRF input in: %s", page)
	}
	body := url.Values{m[1]: {stdhtml.UnescapeString(m[2])}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range get.Result().Cookies() {
		req.AddCookie(c)
	}
	reached := false
	post := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true })).ServeHTTP(post, req)
	if !reached {
		t.Fatalf("the default CSRF middleware refused the token Form stamped (%d): %s", post.Code, post.Body)
	}
}

func TestSweepFormsTextAreaJSONSentenceIsTranslated(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.TextAreaInvalidJSON, "JSON invalide")
	h := string(TextArea(TextAreaConfig{Name: "doc", Label: "Doc", JSON: true, Ctx: ctx}))
	if !strings.Contains(h, "JSON invalide") {
		t.Fatalf("the JSON check should speak the request's locale: %s", h)
	}
}

func TestSweepFormsSelectLastFlaggedOptionWins(t *testing.T) {
	h := string(Select(SelectConfig{Name: "s", Label: "S", Options: []SelectOption{
		{Value: "a", Text: "A", Selected: true},
		{Value: "b", Text: "B", Selected: true},
	}}))
	selected := regexp.MustCompile(`<option[^>]*\bselected\b[^>]*>`).FindAllString(h, -1)
	if len(selected) != 1 || !strings.Contains(selected[0], `value="b"`) {
		t.Fatalf("the last flagged option should be the one selected, got %q in: %s", selected, h)
	}
}

func TestSweepFormsSelectedOverridesTheFlags(t *testing.T) {
	h := string(Select(SelectConfig{Name: "s", Label: "S", Selected: "a", Options: []SelectOption{
		{Value: "a", Text: "A"},
		{Value: "b", Text: "B", Selected: true},
	}}))
	selected := regexp.MustCompile(`<option[^>]*\bselected\b[^>]*>`).FindAllString(h, -1)
	if len(selected) != 1 || !strings.Contains(selected[0], `value="a"`) {
		t.Fatalf("a non-empty Selected should beat the options' flags, got %q in: %s", selected, h)
	}
}

func TestSweepFormsNumberInputStateClasses(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "q", Label: "Q", Disabled: true, Error: "Too many"}))
	root := regexp.MustCompile(`class="fui-number-input[^"]*"`).FindString(h)
	if !strings.Contains(root, "is-disabled") || !strings.Contains(root, "is-error") {
		t.Fatalf("the root should carry is-disabled and is-error, got %q in: %s", root, h)
	}
}

func TestSweepFormsAuthCardAlertInterrupts(t *testing.T) {
	h := string(AuthCard(AuthCardConfig{Title: "Sign in", Alert: render.Text("Wrong password")}))
	if !regexp.MustCompile(`<div class="fui-auth-card__alert" role="alert">`).MatchString(h) {
		t.Fatalf("the alert slot should be role=alert: %s", h)
	}
}

func TestSweepFormsRepeaterRPCSwapsItsItems(t *testing.T) {
	h := string(Repeater(RepeaterConfig{Name: "g", ID: "guests", RPCPath: "/r", Items: []render.HTML{render.Text("x")}}))
	if got := strings.Count(h, `data-cui-rpc-signal="guests-items"`); got != 2 {
		t.Fatalf("add and remove should both swap the guests-items region, found %d in: %s", got, h)
	}
}

func TestSweepFormsSwitchShipsRequiredAndNoFor(t *testing.T) {
	h := string(Switch(ToggleConfig{Name: "on", Label: "On", Required: true, ExtraAttrs: html.Attrs{"for": "elsewhere"}}))
	input := regexp.MustCompile(`<input[^>]*>`).FindString(h)
	if !strings.Contains(input, "required") {
		t.Errorf("a Required Switch should mark its input required: %s", input)
	}
	if strings.Contains(h, `for="elsewhere"`) {
		t.Errorf("a caller's for= should not ship: %s", h)
	}
}

func TestSweepFormsRadioIDSlugsTheValue(t *testing.T) {
	h := string(Radio(ToggleConfig{Name: "size", Value: "Extra Large", Label: "XL"}))
	if !strings.Contains(h, `id="size-extra-large"`) {
		t.Fatalf("a Radio's derived id should slug its value: %s", h)
	}
}

func TestSweepFormsChoiceErrorReplacesHelp(t *testing.T) {
	h := string(Checkbox(ToggleConfig{Name: "tos", Label: "Agree", Help: "HELPTEXT", Error: "You must agree"}))
	if strings.Contains(h, "HELPTEXT") {
		t.Fatalf("an Error should replace the Help: %s", h)
	}
}

func TestSweepFormsGroupErrorReplacesHelp(t *testing.T) {
	h := string(RadioGroup(RadioGroupConfig{Name: "plan", Legend: "Plan", Help: "HELPTEXT", Error: "Pick one",
		Options: []RadioGroupOption{{Value: "a", Label: "A"}}}))
	if strings.Contains(h, "HELPTEXT") || !strings.Contains(h, "Pick one") {
		t.Fatalf("a group's Error should replace its Help: %s", h)
	}
}

func TestSweepFormsFieldsOwnTheirAttributes(t *testing.T) {
	forged := html.Attrs{"name": "forged", "aria-describedby": "forged", "placeholder": "forged", "maxlength": "forged"}
	for name, h := range map[string]render.HTML{
		"TextField": TextField(TextFieldConfig{Name: "n", Label: "N", ExtraAttrs: forged}),
		"TextArea":  TextArea(TextAreaConfig{Name: "n", Label: "N", ExtraAttrs: forged}),
	} {
		if strings.Contains(string(h), "forged") {
			t.Errorf("%s let ExtraAttrs set an attribute it owns: %s", name, h)
		}
	}
}

func TestSweepFormsNumberFieldKeepsAZeroBound(t *testing.T) {
	zero := 0.0
	h := string(NumberField(NumberFieldConfig{Name: "n", Label: "N", Min: &zero}))
	if !strings.Contains(h, `min="0"`) {
		t.Fatalf("an explicit zero Min should render: %s", h)
	}
}

func TestSweepFormsChoiceErrorCarriesTheFormErrorsHook(t *testing.T) {
	h := string(Checkbox(ToggleConfig{Name: "tos", Label: "Agree", Error: "You must agree"}))
	p := regexp.MustCompile(`<p[^>]*fui-choice-field__error[^>]*>`).FindString(h)
	if !strings.Contains(p, "data-hui-field-error") {
		t.Fatalf("the choice's error paragraph should carry data-hui-field-error, got %q in: %s", p, h)
	}
}
