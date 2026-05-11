package static

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	coreapp "github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/registry"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/uihost"
)

var ssgNameSeq atomic.Int64

// styledScreen renders a component wrapped by a registered Style. The
// home page references the marker, so SSG must emit the per-component
// stylesheet and the catalog.js file.
type styledScreen struct {
	style *registry.Style
}

func (s *styledScreen) ScreenTitle() string            { return "Styled" }
func (s *styledScreen) ScreenDescription() string      { return "" }
func (s *styledScreen) ScreenType() coreapp.ScreenType { return coreapp.ScreenPage }
func (s *styledScreen) Render() render.HTML {
	return s.style.WrapHTML(render.HTML(`<div class="x">hi</div>`))
}

func TestSSGEmitsCatalogAndPerComponentCSS(t *testing.T) {
	// Unique name per test run; registry is process-global.
	name := fmt.Sprintf("ssg-comp-%d", ssgNameSeq.Add(1))
	st := registry.RegisterStyle(name, func(theme style.Theme) string {
		return style.NewComponentSheet(name, theme).
			Rule(".x").Set("color", "red").End().
			MustBuild()
	})

	a := coreapp.NewApp("SSGTest")
	a.Register("/", &styledScreen{style: st}, nil)
	host := uihost.New(a)

	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// 1. /__gofastr/comp/<name>.css exists with scoped content.
	cssPath := filepath.Join(out, "__gofastr", "comp", name+".css")
	cssBody, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("comp/%s.css missing: %v", name, err)
	}
	wantSel := `[data-fui-comp="` + name + `"] .x`
	if !strings.Contains(string(cssBody), wantSel) {
		t.Errorf("comp CSS not scoped: %s", cssBody)
	}

	// 2. Rendered HTML embeds the catalog inline as JSON (no separate
	//    catalog.js file gets written anymore — the SSG output is
	//    fully self-contained per page).
	indexPath := filepath.Join(out, "index.html")
	idx, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(idx), `<script type="application/json" id="gofastr-catalog">`) {
		t.Errorf("index.html missing inline catalog JSON block: %s", idx)
	}
	if !strings.Contains(string(idx), `"`+name+`"`) {
		t.Errorf("inline catalog must include %q: %s", name, idx)
	}

	// 3. No separate catalog.js / routes.js file written.
	for _, gone := range []string{
		filepath.Join(out, "__gofastr", "catalog.js"),
		filepath.Join(out, "__gofastr", "routes.js"),
	} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("SSG should not emit %s — data is inlined in the page now", gone)
		}
	}

	// 4. Rendered HTML still references the per-component file directly
	//    (NOT the dynamic comp-bundle.css?names=… URL — static hosts
	//    don't serve query-paramed files).
	if strings.Contains(string(idx), "/__gofastr/comp-bundle.css") {
		t.Error("SSG output must not reference the bundle endpoint (query-paramed URLs don't serve from static hosts)")
	}
	if !strings.Contains(string(idx), "/__gofastr/comp/"+name+".css") {
		t.Errorf("index.html missing direct <link> to comp/%s.css: %s", name, idx)
	}
}
