package desktop

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns what
// fn printed. The pipe is closed and drained BEFORE the buffer is read,
// and the read goroutine exists because a pipe of a few KB can fill and
// deadlock the writer otherwise.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var buf strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, r)
	}()
	func() {
		defer func() { os.Stdout = old }()
		fn()
		w.Close()
	}()
	<-done
	r.Close()
	return buf.String()
}

// manifestScreen is a one-screen site so Init finds a UIHost.
type manifestScreen struct{ component.ContextOnly }

func (manifestScreen) RenderCtx(context.Context) render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("Manifest"))
}

// component.Component compile-time check for the manifest screen.
var _ component.Component = manifestScreen{}

func TestRunManifestModePrintsManifestWithoutWindow(t *testing.T) {
	t.Setenv(ManifestEnv, "1")

	site := appui.NewApp("ManifestApp")
	layout := appui.NewLayout("public").WithContainer()
	site.Register("/", manifestScreen{}, layout)
	host := uihost.New(site)

	shell := newFakeShell()
	b := New(Config{ID: "manifest.example.app", Title: "M", Shell: shell})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "manifestapp"}))
	app.Mount(host)
	app.RegisterBattery(b)

	var out string
	captureExitFree(t, func() {
		out = captureStdout(t, func() {
			if err := b.Run(app); err != nil {
				t.Errorf("Run in manifest mode: %v", err)
			}
		})
	})

	if !strings.Contains(out, `"schema": 1`) || !strings.Contains(out, `"clipboard"`) {
		t.Fatalf("manifest output missing expected content:\n%s", out)
	}
	if !strings.Contains(out, `"window"`) || !strings.Contains(out, `"fs"`) {
		t.Fatalf("manifest output missing core capabilities:\n%s", out)
	}
	// No shell, no listener: manifest mode must never open a window or
	// serve. The fake records nothing.
	if _, ran := shell.config(); ran {
		t.Fatal("manifest mode ran the shell")
	}
	// The registry froze, so a late Register is refused (the generator
	// sees exactly what was printed).
	if err := b.Register(Capability{Name: "late", Version: 1}); err == nil {
		t.Fatal("Register after manifest-mode freeze was accepted")
	}
}

// captureExitFree isolates a panic-driven failure inside fn from the
// harness restore below it.
func captureExitFree(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if v := recover(); v != nil {
			t.Fatalf("panic: %v", v)
		}
	}()
	fn()
}
