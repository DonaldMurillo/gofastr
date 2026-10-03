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

// nfStaticFill renders its label as an outlet fill.
type nfStaticFill string

func (f nfStaticFill) Render() render.HTML { return render.HTML("<span>" + string(f) + "</span>") }

// nfStaticScreen renders its label.
type nfStaticScreen string

func (s nfStaticScreen) Render() render.HTML { return render.HTML("<p>" + string(s) + "</p>") }

// TestStaticBuildSkipsNotFoundOutlet: a route whose render is the
// 404-outlet outcome (FallbackNotFound, nothing fills it) is skipped
// with a warning naming the route — the way a dynamic route without
// StaticPaths is skipped — while the rest of the export builds, and no
// 404 page is baked in at the route's URL. The live server keeps
// answering the route (with the real 404).
func TestStaticBuildSkipsNotFoundOutlet(t *testing.T) {
	buf := captureSlog(t)

	a := coreapp.NewApp("SSGNF")
	toc := coreapp.NewOutlet("toc", coreapp.OutletOptions{Fallback: coreapp.FallbackNotFound})
	aside := coreapp.NewOutlet("aside", coreapp.OutletOptions{Default: nfStaticFill("help")})
	shell := coreapp.NewLayout("shell", coreapp.LayoutSpec{
		Outlets: []*coreapp.Outlet{toc, aside},
	}, func(ctx context.Context, l *coreapp.LayoutTree) render.HTML {
		return render.Join(l.Place(toc), l.Primary(), l.Place(aside))
	})
	a.SetDefaultLayout(shell)
	a.NoLLMMD = true
	a.RegisterScreen(coreapp.NewScreen("/present", nfStaticScreen("PRESENT")).
		Fill(toc, nfStaticFill("TOC")), nil)
	a.RegisterScreen(coreapp.NewScreen("/missing", nfStaticScreen("MISSING")), nil)

	out := t.TempDir()
	res, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 1 || res.Pages[0] != "/present" {
		t.Fatalf("pages = %v, want only /present", res.Pages)
	}
	logged := buf.String()
	if !strings.Contains(logged, "/missing") || !strings.Contains(logged, "404") {
		t.Errorf("the skip must warn naming the route and the 404 outcome; log was:\n%s", logged)
	}
	if _, err := os.Stat(filepath.Join(out, "missing", "index.html")); !os.IsNotExist(err) {
		t.Errorf("no page may be baked in at the 404-outlet route's URL")
	}
	page, err := os.ReadFile(filepath.Join(out, "present", "index.html"))
	if err != nil || !strings.Contains(string(page), "PRESENT") {
		t.Errorf("the exportable route must still build (err=%v)", err)
	}
}
