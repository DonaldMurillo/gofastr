package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// linksScreen renders one link of every shape the export meets.
type linksScreen struct{}

func (linksScreen) ScreenTitle() string            { return "Links" }
func (linksScreen) ScreenDescription() string      { return "" }
func (linksScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (linksScreen) Render() render.HTML {
	return render.HTML(`<a href="/">root</a>` +
		`<a href="/about">page</a>` +
		`<a href="/about?tab=2#team">query</a>` +
		`<a href="/about/">slashed</a>` +
		`<a href="/products/alpha">expanded</a>` +
		`<a href="/products/gamma">not exported</a>` +
		`<a href="/files/report.pdf">file</a>` +
		`<a href="https://example.com/about">absolute</a>` +
		`<form action="/about" method="get"></form>` +
		`<pre>href=&quot;/about&quot;</pre>`)
}

// TestExportLinksPagesWithSlash: a page is written as <path>/index.html,
// which static hosts serve at <path>/ and answer <path> with a 301.
// Links to exported pages carry the slash so a click (client navigation
// included) skips that round trip; every other link is left as written.
func TestExportLinksPagesWithSlash(t *testing.T) {
	for _, base := range []string{"", "/gofastr"} {
		t.Run("base="+base, func(t *testing.T) {
			a := coreapp.NewApp("SSGTest")
			a.Register("/", &linksScreen{}, nil)
			a.Register("/about", &homeScreen{}, nil)
			a.Register("/products/:slug", &productScreen{}, nil)
			out := t.TempDir()
			if _, err := (&Builder{Host: uihost.New(a), OutDir: out, BasePath: base}).Build(context.Background()); err != nil {
				t.Fatalf("Build: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(out, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			page := string(raw)
			for _, want := range []string{
				`href="` + base + `/"`,
				`href="` + base + `/about/"`,
				`href="` + base + `/about/?tab=2#team"`,
				`href="` + base + `/products/alpha/"`,
				`action="` + base + `/about/"`,
				// Left as written: an already-slashed link, a route the
				// export did not write, a file, an absolute URL, code text.
				`href="` + base + `/products/gamma"`,
				`href="` + base + `/files/report.pdf"`,
				`href="https://example.com/about"`,
				`href=&quot;/about&quot;`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("exported page lacks %s", want)
				}
			}
			for _, bad := range []string{`/about"`, `/about//`, `/products/alpha"`} {
				if strings.Contains(strings.ReplaceAll(page, `&quot;/about&quot;`, ""), `href="`+base+bad) {
					t.Errorf("exported page still links %s", base+bad)
				}
			}
		})
	}
}
