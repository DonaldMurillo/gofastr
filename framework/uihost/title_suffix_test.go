package uihost

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type suffixTitleComponent struct{ Title string }

func (c *suffixTitleComponent) Render() render.HTML { return "Help center" }
func (c *suffixTitleComponent) ScreenTitle() string { return c.Title }

func TestHostTitleSuffix(t *testing.T) {
	for _, mode := range []string{"partial", "recovery"} {
		for _, title := range []string{"Help center", "Help center — Luna Help", "Luna Help — Other"} {
			t.Run(mode+"/"+title, func(t *testing.T) {
				a := app.NewApp("Luna Help")
				c := &suffixTitleComponent{Title: title}
				a.Register("/help", c, nil)
				host := New(a)
				req := httptest.NewRequest("GET", "/help", nil)
				rec := httptest.NewRecorder()
				want := title
				if title != "Help center — Luna Help" {
					want += " — Luna Help"
				}
				if mode == "partial" {
					req.Header.Set("X-Gofastr-Navigate", "1")
					host.ServeHTTP(rec, req)
					got, err := url.PathUnescape(rec.Header().Get("X-Gofastr-Title"))
					if err != nil || got != want {
						t.Fatalf("partial title = %q (%v), want %q", got, err, want)
					}
				} else {
					host.RenderScreen(rec, req, c, ScreenResponse{})
					if !strings.Contains(rec.Body.String(), "<title>"+want+"</title>") {
						t.Fatalf("wrong recovery title, want %q", want)
					}
				}
			})
		}
	}
}
