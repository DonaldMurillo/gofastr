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

// injectLinkComp registered by value fails DI.
type injectLinkComp struct {
	Svc fmt.Stringer `inject:""`
}

func (injectLinkComp) Render() render.HTML { return `<a href="/about">about</a>` }

func TestConcreteMarkdownReportsFailure(t *testing.T) {
	a := NewApp("x")
	down := NewKey[string]("down")
	g := NewScreenGroup("/f", nil)
	g.Resolve(down.From(func(context.Context) (string, error) { return "", errors.New("down") }))
	g.Requires(down)
	g.Screen(NewScreen("/f", aboutLinkComp{}), nil)
	a.Router.ScreenGroup(g)
	a.RegisterScreen(NewScreen("/di", injectLinkComp{}), nil)

	for _, path := range []string{"/f", "/di"} {
		res, ok := ScreenLLMMDForPath(context.Background(), a, path)
		if !ok || res.Err == nil || res.Allowed || res.MD != "" {
			t.Errorf("%s: want a failure without a document, got %+v, resolved=%t", path, res, ok)
		}
	}
}

func TestPatternDocFollowsLocalePrefix(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	es := i18n.WithPathLocale(context.Background(), set, "es")
	a := NewApp("x")

	h := ScreenLLMMDHandler(a, NewScreen("/h", aboutLinkComp{}))
	for ctx, want := range map[context.Context]string{es: "(/es/about)", context.Background(): "(/about)"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/h/llm.md", nil).WithContext(ctx))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("handler lacks %s:\n%s", want, rec.Body.String())
		}
	}
}
