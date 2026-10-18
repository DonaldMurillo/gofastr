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
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	ds := New(application) // no WithExtraScripts

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	if !strings.Contains(page, `<script defer src="/__livereload.js"></script>`) {
		t.Fatalf("livereload script not auto-injected:\n%s", page)
	}
}

func TestUIHostOmitsLiveReloadScriptByDefault(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")

	application := app.NewApp("Test")
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	ds := New(application)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	if strings.Contains(page, "/__livereload.js") {
		t.Fatalf("livereload script injected without GOFASTR_DEV:\n%s", page)
	}
}

func TestUIHostAddsLayoutLayerWhenDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LAYOUT", "")
	t.Setenv("GOFASTR_ENV", "")

	application := app.NewApp("Test")
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	rec := httptest.NewRecorder()
	New(application).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `<script defer src="/__devlayout.js"></script>`) {
		t.Fatalf("layout layer script not added:\n%s", rec.Body.String())
	}

	t.Setenv("GOFASTR_DEV_LAYOUT", "0")
	rec = httptest.NewRecorder()
	New(application).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(rec.Body.String(), "/__devlayout.js") {
		t.Fatalf("layout layer added despite GOFASTR_DEV_LAYOUT=0")
	}
}

// A host served on its own (no framework.App) links both dev scripts, so
// it must answer them too, not 404.
func TestStandaloneHostServesDevScripts(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")
	t.Setenv("GOFASTR_DEV_LAYOUT", "")
	t.Setenv("GOFASTR_ENV", "")

	application := app.NewApp("Test")
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home").WithDescription("h"), nil)

	ds := New(application)
	for _, url := range []string{"/__livereload.js", "/__devlayout.js"} {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
			t.Errorf("GET %s = %d %q, want the script", url, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
