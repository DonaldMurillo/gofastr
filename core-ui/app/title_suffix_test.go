package app_test

import (
	"context"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"strings"
	"testing"
)

type suffixTitleComp struct{ Title string }

func (c *suffixTitleComp) Render() render.HTML { return "Help center" }
func (c *suffixTitleComp) ScreenTitle() string { return c.Title }

func TestPageTitleSuffix(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, title := range []string{"Help center", "Help center — Luna Help", "Luna Help — Other"} {
			t.Run(title+map[bool]string{false: "/screen", true: "/outlet"}[fallback], func(t *testing.T) {
				a := app.NewApp("Luna Help")
				c := &suffixTitleComp{Title: title}
				a.Register("/help", c, nil)
				if fallback {
					shell, _ := nfShell()
					a.SetDefaultLayout(shell)
					a.WithNotFoundBody(c)
				}
				res, err := a.RenderPageResult(context.Background(), "/help")
				if err != nil {
					t.Fatal(err)
				}
				want := title
				if title != "Help center — Luna Help" {
					want += " — Luna Help"
				}
				if !strings.Contains(string(res.HTML), "<title>"+want+"</title>") {
					t.Fatalf("wrong page title, want %q", want)
				}
				if res.NotFoundOutlet != fallback {
					t.Fatal("wrong rendering path")
				}
			})
		}
	}
}
