package app_test

// PROTOTYPE (spike/layout-resolve): unit + HTTP tests for parametric
// group layer keys — a group prefix carrying {param}s embeds the
// RESOLVED values in its layer key, so the layer is kept across one
// value's pages and re-rendered when the value changes.

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// paramScreen accepts route params (the router requires SetParams on
// dynamic routes) and renders its slug.
type paramScreen struct {
	slug string
}

func (s *paramScreen) SetParams(m map[string]string) { s.slug = m["project"] }
func (s *paramScreen) Render() render.HTML           { return render.Text("P[" + s.slug + "]") }

// paramKeyApp builds the canonical param-group app: a shell root and
// ONE group at /projects/{project} with its own layer.
func paramKeyApp(t *testing.T) *app.App {
	t.Helper()
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	project := app.NewLayout("project", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	a.SetDefaultLayout(shell)
	g := app.NewScreenGroup("/projects/{project}", project)
	g.Screen(app.NewScreen("/projects/{project}", &paramScreen{}), nil)
	g.Screen(app.NewScreen("/projects/{project}/issues/{n}", &paramScreen{}), nil)
	a.Router.ScreenGroup(g)
	return a
}

// TestParamGroupLayerKeyedByResolvedValue pins the server half: the
// rendered DOM carries the resolved key, the manifest keeps the
// template, and the shared depth is computed on resolved keys from
// both matches — the same project keeps the layer, another project
// re-renders it.
func TestParamGroupLayerKeyedByResolvedValue(t *testing.T) {
	a := paramKeyApp(t)

	// The rendered DOM carries the RESOLVED key.
	res, err := a.RenderPageResult(context.Background(), "/projects/billing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.HTML), `data-fui-layout-key="g:/projects/billing/:project"`) {
		t.Errorf("the group layer's key embeds the resolved value:\n%s", res.HTML)
	}

	// The route manifest keeps the TEMPLATE (the client substitutes
	// its matched path segments; the server stays the matcher).
	found := false
	for _, e := range a.Routes() {
		if e.Path == "/projects/:project" {
			found = true
			want := []string{"l:shell", "g:/projects/{project}/:project"}
			if strings.Join(e.Layouts, "|") != strings.Join(want, "|") {
				t.Errorf("manifest layouts = %v, want %v", e.Layouts, want)
			}
		}
	}
	if !found {
		t.Fatal("the param route is missing from the manifest")
	}

	// Same project, deeper page: the project layer is KEPT (the swap
	// boundary is the layer below it).
	same, err := a.RenderPartialFromResult(context.Background(), "/projects/billing/issues/42", "/projects/billing")
	if err != nil {
		t.Fatal(err)
	}
	if same.SwapLayer != "g:/projects/billing/:project" {
		t.Fatalf("same project keeps the layer: SwapLayer = %q, want g:/projects/billing/:project", same.SwapLayer)
	}

	// Another project: the layer re-renders (the boundary is the shell).
	cross, err := a.RenderPartialFromResult(context.Background(), "/projects/search", "/projects/billing")
	if err != nil {
		t.Fatal(err)
	}
	if cross.SwapLayer != "l:shell" {
		t.Fatalf("another project re-renders the layer: SwapLayer = %q, want l:shell", cross.SwapLayer)
	}
	if !strings.Contains(string(cross.HTML), `data-fui-layout-key="g:/projects/search/:project"`) {
		t.Errorf("the re-rendered layer carries the new value's key:\n%s", cross.HTML)
	}
}
