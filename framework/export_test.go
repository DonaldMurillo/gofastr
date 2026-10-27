package framework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestExportStaticUsesAppCSP: ExportStatic hands the builder the policy
// the app configured for its live responses.
func TestExportStaticUsesAppCSP(t *testing.T) {
	site := coreapp.NewApp("export-csp")
	site.Register("/", &exportHomeComp{}, nil)
	policy := "default-src 'self'; font-src https://fonts.gstatic.com"
	fwApp := NewApp(WithoutDefaultMiddleware(), WithSecurityHeaders(middleware.SecurityHeadersConfig{
		ContentSecurityPolicy: policy,
	})).Mount(uihost.New(site))
	dir := t.TempDir()
	if err := fwApp.ExportStatic(context.Background(), dir, ""); err != nil {
		t.Fatalf("ExportStatic: %v", err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `content="`+policy+`"`) {
		t.Errorf("the export ignored the app's Content-Security-Policy")
	}
	headers, err := os.ReadFile(filepath.Join(dir, "_headers"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(headers), policy) {
		t.Errorf("_headers ignored the app's Content-Security-Policy:\n%s", headers)
	}
}

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
	// The split runtime module that 404'd under the wget crawl must
	// now be on disk. The themeswitch module retired with the theme
	// family's move onto headless-navigation; the navigation module
	// that replaced it is the exported split module now.
	if _, err := os.Stat(filepath.Join(dir, "__gofastr", "runtime", "headless-navigation.js")); err != nil {
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

// TestStartExportsWhenAsked: with GOFASTR_EXPORT set, Start writes the
// export and returns instead of serving (`gofastr export` runs the app
// this way).
func TestStartExportsWhenAsked(t *testing.T) {
	site := coreapp.NewApp("export-env-test")
	site.Register("/", &exportHomeComp{}, nil)
	fwApp := NewApp(WithoutDefaultMiddleware()).Mount(uihost.New(site))
	dir := t.TempDir()
	t.Setenv("GOFASTR_EXPORT", dir)
	t.Setenv("GOFASTR_EXPORT_BASE", "/sub")

	done := make(chan error, 1)
	go func() { done <- fwApp.Start("127.0.0.1:0") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = fwApp.Shutdown(context.Background())
		t.Fatal("Start served instead of exporting")
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("index.html missing: %v", err)
	}
	if !strings.Contains(string(page), `"/sub/__gofastr/`) {
		t.Error("the export ignored GOFASTR_EXPORT_BASE")
	}
}
