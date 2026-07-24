package static

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// captureSlog swaps the default slog logger for a text handler writing to a
// buffer for the duration of the test, restoring stderr afterward. Tests in
// this package run sequentially (no t.Parallel), so the global swap is safe.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil))) })
	return &buf
}

// A dynamic route whose screen has no StaticPaths is still skipped, but the
// builder now warns (naming the route + the fix) instead of failing silently.
func TestBuildWarnsOnDynamicRouteWithoutStaticPaths(t *testing.T) {
	buf := captureSlog(t)

	a := coreapp.NewApp("SSGWarn")
	a.Register("/items/:id", &productScreenWithoutPaths{}, nil)
	host := uihost.New(a)

	res, err := (&Builder{Host: host, OutDir: t.TempDir()}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Pages) != 0 {
		t.Errorf("expected 0 pages (still skipped), got %v", res.Pages)
	}
	logged := buf.String()
	if !strings.Contains(logged, "/items/:id") {
		t.Errorf("warn must name the route pattern; log was:\n%s", logged)
	}
	if !strings.Contains(logged, "StaticPaths") {
		t.Errorf("warn must mention the StaticPaths fix; log was:\n%s", logged)
	}
}

// A dynamic route that DOES implement StaticPaths is exported and produces no
// skip warning.
func TestBuildNoWarnWhenStaticPathsProvided(t *testing.T) {
	buf := captureSlog(t)

	a := coreapp.NewApp("SSGOK")
	a.Register("/products/:slug", &productScreen{}, nil) // implements StaticPaths
	host := uihost.New(a)

	if _, err := (&Builder{Host: host, OutDir: t.TempDir()}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if strings.Contains(buf.String(), "/products/:slug") && strings.Contains(buf.String(), "StaticPaths") {
		t.Errorf("a StaticPaths-bearing route must not warn; log was:\n%s", buf.String())
	}
}

// A policy-gated dynamic screen (RenderAlt — e.g. a login prompt) builds,
// but its llm.md is the withheld doc: no title, no rendered content. A
// Block policy or a constraint-violating StaticPaths value fails the
// build loudly instead (see TestBuildFailsOnUnresolvableStaticPath).
func TestBuildGatedScreenExportsWithheldDoc(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &productScreen{}, nil)
	gate := coreapp.PolicyFunc(func(ctx context.Context) coreapp.Decision {
		return coreapp.Decision{Kind: coreapp.DecisionRenderAlt, AltFactory: func() component.Component {
			return &altLoginComp{}
		}}
	})
	scr := coreapp.NewScreen("/admin/{id:int}", &gatedStaticComp{}).WithPolicy(gate)
	scr.Title = "Project NIGHTFALL"
	a.RegisterScreen(scr, nil)

	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(out, "admin", "7", "llm.md"))
	if err != nil {
		t.Fatalf("read exported llm.md: %v", err)
	}
	for _, leak := range []string{"NIGHTFALL", "secret admin body"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("gated export leaked %q:\n%s", leak, body)
		}
	}
	if !strings.Contains(string(body), "withheld") {
		t.Errorf("expected withheld doc:\n%s", body)
	}
	page, err := os.ReadFile(filepath.Join(out, "admin", "7", "index.html"))
	if err != nil {
		t.Fatalf("read exported page: %v", err)
	}
	if strings.Contains(string(page), "secret admin body") {
		t.Errorf("gated page export leaked content:\n%s", page)
	}
}

// A StaticPaths value that violates the route's constraint fails the
// build loudly (the expanded path resolves to nothing) — never a silent
// skip, never an ungated fallback doc.
func TestBuildFailsOnUnresolvableStaticPath(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &productScreen{}, nil)
	a.Register("/admin/{id:int}", &badPathsComp{}, nil)
	_, err := (&Builder{Host: uihost.New(a), OutDir: t.TempDir()}).Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/admin/abc") {
		t.Errorf("expected loud failure naming the bad path, got: %v", err)
	}
}

type gatedStaticComp struct{ id string }

func (c *gatedStaticComp) SetParams(m map[string]string) { c.id = m["id"] }
func (c *gatedStaticComp) Render() render.HTML {
	return render.HTML("<p>secret admin body</p>")
}
func (c *gatedStaticComp) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"id": "7"}}
}

type altLoginComp struct{}

func (c *altLoginComp) Render() render.HTML { return render.HTML("<p>Please log in.</p>") }

type badPathsComp struct{ id string }

func (c *badPathsComp) SetParams(m map[string]string) { c.id = m["id"] }
func (c *badPathsComp) Render() render.HTML           { return render.Raw("x") }
func (c *badPathsComp) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"id": "abc"}}
}
