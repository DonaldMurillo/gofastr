package entityui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func fptr(f float64) *float64 { return &f }

// contactEntities is a form fixture with one field per validator the
// schema carries.
func contactEntities() map[string]entity.EntityConfig {
	contacts := entity.EntityConfig{
		Table: "contacts",
		Fields: []schema.Field{
			{Name: "code", Type: schema.String, Required: true, Min: fptr(2), Max: fptr(10), Pattern: "[A-Z]{2}"},
			{Name: "bio", Type: schema.Text, Min: fptr(10.5), Max: fptr(500)},
			{Name: "email", Type: schema.String, Required: true, Max: fptr(120)},
			{Name: "site", Type: schema.String, Pattern: "(?i)^https"},
			{Name: "age", Type: schema.Int, Min: fptr(0), Max: fptr(130)},
		},
		Display: &entity.DisplayConfig{
			Singular: "Contact", Plural: "Contacts", TitleFields: []string{"code"},
			Fields: map[string]entity.FieldDisplay{"email": {Input: "email"}},
		},
	}
	return map[string]entity.EntityConfig{"contacts": contacts.WithTimestamps(false)}
}

// tagFor is the opening tag of the control named name.
func tagFor(t *testing.T, body, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<(?:input|textarea|select)\b[^>]*\bname="` + name + `"[^>]*>`).FindString(body)
	if m == "" {
		t.Fatalf("no control named %q:\n%s", name, body)
	}
	return m
}

// The record form's inputs carry what the schema checks, so the
// browser refuses a bad value before the round trip: required, the
// length bounds (a fractional minimum rounds up, as the server's rune
// count compares), the number bounds, the email kind's type, and the
// pattern wrapped to match anywhere, the way the server's unanchored
// regexp.MatchString does. A pattern in Go-only syntax stays off.
func TestRecordFormCarriesSchemaValidation(t *testing.T) {
	x := newTestUI(t, contactEntities(), nil, withAPI(map[string]string{"contacts": "/api/contacts"}))
	body := string(x.ui.Create("contacts").Base("/rec/contacts").RenderCtx(x.userCtx("/rec/contacts/create", "", "u1")))
	for name, wants := range map[string][]string{
		"code":  {`required`, `minlength="2"`, `maxlength="10"`, `pattern="[\s\S]*(?:[A-Z]{2})[\s\S]*"`},
		"bio":   {`minlength="11"`, `maxlength="500"`},
		"email": {`type="email"`, `required`, `maxlength="120"`},
		"age":   {`min="0"`, `max="130"`},
	} {
		tag := tagFor(t, body, name)
		for _, want := range wants {
			if !strings.Contains(tag, want) {
				t.Errorf("%s lacks %s: %s", name, want, tag)
			}
		}
	}
	if tag := tagFor(t, body, "site"); strings.Contains(tag, "pattern=") {
		t.Errorf("a Go-only pattern reached the browser: %s", tag)
	}
	// The email kind's label carries the required mark its siblings do.
	if label := regexp.MustCompile(`<label[^>]*\bfor="eui-f-email"[^>]*>`).FindString(body); !strings.Contains(label, "data-required") {
		t.Errorf("the email field's label lacks the required mark:\n%s", body)
	}
}

// The wrapped pattern agrees with the server: the browser accepts
// exactly the values regexp.MatchString accepts.
func TestPatternAttrMatchesLikeTheServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	const pattern = `[A-Z]{2}`
	attr := patternAttr(pattern)["pattern"]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><input id="f" pattern="` + attr + `">`))
	}))
	defer srv.Close()
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(pattern)
	for _, v := range []string{"AB", "xxABxx", "ab", "A", "a\nAB", ""} {
		var ok bool
		js := `(() => { const f = document.getElementById("f"); f.value = ` + jsString(v) + `; return f.validity.valid; })()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err != nil {
			t.Fatal(err)
		}
		// An empty value is the required attribute's to refuse.
		if want := v == "" || re.MatchString(v); ok != want {
			t.Errorf("%q: browser valid=%v, server match=%v", v, ok, want)
		}
	}
}

func jsString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}
