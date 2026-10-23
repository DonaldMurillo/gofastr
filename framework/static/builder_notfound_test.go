package static

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
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

// nfSetStatusScreen draws a not-found body and sets 404 while it draws,
// the way entityui's Record does for a missing row.
type nfSetStatusScreen struct{ component.ContextOnly }

func (nfSetStatusScreen) RenderCtx(ctx context.Context) render.HTML {
	coreapp.SetStatus(ctx, http.StatusNotFound)
	return "<p>GONE</p>"
}

// nfDeclaredGone declares 410 through ScreenStatusCode.
type nfDeclaredGone struct{}

func (nfDeclaredGone) Render() render.HTML   { return "<p>GONE FOR GOOD</p>" }
func (nfDeclaredGone) ScreenStatusCode() int { return http.StatusGone }

// TestStaticBuildSkipsSetStatusPage: a page that answers 404 through
// app.SetStatus is skipped with a warning, like a 404-outlet route; a
// 200 file at its URL would tell every visitor the page is there.
func TestStaticBuildSkipsSetStatusPage(t *testing.T) {
	buf := captureSlog(t)

	a := coreapp.NewApp("SSGStatus")
	a.RegisterScreen(coreapp.NewScreen("/present", nfStaticScreen("PRESENT")), nil)
	// A pointer, as a real record screen is: the per-URL llm.md renders
	// the instance only for an injectable (pointer) component.
	a.RegisterScreen(coreapp.NewScreen("/gone", &nfSetStatusScreen{}), nil)
	// A value screen declaring its status: DI refuses it, so its doc is
	// the pattern doc, and the declaration must still keep it out.
	a.RegisterScreen(coreapp.NewScreen("/declared", nfDeclaredGone{}), nil)

	out := t.TempDir()
	res, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 1 || res.Pages[0] != "/present" {
		t.Fatalf("pages = %v, want only /present", res.Pages)
	}
	if logged := buf.String(); !strings.Contains(logged, "/gone") || !strings.Contains(logged, "404") {
		t.Errorf("the skip must warn naming the route and its status; log was:\n%s", logged)
	}
	if _, err := os.Stat(filepath.Join(out, "gone", "index.html")); !os.IsNotExist(err) {
		t.Errorf("no page may be baked in at a 404 page's URL")
	}
	// Nor its llm.md: the static host would serve the not-found body as
	// a 200 doc. The present page keeps its doc.
	if _, err := os.Stat(filepath.Join(out, "gone", "llm.md")); !os.IsNotExist(err) {
		md, _ := os.ReadFile(filepath.Join(out, "gone", "llm.md"))
		t.Errorf("no llm.md may be written for a 404 page; wrote:\n%s\nlog:\n%s", md, buf.String())
	}
	for _, f := range []string{"declared/index.html", "declared/llm.md"} {
		if _, err := os.Stat(filepath.Join(out, f)); !os.IsNotExist(err) {
			t.Errorf("%s written for a screen that declares 410", f)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "present", "llm.md")); err != nil {
		t.Errorf("the present page lost its llm.md: %v", err)
	}
}
