package static

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

var indexRowLink = regexp.MustCompile(`^\| \[[^\]]*\]\(([^)]*)\)`)

// goneScreen answers 404, so the export writes neither page nor doc.
type goneScreen struct{}

func (*goneScreen) ScreenStatusCode() int { return 404 }
func (*goneScreen) Render() render.HTML   { return "<p>gone</p>" }

// The exported page index links docs the export wrote: a StaticPaths
// route linked /products/:slug/llm.md, a file no export writes, and
// never its exported pages. The Spanish tree's index did the same.
func TestExportIndexLinksWrittenDocs(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	a.Register("/gone", &goneScreen{}, nil) // answers 404: no page, no doc
	host := uihost.New(a)
	host.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, index := range []string{"llm-pages.md", "es/llm-pages.md"} {
		md := readOut(t, out, index)
		pre := strings.TrimSuffix(strings.TrimSuffix(index, "llm-pages.md"), "/")
		if pre != "" {
			pre = "/" + pre
		}
		for _, want := range []string{pre + "/products/alpha/llm.md", pre + "/products/beta/llm.md"} {
			if !strings.Contains(md, "("+want+")") {
				t.Errorf("%s does not link %s:\n%s", index, want, md)
			}
		}
		for _, line := range strings.Split(md, "\n") {
			m := indexRowLink.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(m[1], "/")))); err != nil {
				t.Errorf("%s links %s, which the export did not write", index, m[1])
			}
		}
	}
}
