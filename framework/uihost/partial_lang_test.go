package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type partialLangScreen struct {
	lang func() string
}

func (s partialLangScreen) Render() render.HTML { return "<p>Language test</p>" }
func (s partialLangScreen) ScreenLang() string  { return s.lang() }

func TestPartialScreenLanguageMatchesPage(t *testing.T) {
	for _, tc := range []struct {
		name, declared, lang, dir string
	}{
		{name: "trimmed", declared: " ar ", lang: "ar", dir: "rtl"},
		{name: "empty", declared: " ", lang: "en", dir: "ltr"},
		{name: "panic", lang: "en", dir: "ltr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			screen := partialLangScreen{lang: func() string {
				calls++
				if tc.name == "panic" {
					panic("test language is unavailable")
				}
				return tc.declared
			}}
			a := app.NewApp("language").WithLang("en")
			aside := app.NewOutlet("aside", app.OutletOptions{})
			a.SetDefaultLayout(app.NewLayout("lang", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
				func(_ context.Context, l *app.LayoutTree) render.HTML {
					return render.Join(l.Primary(), l.Place(aside))
				}))
			a.Register("/", &testHomeComp{}, nil)
			a.RegisterScreen(app.NewScreen("/language", screen).Fill(aside, parityText("aside")), nil)
			host := New(a)
			for _, mode := range []string{"full", "bare", "envelope"} {
				t.Run(mode, func(t *testing.T) {
					defer func() {
						if p := recover(); p != nil {
							t.Errorf("render let ScreenLang panic escape: %v", p)
						}
					}()
					before := calls
					req := httptest.NewRequest(http.MethodGet, "/language", nil)
					attr := ""
					if mode != "full" {
						req.Header.Set("X-Gofastr-Navigate", "1")
						attr = "data-cui-"
					}
					if mode == "envelope" {
						req.Header.Set("X-Gofastr-From", "/")
						req.Header.Set("X-Gofastr-Fills", "2")
					}
					rec := httptest.NewRecorder()
					host.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						t.Fatalf("status = %d, want 200", rec.Code)
					}
					if mode == "envelope" && rec.Header().Get("X-Gofastr-Envelope") != "2" {
						t.Fatal("fixture did not produce a fills envelope")
					}
					for _, want := range []string{attr + `lang="` + tc.lang + `"`, attr + `dir="` + tc.dir + `"`} {
						if !strings.Contains(rec.Body.String(), want) {
							t.Errorf("response does not carry %s", want)
						}
					}
					if reads := calls - before; reads != 1 {
						t.Errorf("ScreenLang called %d times, want 1", reads)
					}
				})
			}
		})
	}
}
