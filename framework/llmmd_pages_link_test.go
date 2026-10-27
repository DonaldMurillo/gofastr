package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The /api/llm.md index links the page index only when the app serves one.
// /llm-pages.md is mounted by a UI host built with WithPublicLLMMD; an
// API-only app, or a host without the opt-in, answers 404 there.
func TestLLMIndexPagesLinkMatchesRoutes(t *testing.T) {
	cases := []struct {
		name     string
		host     func() Mountable
		wantLink bool
	}{
		{name: "api only", host: func() Mountable { return nil }},
		{name: "host without page docs", host: func() Mountable {
			return uihost.New(pagesLinkCoreApp())
		}},
		{name: "host with page docs", wantLink: true, host: func() Mountable {
			return uihost.New(pagesLinkCoreApp(), uihost.WithPublicLLMMD())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []AppOption{WithPublicOpenAPI(), WithDB(bannerTestDB(t))}
			var fw *App
			if h := tc.host(); h != nil {
				fw = NewUIHostApp(h, opts...)
			} else {
				fw = NewApp(opts...)
			}
			fw.Entity("posts", entity.EntityConfig{
				Table:  "posts",
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
			}.WithTimestamps(false))
			a, cleanup := startApp(t, fw)
			defer cleanup()

			get := func(path string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				return rec
			}
			index := get("/api/llm.md")
			if index.Code != http.StatusOK {
				t.Fatalf("/api/llm.md = %d", index.Code)
			}
			linked := strings.Contains(index.Body.String(), "/llm-pages.md")
			if linked != tc.wantLink {
				t.Fatalf("index links /llm-pages.md = %v, want %v:\n%s", linked, tc.wantLink, index.Body.String())
			}
			if pages := get("/llm-pages.md").Code; linked && pages != http.StatusOK {
				t.Fatalf("index links /llm-pages.md but it answers %d", pages)
			}
		})
	}
}

func pagesLinkCoreApp() *app.App {
	core := app.NewApp("pageslink")
	core.RegisterScreen(app.NewScreen("/", &isAgenticScreen{}).WithTitle("Home"), nil)
	return core
}
