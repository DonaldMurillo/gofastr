package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// pathLinks writes a link off the request's path, the way entityui.List
// derives its record links.
type pathLinks struct{ component.ContextOnly }

func (pathLinks) RenderCtx(ctx context.Context) render.HTML {
	r := coreapp.RequestFromContext(ctx)
	return render.HTML(`<a href="` + r.URL.Path + `/7">row 7</a>`)
}

// itemLinks is pathLinks on a dynamic route whose one ID holds a "?".
type itemLinks struct{ pathLinks }

func (itemLinks) SetParams(map[string]string) {}
func (itemLinks) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"id": "a?b"}}
}

// Exported markdown rendered with no page request, so a screen that
// reads the request panicked into an error placeholder where the live
// /customers/llm.md and the exported HTML both render its links. The
// request keeps a param's "?" in its path, as the live URL does.
func TestExportDocSeesPageRequest(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/customers", pathLinks{}, nil)
	a.Register("/items/:id", itemLinks{}, nil)
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if html := readOut(t, out, "customers/index.html"); !strings.Contains(html, "/customers/7") {
		t.Fatalf("exported HTML lacks the request-built link:\n%s", html)
	}
	for doc, want := range map[string]string{"customers/llm.md": "/customers/7", "items/a?b/llm.md": "/items/a?b/7"} {
		if md := readOut(t, out, doc); !strings.Contains(md, want) {
			t.Errorf("exported %s lacks the request-built link %s:\n%s", doc, want, md)
		}
	}
}
