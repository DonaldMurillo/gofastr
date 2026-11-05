package entityui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestRecordWireErrorsReachControls(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		field    schema.Field
		jsonCase crud.JSONCase
		wire     string
	}{
		{name: "explicit", field: schema.Field{Name: "author_name", WireName: "author", Type: schema.String, Min: fptr(2)}, jsonCase: crud.CaseCamel, wire: "author"},
		{name: "camel_digits", field: schema.Field{Name: "address_line_1", Type: schema.String, Min: fptr(2)}, jsonCase: crud.CaseCamel, wire: "addressLine1"},
		{name: "snake_digits", field: schema.Field{Name: "address_line_1", Type: schema.String, Min: fptr(2)}, jsonCase: crud.CaseSnake, wire: "address_line_1"},
		{name: "explicit_column", field: schema.Field{Name: "address_line_1", WireName: "address_line_1", Type: schema.String, Min: fptr(2)}, jsonCase: crud.CaseCamel, wire: "address_line_1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			url := recordWireErrorURL(t, scenario.field, scenario.jsonCase)
			ctx := chromedptest.Context(t)
			var state struct {
				Name    string `json:"name"`
				Invalid string `json:"invalid"`
				Message string `json:"message"`
			}
			if err := chromedp.Run(ctx, chromedp.Navigate(url),
				chromedp.Evaluate(fmt.Sprintf(`const input = document.getElementById(%q); input.value = 'x'; const form = input.form; form.noValidate = true; form.requestSubmit();`, "eui-f-"+scenario.field.Name), nil),
				chromedp.Poll(`!!window.__gofastr.loadedModules.formerrors`, nil),
				chromedp.Evaluate(fmt.Sprintf(`(() => { const input = document.getElementById(%q); return {name: input.name, invalid: input.getAttribute('aria-invalid') || '', message: input.closest('[data-hui-field]').querySelector('[data-hui-field-error]')?.textContent || ''}; })()`, "eui-f-"+scenario.field.Name), &state),
			); err != nil {
				t.Fatal(err)
			}
			if state.Name != scenario.wire || state.Invalid != "true" || state.Message == "" {
				t.Fatalf("wire-key refusal did not reach its named control: %+v; want name %q, aria-invalid and inline error", state, scenario.wire)
			}
		})
	}
}

func recordWireErrorURL(t *testing.T, field schema.Field, jsonCase crud.JSONCase) string {
	t.Helper()
	config := entity.EntityConfig{Table: "authors", Fields: []schema.Field{field}}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"authors": config}, nil, withAPI(map[string]string{"authors": "/api/authors"}))
	handler, err := fixture.host.Crud(fixture.host.reg.ents["authors"])
	if err != nil {
		t.Fatal(err)
	}
	handler.WithJSONCase(jsonCase)
	body := fixture.ui.Create("authors").Base("/authors").RenderCtx(fixture.userCtx("/authors/create", "", "user"))
	javascript, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/authors", func(w http.ResponseWriter, request *http.Request) {
		handler.Create().ServeHTTP(w, request.WithContext(asUser(request.Context(), "user")))
	})
	mux.HandleFunc("/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, javascript)
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, request *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/__gofastr/runtime/"), ".js")
		module, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, module)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><body>"+string(body)+`<script src="/runtime.js"></script>`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func TestRecordWireConditionalNames(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		wireName string
		jsonCase crud.JSONCase
		wantName string
	}{
		{name: "explicit", wireName: "active", jsonCase: crud.CaseCamel, wantName: "active"},
		{name: "camel_digits", jsonCase: crud.CaseCamel, wantName: "isEnabled1"},
		{name: "snake_digits", jsonCase: crud.CaseSnake, wantName: "is_enabled_1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			config := entity.EntityConfig{Table: "authors", Fields: []schema.Field{
				{Name: "is_enabled_1", WireName: scenario.wireName, Type: schema.Bool},
				{Name: "author_name", WireName: "author", Type: schema.String},
			}, Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{
				"author_name": {ShowWhen: "is_enabled_1 = true"},
			}}}.WithTimestamps(false)
			fixture := newTestUI(t, map[string]entity.EntityConfig{"authors": config}, nil, withAPI(map[string]string{"authors": "/api/authors"}))
			handler, err := fixture.host.Crud(fixture.host.reg.ents["authors"])
			if err != nil {
				t.Fatal(err)
			}
			handler.WithJSONCase(scenario.jsonCase)
			body := string(fixture.ui.Create("authors").Base("/authors").RenderCtx(fixture.userCtx("/authors/create", "", "user")))
			for _, attribute := range []string{`name="` + scenario.wantName + `"`, `name="author"`, `data-hui-when="` + scenario.wantName + `"`, `id="eui-f-is_enabled_1"`} {
				if !strings.Contains(body, attribute) {
					t.Errorf("wire-named conditional form is missing %s", attribute)
				}
			}
		})
	}
}
