package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The export wrote every widget's files at /core-ui/widget/<name>/ and
// prefixed only catalog URLs under that root (2026-10-27 U33): a widget
// mounted on a group, or with its own StylePath, was published at one
// URL and written at another.
func TestExportWritesWidgetsAtTheirURLs(t *testing.T) {
	a := coreapp.NewApp("SSGWidgetURLs")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)
	live := router.New()
	grouped := widget.New("ssg-url-grouped").Hidden().Slot("body", homeScreen{}).Build()
	widget.Mount(live.Group("/v2"), &grouped)
	styled := widget.New("ssg-url-styled").Hidden().Slot("body", homeScreen{}).Build()
	// "&" is a byte JSON escapes unless HTML escaping is off, as the
	// catalog's encoder has it.
	styled.StylePath = "/assets/ssg-url-r&d.css"
	widget.Mount(live, &styled)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: "/site"}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{
		"v2/core-ui/widget/ssg-url-grouped/chrome",
		"v2/core-ui/widget/ssg-url-grouped/style.css",
		"assets/ssg-url-r&d.css",
		"core-ui/widget/ssg-url-styled/chrome",
	} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Errorf("missing widget asset %s: %v", rel, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(out, "__gofastr", "widgets.json"))
	if err != nil {
		t.Fatal(err)
	}
	cat := string(b)
	for _, want := range []string{
		`"/site/v2/core-ui/widget/ssg-url-grouped/chrome"`,
		`"/site/v2/core-ui/widget/ssg-url-grouped/style.css"`,
		`"/site/assets/ssg-url-r&d.css"`,
		`"/site/core-ui/widget/ssg-url-styled/chrome"`,
	} {
		if !strings.Contains(cat, want) {
			t.Errorf("catalog lacks %s:\n%s", want, cat)
		}
	}
	if strings.Contains(cat, `"/core-ui/`) || strings.Contains(cat, `"/assets/`) || strings.Contains(cat, `"/v2/`) {
		t.Errorf("catalog carries a URL without the base path:\n%s", cat)
	}
}
