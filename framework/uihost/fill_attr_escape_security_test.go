package uihost

import (
	"context"
	stdhtml "html"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type fillParamScreen struct{ slug string }

func (s *fillParamScreen) SetParams(m map[string]string) { s.slug = m["project"] }
func (s *fillParamScreen) Render() render.HTML           { return render.Text("P[" + s.slug + "]") }

var fillAttrRE = regexp.MustCompile(`<template data-cui-fill="([^"]*)">`)

// A {param} group's layer key embeds the resolved value, and the fills
// envelope writes that key into a data-cui-fill attribute. It was written
// with %q, Go string quoting: a `"` became `\"`, which HTML reads as a
// backslash and then the end of the attribute, so a URL could break out of
// the attribute and add its own. Attributes are HTML-escaped now.
func TestFillAttrHTMLEscapesParamKey(t *testing.T) {
	a := app.NewApp("t")
	top := app.NewOutlet("top")
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{top}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Place(top), l.Primary())
		})
	side := app.NewOutlet("side", app.OutletOptions{
		Deferred: true,
		Loading:  &app.Loading{Show: app.LoadingComponent(render.Text("LOADING"))},
	})
	project := app.NewLayout("project", app.LayoutSpec{Outlets: []*app.Outlet{side}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Place(side), l.Primary())
		})
	a.SetDefaultLayout(shell)
	g := app.NewScreenGroup("/projects/{project}", project)
	g.Screen(app.NewScreen("/projects/{project}", &fillParamScreen{}).
		Fill(side, app.NewStaticComponent("SIDE")).
		Fill(top, app.NewStaticComponent("TOP")), nil)
	g.Screen(app.NewScreen("/projects/{project}/issues/{n}", &fillParamScreen{}).
		Fill(side, app.NewStaticComponent("ISSUE-SIDE")).
		Fill(top, app.NewStaticComponent("ISSUE-TOP")), nil)
	a.Router.ScreenGroup(g)
	ds := New(a)

	req := httptest.NewRequest("GET", "/projects/a%22%20onmouseover%3D%22x", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	// Same project, so the group layer is kept and its key, with the
	// resolved value in it, addresses the swap and the side fill.
	req.Header.Set("X-Gofastr-From", `/projects/a" onmouseover="x/issues/1`)
	req.Header.Set("X-Gofastr-Fills", "2")
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	if strings.Contains(body, `\"`) {
		t.Fatalf("a route param broke out of a data-cui-fill attribute:\n%s", body)
	}
	attrs := fillAttrRE.FindAllStringSubmatch(body, -1)
	if len(attrs) == 0 {
		t.Fatalf("premise: no fill envelope in the partial:\n%s", body)
	}
	found := false
	for _, m := range attrs {
		if strings.Contains(stdhtml.UnescapeString(m[1]), `a" onmouseover="x`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no data-cui-fill attribute decodes to the resolved key; got %q", attrs)
	}

	// The part request (one deferred region on its own) writes the same
	// attribute from the address the client names.
	preq := httptest.NewRequest("GET", "/projects/a%22%20onmouseover%3D%22x", nil)
	preq.Header.Set("X-Gofastr-Navigate", "1")
	preq.Header.Set("X-Gofastr-Part", `g:/projects/a" onmouseover="x/:project#side`)
	for _, c := range w.Result().Cookies() {
		preq.AddCookie(c)
	}
	pw := httptest.NewRecorder()
	ds.ServeHTTP(pw, preq)
	part := pw.Body.String()
	if !strings.Contains(part, "SIDE") {
		t.Fatalf("premise: the part request did not render the side fill: %d %q", pw.Code, part)
	}
	if strings.Contains(part, `\"`) {
		t.Fatalf("a route param broke out of the part's data-cui-fill attribute:\n%s", part)
	}
}

// validExternalScriptSrc refuses schemes, hosts and traversal but not a
// `"`, so the rail's src attribute needs HTML escaping, not %q.
func TestScriptSrcHTMLEscapesQuote(t *testing.T) {
	ds := newTestUIHost()
	src := `/plug.js?a="b`
	if err := ds.RegisterExternalScript(src); err != nil {
		t.Fatalf("premise: RegisterExternalScript(%q) = %v", src, err)
	}
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	page := w.Body.String()
	if strings.Contains(page, `\"b`) {
		t.Fatalf("the script src was Go-quoted, which ends the attribute at the quote:\n%s", page)
	}
	if !strings.Contains(page, `<script src="/plug.js?a=&#34;b"></script>`) {
		t.Fatalf("no HTML-escaped script src in the page:\n%s", page)
	}
}
