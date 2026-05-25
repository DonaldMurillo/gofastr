package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestUIHostAutoInjectsLiveReloadScriptWhenDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")
	t.Setenv("GOFASTR_ENV", "")

	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	ds := New(application) // no WithExtraScripts

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	if !strings.Contains(page, `<script src="/__livereload.js"></script>`) {
		t.Fatalf("livereload script not auto-injected:\n%s", page)
	}
}

func TestUIHostOmitsLiveReloadScriptByDefault(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")

	application := app.NewApp("Test")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	ds := New(application)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	if strings.Contains(page, "/__livereload.js") {
		t.Fatalf("livereload script injected without GOFASTR_DEV:\n%s", page)
	}
}
