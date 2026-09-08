package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A layout's layer key can diverge from its name (#408): identity (what
// the runtime compares to pick the swap boundary) becomes independent of
// the name (the CSS/debug contract). The driving case: one shell shape
// per language must re-render per language, without suffixing the name
// and forcing the CSS into attribute-prefix selectors.

func TestLayoutKeyOverridesLayerIdentity(t *testing.T) {
	a := app.NewApp("t")
	a.Register("/en", &stubComp{html: "EN"}, app.NewLayout("docs").WithKey("docs-en"))
	a.Register("/es", &stubComp{html: "ES"}, app.NewLayout("docs").WithKey("docs-es"))

	for _, tc := range []struct{ path, wantKey string }{
		{"/en", `data-fui-layout-key="l:docs-en"`},
		{"/es", `data-fui-layout-key="l:docs-es"`},
	} {
		res, err := a.RenderPageResult(context.Background(), tc.path)
		if err != nil {
			t.Fatalf("RenderPageResult(%s): %v", tc.path, err)
		}
		s := string(res.HTML)
		if !strings.Contains(s, tc.wantKey) {
			t.Errorf("%s: key %q missing:\n%s", tc.path, tc.wantKey, s)
		}
		// The name is untouched: it still drives data-fui-layout and the
		// wrapper class, so the layout's CSS contract is stable across keys.
		if !strings.Contains(s, `data-fui-layout="docs"`) || !strings.Contains(s, `class="layout-docs"`) {
			t.Errorf("%s: name contract changed by WithKey:\n%s", tc.path, s)
		}
	}

	// The manifest carries the keyed chains, so the runtime sees two
	// different shells and swaps between them.
	keys := map[string][]string{}
	for _, e := range a.Routes() {
		keys[e.Path] = e.Layouts
	}
	if got := keys["/en"]; len(got) != 1 || got[0] != "l:docs-en" {
		t.Errorf("/en chain = %v, want [l:docs-en]", got)
	}
	if got := keys["/es"]; len(got) != 1 || got[0] != "l:docs-es" {
		t.Errorf("/es chain = %v, want [l:docs-es]", got)
	}
}

func TestGroupLayerKeyUsesDeclaredKey(t *testing.T) {
	g := app.NewScreenGroup("/es", app.NewLayout("docs").WithKey("docs-es"))
	g.Screen(app.NewScreen("x", &stubComp{html: "X"}), nil)
	a := app.NewApp("t")
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/es/x")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if !strings.Contains(s, `data-fui-layout-key="g:/es/:docs-es"`) {
		t.Errorf("group layer must embed the declared key:\n%s", s)
	}
	if !strings.Contains(s, `data-fui-layout="docs"`) {
		t.Errorf("group layer name contract must not change:\n%s", s)
	}
}
