package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type linkHome struct{}

func (linkHome) Render() render.HTML {
	return `<a href="/items/a%3Fb">q</a><a href="/items/caf%C3%A9">c</a>`
}

// A page whose key holds "?" was precached under its routing form,
// "/items/a?b/": the worker fetched /items/a with a query, the host
// answered 404 and the whole install failed. Its escaped links also
// missed the page set, so they kept the slashless form the host 301s.
func TestPWAPrecachesEscapedPagePath(t *testing.T) {
	a := coreapp.NewApp("Esc")
	a.Register("/", linkHome{}, nil)
	a.Register("/items/:id", &keyedScreen{paths: []map[string]string{{"id": "a?b"}, {"id": "café"}}}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sw := readOut(t, out, "service-worker.js")
	if !strings.Contains(sw, `"/items/a%3Fb/"`) || strings.Contains(sw, `"/items/a?b/"`) {
		t.Errorf("worker must precache the escaped page URL /items/a%%3Fb/:\n%s", sw)
	}
	if !strings.Contains(sw, `"/items/a%3Fb/llm.md"`) {
		t.Error("worker must precache the escaped doc URL /items/a%3Fb/llm.md")
	}
	home := readOut(t, out, "index.html")
	for _, want := range []string{`href="/items/a%3Fb/"`, `href="/items/caf%C3%A9/"`} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %s", want)
		}
	}
	readOut(t, out, "items/a?b/index.html")
}
