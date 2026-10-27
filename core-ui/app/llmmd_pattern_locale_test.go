package app

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type aboutLinkComp struct{}

func (aboutLinkComp) Render() render.HTML { return `<a href="/about">about</a>` }

// injectLinkComp registered by value fails DI: the doc falls back.
type injectLinkComp struct {
	Svc fmt.Stringer `inject:""`
}

func (injectLinkComp) Render() render.HTML { return `<a href="/about">about</a>` }

// The pattern doc stands in for a page whose per-URL render failed and
// is served by ScreenLLMMDHandler: under /es/ both linked the English
// pages, while the per-URL doc and the HTML link the Spanish ones.
func TestPatternDocFollowsLocalePrefix(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	es := i18n.WithPathLocale(context.Background(), set, "es")

	a := NewApp("x")
	down := NewKey[string]("down")
	g := NewScreenGroup("/f", nil)
	g.Resolve(down.From(func(context.Context) (string, error) { return "", errors.New("down") }))
	g.Requires(down)
	g.Screen(NewScreen("/f", aboutLinkComp{}), nil)
	a.Router.ScreenGroup(g)
	res, ok := ScreenLLMMDForPath(es, a, "/f")
	if !ok || !strings.Contains(res.MD, "(/es/about)") {
		t.Errorf("fallback doc: ok=%v, want (/es/about):\n%s", ok, res.MD)
	}

	a.RegisterScreen(NewScreen("/di", injectLinkComp{}), nil)
	if res, ok := ScreenLLMMDForPath(es, a, "/di"); !ok || !strings.Contains(res.MD, "(/es/about)") {
		t.Errorf("DI fallback doc: ok=%v, want (/es/about):\n%s", ok, res.MD)
	}

	h := ScreenLLMMDHandler(a, NewScreen("/h", aboutLinkComp{}))
	for ctx, want := range map[context.Context]string{es: "(/es/about)", context.Background(): "(/about)"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/h/llm.md", nil).WithContext(ctx))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("handler lacks %s:\n%s", want, rec.Body.String())
		}
	}
}
