package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// WithExtraScripts writes the same script rail RegisterExternalScript
// guards, and the rail's emit site carries an allow marker that says
// every src passed validExternalScriptSrc. WithExtraScripts appended raw,
// so an off-origin src shipped on every page. It now goes through
// registerScript: a src the grammar refuses is dropped, a good one ships
// once, in <head>.
func TestWithExtraScriptsHoldsTheRailGrammar(t *testing.T) {
	bad := []string{
		"https://cdn.evil.example/x.js",
		"//evil.example/x.js",
		`/\evil.example/x.js`,
		"javascript:alert(1)",
		"relative.js",
		"/a/../x.js",
		"/x.js#frag",
	}
	for _, src := range bad {
		if err := New(app.NewApp("probe")).RegisterExternalScript(src); err == nil {
			t.Fatalf("setup: RegisterExternalScript accepted %q", src)
		}
	}
	a := app.NewApp("extra")
	a.Register("/", &testHomeComp{}, nil)
	ds := New(a, WithExtraScripts(append(bad, "/good.js?v=1", "/good.js?v=1")...))
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()
	for _, src := range bad {
		if strings.Contains(page, `src="`+src+`"`) || strings.Contains(page, "evil.example") {
			t.Errorf("SECURITY: [uihost] WithExtraScripts shipped %q, a src RegisterExternalScript refuses", src)
		}
	}
	if n := strings.Count(page, `src="/good.js?v=1"`); n != 1 {
		t.Errorf("good src emitted %d times, want once", n)
	}
	if i, j := strings.Index(page, `src="/good.js?v=1"`), strings.Index(page, "</head>"); i < 0 || i > j {
		t.Errorf("good src at %d, </head> at %d: the rail closes <head>", i, j)
	}
}
