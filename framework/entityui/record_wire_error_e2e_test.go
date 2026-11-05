package entityui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestRecordWireErrorsReachControls(t *testing.T) {
	config := entity.EntityConfig{Table: "authors", Fields: []schema.Field{{Name: "author_name", WireName: "author", Type: schema.String, Required: true}}}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"authors": config}, nil, withAPI(map[string]string{"authors": "/api/authors"}))
	body := fixture.ui.Create("authors").Base("/authors").RenderCtx(fixture.userCtx("/authors/create", "", "user"))
	crud := fixture.host.cruds["authors"]
	if crud == nil {
		t.Fatal("record did not resolve its mounted CRUD handler")
	}
	javascript, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/authors", func(w http.ResponseWriter, request *http.Request) {
		crud.Create().ServeHTTP(w, request.WithContext(asUser(request.Context(), "user")))
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
	defer server.Close()
	ctx := chromedptest.Context(t)
	var invalid bool
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL),
		chromedp.Evaluate(`const form = document.querySelector('form'); form.noValidate = true; form.requestSubmit();`, nil),
		chromedp.Poll(`!!window.__gofastr.loadedModules.formerrors`, nil),
		chromedp.Evaluate(`document.getElementById('eui-f-author_name').getAttribute('aria-invalid') === 'true'`, &invalid),
	); err != nil {
		t.Fatal(err)
	}
	if !invalid {
		t.Fatal("explicit wire-key refusal did not reach the entity form control")
	}
}

func TestRecordWireConditionalNames(t *testing.T) {
	config := entity.EntityConfig{Table: "authors", Fields: []schema.Field{
		{Name: "is_enabled", WireName: "active", Type: schema.Bool},
		{Name: "author_name", WireName: "author", Type: schema.String},
	}, Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{
		"author_name": {ShowWhen: "is_enabled = true"},
	}}}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"authors": config}, nil, withAPI(map[string]string{"authors": "/api/authors"}))
	body := string(fixture.ui.Create("authors").Base("/authors").RenderCtx(fixture.userCtx("/authors/create", "", "user")))
	for _, attribute := range []string{`name="active"`, `name="author"`, `data-hui-when="active"`} {
		if !strings.Contains(body, attribute) {
			t.Errorf("wire-named conditional form is missing %s", attribute)
		}
	}
}
