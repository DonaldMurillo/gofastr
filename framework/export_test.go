package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type exportHomeComp struct{}

func (exportHomeComp) Render() render.HTML { return render.HTML("<main>home</main>") }

func TestAppExportStatic_BuildsSiteAndModules(t *testing.T) {
	site := coreapp.NewApp("export-test")
	site.Register("/", &exportHomeComp{}, nil)
	host := uihost.New(site)
	fwApp := NewApp(WithoutDefaultMiddleware()).Mount(host)

	dir := t.TempDir()
	if err := fwApp.ExportStatic(context.Background(), dir, ""); err != nil {
		t.Fatalf("ExportStatic: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Errorf("index.html missing: %v", err)
	}
	// The split runtime module that 404'd under the wget crawl must now be on disk.
	if _, err := os.Stat(filepath.Join(dir, "__gofastr", "runtime", "themeswitch.js")); err != nil {
		t.Errorf("split runtime module missing: %v", err)
	}
}

func TestAppExportStatic_NoHostErrors(t *testing.T) {
	fwApp := NewApp(WithoutDefaultMiddleware())
	err := fwApp.ExportStatic(context.Background(), t.TempDir(), "")
	if err == nil {
		t.Error("expected error when no uihost.UIHost is mounted")
	}
}
