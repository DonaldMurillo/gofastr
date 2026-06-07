package app

// Verifies that layout chrome (sidebar/header/footer) receives the request
// context during a page render, so context-aware chrome (auth nav, current
// tenant, etc.) works. Before the fix, Layout.wrap called Component.Render()
// — dropping ctx — so a ContextComponent in a slot rendered with an empty
// background context (ContextOnly.Render returns "").

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type layoutCtxKey struct{}

// ctxChrome is a context-aware chrome component: it reads a value off ctx.
type ctxChrome struct {
	component.ContextOnly
	prefix string
}

func (c ctxChrome) RenderCtx(ctx context.Context) render.HTML {
	v, _ := ctx.Value(layoutCtxKey{}).(string)
	return render.Raw(c.prefix + ":" + v)
}

func TestLayoutChromeReceivesContext(t *testing.T) {
	a := NewApp("ctxchrome")
	layout := NewLayout("main").
		WithSidebar(ctxChrome{prefix: "SIDEBAR"}).
		WithHeader(ctxChrome{prefix: "HEADER"}).
		WithFooter(ctxChrome{prefix: "FOOTER"})
	a.RegisterScreen(NewScreen("/p", &stubComponent{html: render.Raw("BODY")}), layout)

	ctx := context.WithValue(context.Background(), layoutCtxKey{}, "ada")
	html, err := a.RenderPage(ctx, "/p")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	for _, want := range []string{"SIDEBAR:ada", "HEADER:ada", "FOOTER:ada"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("layout chrome did not receive request ctx (missing %q); got %q", want, html)
		}
	}
}
