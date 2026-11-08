package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// descriptionContents parses page the way a browser does and returns the
// content of each <meta name="description"> it holds.
func descriptionContents(t *testing.T, page string) []string {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "meta" {
			name, content := "", ""
			for _, a := range n.Attr {
				switch a.Key {
				case "name":
					name = a.Val
				case "content":
					content = a.Val
				}
			}
			if strings.EqualFold(strings.TrimSpace(name), "description") {
				out = append(out, content)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// A page carries one description. WithDescription is the fallback for a
// page that names none of its own; a screen description, a screen's
// HeadHTML tag or a WithHeadHTML tag replaces it.
func TestOneDescriptionPerPage(t *testing.T) {
	cases := []struct {
		name string
		comp interface{ Render() render.HTML }
		opts []Option
		want string
	}{
		{"site only", &silentHomeComp{}, nil, "Global site description"},
		{"screen describer", &describedHomeComp{}, nil, "Welcome to the test site"},
		{"screen head html", &seoTestComp{headHTML: `<meta name="description" content="From HeadHTML">`}, nil, "From HeadHTML"},
		{"host head html", &silentHomeComp{}, []Option{WithHeadHTML(`<meta name="description" content="From WithHeadHTML">`)}, "From WithHeadHTML"},
		{"commented tag is no description", &seoTestComp{headHTML: `<!-- <meta name="description" content="example"> -->`}, nil, "Global site description"},
		{"typed over screen head html", &describedHeadComp{}, nil, "Welcome to the test site"},
		{"gt in a site tag", &silentHomeComp{}, []Option{WithHeadHTML(`<meta name="description" content="a > b">`)}, "a > b"},
		{"screen over gt in host head html", &describedHomeComp{}, []Option{WithHeadHTML(`<meta name="description" content="a > b"><meta name="author" content="kept">`)}, "Welcome to the test site"},
		{"character reference in the name", &seoTestComp{headHTML: `<meta name="descr&#105;ption" content="By reference">`}, nil, "By reference"},
		{"first of two in screen head html", &seoTestComp{headHTML: `<meta name="description" content="First"><meta name="description" content="Second">`}, nil, "First"},
		{"screen over host head html", &describedHomeComp{}, []Option{WithHeadHTML(`<meta name="description" content="From WithHeadHTML"><meta name="author" content="kept">`)}, "Welcome to the test site"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			application := app.NewApp("Test App")
			application.Register("/", tc.comp, nil)
			opts := append([]Option{WithDescription("Global site description")}, tc.opts...)
			rec := httptest.NewRecorder()
			New(application, opts...).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
			body := rec.Body.String()
			if got := descriptionContents(t, body); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("descriptions = %q, want just %q", got, tc.want)
			}
			if strings.Contains(tc.name, " over ") && !strings.Contains(body, `content="kept"`) {
				t.Errorf("the rest of the head HTML was dropped with its description: %s", body)
			}
		})
	}
}

// The site description rides the page metadata a client navigation
// swaps, so leaving a described page puts the site text back instead of
// keeping the last page's.
func TestPartialCarriesSiteDescription(t *testing.T) {
	application := app.NewApp("Test App")
	comp := &silentHomeComp{}
	application.Register("/", comp, nil)
	host := New(application, WithDescription("Global site description"))
	res := app.RenderResult{HTML: render.Text("Page"), Component: comp}
	rec := httptest.NewRecorder()
	host.writePartialResult(rec, httptest.NewRequest("GET", "/", nil), context.Background(), "/", res, nil, 0)
	body := rec.Body.String()
	if !strings.Contains(body, "data-cui-page-head=") || !strings.Contains(body, "Global site description") {
		t.Fatalf("partial page head lacks the site description: %s", body)
	}
}

// A description in WithHeadHTML is site-level like WithDescription: it
// rides the swapped page metadata, so a client navigation from a
// described page to one without a description restores it.
func TestPartialCarriesHostHeadDescription(t *testing.T) {
	application := app.NewApp("Test App")
	comp := &silentHomeComp{}
	application.Register("/", comp, nil)
	host := New(application, WithHeadHTML(`<meta name="description" content="From WithHeadHTML">`))
	res := app.RenderResult{HTML: render.Text("Page"), Component: comp}
	rec := httptest.NewRecorder()
	host.writePartialResult(rec, httptest.NewRequest("GET", "/", nil), context.Background(), "/", res, nil, 0)
	if body := rec.Body.String(); !strings.Contains(body, "From WithHeadHTML") {
		t.Fatalf("partial page head lacks the WithHeadHTML description: %s", body)
	}
}

// describedHeadComp names its description twice: through
// ScreenDescriber and in its HeadHTML. The typed one wins.
type describedHeadComp struct{ describedHomeComp }

func (c *describedHeadComp) HeadHTML() string {
	return `<meta name="description" content="From HeadHTML"><meta name="author" content="kept">`
}

// Strict mode's site-description check reads the same site description
// the page does, so a description in WithHeadHTML satisfies it.
func TestStrictTakesHostHeadDescription(t *testing.T) {
	ds := New(app.NewApp("Test App"), WithHeadHTML(`<meta name="description" content="From WithHeadHTML">`))
	for _, f := range ds.strictSiteFindings() {
		if f.check == strictCheckSiteDescription {
			t.Fatalf("strict reports %q for a site whose WithHeadHTML carries its description", f.msg)
		}
	}
}
