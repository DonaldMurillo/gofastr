package static

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A static export is public: anyone can fetch every file in it. A
// widget whose live /chrome endpoint 403s an anonymous caller
// (RequireSession, RequireAuthenticated) must not have its chrome
// written into the tree; the export dumped every widget's chrome.
func TestBuildSkipsGatedWidgetChrome(t *testing.T) {
	a := coreapp.NewApp("SSGGated")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)
	live := router.New()
	mount := func(name string, gate func(*widget.Definition)) {
		def := widget.New(name).Hidden().Slot("body", homeScreen{}).Build()
		gate(&def)
		widget.Mount(live, &def)
	}
	mount("ssg-gate-open", func(*widget.Definition) {})
	mount("ssg-gate-session", func(d *widget.Definition) { d.RequireSession = true })
	mount("ssg-gate-auth", func(d *widget.Definition) { d.RequireAuthenticated = true })
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(out, rel))
		return err == nil
	}
	if !exists("core-ui/widget/ssg-gate-open/chrome") {
		t.Error("an ungated widget's chrome is missing from the export")
	}
	for _, name := range []string{"ssg-gate-session", "ssg-gate-auth"} {
		if exists("core-ui/widget/" + name + "/chrome") {
			t.Errorf("%s: a gated widget's chrome shipped in the public export", name)
		}
		if !exists("core-ui/widget/" + name + "/style.css") {
			t.Errorf("%s: style.css is missing; the live sheet is ungated", name)
		}
	}
}

// A widget its ListedFor hook hides from an anonymous caller (battery/admin's
// drawer, palette and shortcut sheet, whose body slot renders the back-office
// nav) is left out of widgets.json; its chrome must stay out of the public
// export too, where only the session gate was checked.
func TestBuildSkipsUnlistedWidgetChrome(t *testing.T) {
	a := coreapp.NewApp("SSGUnlisted")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)
	live := router.New()
	def := widget.New("ssg-unlisted").Hidden().Slot("body", homeScreen{}).Build()
	def.ListedFor = func(*http.Request) bool { return false }
	widget.Mount(live, &def)
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "core-ui/widget/ssg-unlisted/chrome")); err == nil {
		t.Error("an unlisted widget's chrome shipped in the public export")
	}
	if _, err := os.Stat(filepath.Join(out, "core-ui/widget/ssg-unlisted/style.css")); err != nil {
		t.Errorf("style.css is missing; the live sheet is ungated: %v", err)
	}
}
