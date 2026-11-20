package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	layout := containerLayout("public")
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

type capPlugin struct{ b *Battery }

func (p capPlugin) Name() string { return "capplugin" }
func (p capPlugin) Init(*framework.App) error {
	return p.b.Register(Capability{Name: "plugcap", Version: 1, Methods: []Method{{
		Name:    "go",
		Handler: func(context.Context, json.RawMessage) (any, error) { return nil, nil },
	}}})
}

func TestManifestModeIncludesPluginCapabilities(t *testing.T) {
	t.Setenv(ManifestEnv, "1")
	site := appui.NewApp("ManifestPlugin")
	site.Register("/", manifestScreen{}, containerLayout("public"))
	b := New(Config{ID: "manifestplug.example.app", Shell: newFakeShell()})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "manifestplug"}))
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)
	app.RegisterPlugin(capPlugin{b})
	var out string
	captureExitFree(t, func() {
		out = captureStdout(t, func() {
			if err := b.Run(app); err != nil {
				t.Errorf("Run in manifest mode: %v", err)
			}
		})
	})
	if !strings.Contains(out, `"plugcap"`) {
		t.Fatalf("a capability a plugin registers from Init is missing from the manifest:\n%s", out)
	}
}

func TestInitTwiceIsRefused(t *testing.T) {
	site := appui.NewApp("Twice")
	site.Register("/", manifestScreen{}, containerLayout("public"))
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "twice"}))
	app.Mount(uihost.New(site))
	b := New(Config{ID: "twice.example.app", Shell: newFakeShell(), DataDir: t.TempDir()})
	if err := b.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := b.Init(app); err == nil {
		t.Fatal("a second Init was accepted")
	}
}

func TestRunRefusesAnUnregisteredBattery(t *testing.T) {
	shell := newFakeShell()
	b := New(Config{ID: "unregistered.example.app", Shell: shell})
	err := b.Run(framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "unregistered"})))
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Run on an App without the battery = %v, want the not-registered error", err)
	}
	if _, ran := shell.config(); ran {
		t.Fatal("Run reached the shell")
	}
}

func TestRunTurnsWorktreeIsolationOff(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "probe")
	site := appui.NewApp("Isolation")
	site.Register("/", manifestScreen{}, containerLayout("public"))
	shell := newFakeShell()
	b := New(Config{ID: "isolation.example.app", Shell: shell})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "isolation"}))
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)
	done := make(chan error, 1)
	go func() { done <- b.Run(app) }()
	waitFor(t, "the shell to run", func() bool { _, ok := shell.config(); return ok })
	got := os.Getenv("GOFASTR_ISOLATION")
	shell.Quit()
	<-done
	if got != "off" {
		t.Fatalf("GOFASTR_ISOLATION during Run = %q, want off unless KeepIsolation", got)
	}
}

type failingEvalWindow struct{ *fakeWindow }

func (failingEvalWindow) Eval(string) error { return fmt.Errorf("eval failed") }

func TestEmitKeepsGoingPastAFailedWindow(t *testing.T) {
	b, _ := newTestBattery(t)
	main := failingEvalWindow{&fakeWindow{id: MainWindowID}}
	w2 := &fakeWindow{id: "w2"}
	b.window = main
	b.windows[MainWindowID] = main
	b.windows["w2"] = w2
	b.winOrder = []string{"w2"}
	if err := b.Emit("ping", nil); err == nil {
		t.Fatal("Emit hid the failed window's error")
	}
	if len(w2.evaluated) != 1 {
		t.Fatalf("the window after a failed one got %d evals, want 1", len(w2.evaluated))
	}
}

func TestFocusBeforeAnyWindowIsDroppedQuietly(t *testing.T) {
	b, _ := newTestBattery(t)
	var buf bytes.Buffer
	b.logger = slog.New(slog.NewTextHandler(&buf, nil))
	b.emitWindowFocus(MainWindowID)
	b.emitAppearance(Appearance{ReduceTransparency: true})
	if buf.Len() != 0 {
		t.Fatalf("a focus report during startup was logged:\n%s", buf.String())
	}
}

func TestEmitAfterStopFails(t *testing.T) {
	b, _ := newTestBattery(t)
	w := &fakeWindow{id: MainWindowID}
	b.window = w
	b.windows[MainWindowID] = w
	if err := b.OnStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Emit("late", nil); err == nil {
		t.Fatal("Emit after OnStop reached the stopped window")
	}
}
