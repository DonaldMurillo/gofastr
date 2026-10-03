package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// behaviorMarkerScreen renders the registered test behaviour's marker,
// so the export exercises the marker-bearing page path.
type behaviorMarkerScreen struct{}

func (behaviorMarkerScreen) ScreenTitle() string            { return "Behavior" }
func (behaviorMarkerScreen) ScreenDescription() string      { return "" }
func (behaviorMarkerScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (behaviorMarkerScreen) Render() render.HTML {
	return render.HTML(`<p data-static-probe>probe</p>`)
}

// The behaviour seam (docs/spec-behavior-registry.md) must survive the
// static export: a registered behaviour's module is dumped under
// /__gofastr/runtime/<name>.js (query-free, like every split module)
// and exported pages carry the inline #gofastr-behaviors block the
// kernel reads, because exports must be self-contained files.
func TestBuildEmitsRegisteredBehavior(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("static-probe-dep", `(function(){})();`, registry.Markers("[data-static-probe-dep]"))
	registry.RegisterBehavior("static-probe", `(function(){})();`, registry.Markers("[data-static-probe]"),
		registry.Requires("static-probe-dep"))

	a := coreapp.NewApp("SSGBehavior")
	a.Register("/", &behaviorMarkerScreen{}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The module file: present and non-empty, query-free path.
	data, err := os.ReadFile(filepath.Join(out, "__gofastr", "runtime", "static-probe.js"))
	if err != nil {
		t.Fatalf("registered behaviour module not dumped: %v", err)
	}
	if len(data) == 0 {
		t.Error("registered behaviour module is empty")
	}

	// The exported page carries the inline behaviours block the kernel
	// reads at boot (exports cannot rely on manifest.js).
	page, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("index.html missing: %v", err)
	}
	if !strings.Contains(string(page), `id="gofastr-behaviors"`) {
		t.Error("exported page missing the inline #gofastr-behaviors block")
	}
	if !strings.Contains(string(page), `"static-probe"`) {
		t.Error("exported behaviours block does not list the registered behaviour")
	}
	// A requirement rides the block as r, and the required module is
	// dumped like every other: an export is self-contained, so the
	// primitive must ship with the adapter that needs it even though
	// no marker of its own appears on any page.
	if !strings.Contains(string(page), `"s":["[data-static-probe]"],"r":["static-probe-dep"]`) {
		t.Error("exported behaviours block does not carry the requirement as r")
	}
	if _, err := os.Stat(filepath.Join(out, "__gofastr", "runtime", "static-probe-dep.js")); err != nil {
		t.Errorf("required behaviour module not dumped: %v", err)
	}
}
