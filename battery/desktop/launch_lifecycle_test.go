package desktop

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The optional launch seam can own resources before the shell runs. These
// tests keep it independent of a platform transport and of OS registration.
type launchLifecycleShell struct {
	Shell
	prepared, released, ran atomic.Int32
	handled                 bool
	prepareErr, runErr      error
	startupURLs             []string
}

func (s *launchLifecycleShell) PrepareLaunch(_, _, _ string) (bool, []string, error) {
	s.prepared.Add(1)
	return s.handled, s.startupURLs, s.prepareErr
}
func (s *launchLifecycleShell) ReleaseLaunch() { s.released.Add(1) }
func (s *launchLifecycleShell) Run(context.Context, WindowConfig, func(Window)) error {
	s.ran.Add(1)
	return s.runErr
}

func launchLifecycleApp(t *testing.T, scheme string, shell Shell, mount bool) (*Battery, *framework.App) {
	t.Helper()
	previous := builtDeepLinkScheme
	builtDeepLinkScheme = scheme
	t.Cleanup(func() { builtDeepLinkScheme = previous })
	t.Setenv(ManifestEnv, "")
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	b := New(Config{ID: "launch.example.app", Title: "Launch", Shell: shell, KeepIsolation: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "launch-lifecycle"}))
	if mount {
		site := appui.NewApp("Launch")
		site.Register("/", e2eScreen{}, containerLayout("public"))
		app.Mount(uihost.New(site))
	}
	app.RegisterBattery(b)
	return b, app
}

func TestLaunchCleanupOnStartupFailure(t *testing.T) {
	shell := &launchLifecycleShell{Shell: NewUnsupportedShell()}
	// Missing UIHost fails app.Start's Init before any listener is opened.
	b, app := launchLifecycleApp(t, "notes", shell, false)
	err := b.Run(app)
	if err == nil || !strings.Contains(err.Error(), "no *uihost.UIHost") {
		t.Fatalf("Run error = %v, want missing UIHost startup failure", err)
	}
	if shell.prepared.Load() != 1 || shell.released.Load() != 1 || shell.ran.Load() != 0 {
		t.Fatalf("prepare/release/run = %d/%d/%d, want 1/1/0", shell.prepared.Load(), shell.released.Load(), shell.ran.Load())
	}
}

func TestLaunchCleanupAfterShellReturn(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{"normal", nil}, {"failure", errors.New("shell fixture failed")}} {
		t.Run(test.name, func(t *testing.T) {
			shell := &launchLifecycleShell{Shell: NewUnsupportedShell(), runErr: test.err}
			b, app := launchLifecycleApp(t, "notes", shell, true)
			err := b.Run(app)
			if !errors.Is(err, test.err) {
				t.Fatalf("Run = %v, want %v", err, test.err)
			}
			if shell.prepared.Load() != 1 || shell.released.Load() != 1 || shell.ran.Load() != 1 {
				t.Fatalf("prepare/release/run = %d/%d/%d, want 1/1/1", shell.prepared.Load(), shell.released.Load(), shell.ran.Load())
			}
		})
	}
}

func TestLaunchSecondaryOwnsItsCleanup(t *testing.T) {
	shell := &launchLifecycleShell{Shell: NewUnsupportedShell(), handled: true}
	b, app := launchLifecycleApp(t, "notes", shell, false)
	if err := b.Run(app); err != nil {
		t.Fatal(err)
	}
	if shell.prepared.Load() != 1 || shell.released.Load() != 0 || shell.ran.Load() != 0 || b.app != nil {
		t.Fatalf("secondary started app/shell or borrowed cleanup: prepare=%d release=%d run=%d app=%p", shell.prepared.Load(), shell.released.Load(), shell.ran.Load(), b.app)
	}
}

func TestLaunchPrepareFailureOwnsItsCleanup(t *testing.T) {
	want := errors.New("prepare fixture failed")
	shell := &launchLifecycleShell{Shell: NewUnsupportedShell(), prepareErr: want}
	b, app := launchLifecycleApp(t, "notes", shell, false)
	if err := b.Run(app); !errors.Is(err, want) {
		t.Fatalf("Run = %v, want %v", err, want)
	}
	if shell.released.Load() != 0 || shell.ran.Load() != 0 || b.app != nil {
		t.Fatal("failed preparation started app/shell or called primary cleanup")
	}
}

func TestLaunchStillRequiresEmbeddedScheme(t *testing.T) {
	shell := &launchLifecycleShell{Shell: NewUnsupportedShell()}
	b, app := launchLifecycleApp(t, "", shell, false)
	// A development-only deep-link config must not enable Windows runtime
	// protocol registration (or any new transport) as an accidental side effect.
	b.cfg.DeepLink = &DeepLinkConfig{Scheme: "notes"}
	if err := b.Run(app); err == nil || !strings.Contains(err.Error(), "no *uihost.UIHost") {
		t.Fatalf("Run = %v, want startup failure after skipping preparation", err)
	}
	if shell.prepared.Load() != 0 || shell.released.Load() != 0 {
		t.Fatal("unembedded development scheme enabled native launch preparation")
	}
}

func TestLaunchCleanupPrecedesColdLinkMapping(t *testing.T) {
	shell := &launchLifecycleShell{Shell: NewUnsupportedShell(), startupURLs: []string{"notes://a"}}
	b, app := launchLifecycleApp(t, "notes", shell, false)
	b.cfg.DeepLink.OnDeepLink = func(*url.URL) (string, bool) { panic("mapping fixture") }
	defer func() {
		if got := recover(); got != "mapping fixture" {
			t.Fatalf("panic = %v, want mapping fixture", got)
		}
		if shell.released.Load() != 1 || b.app != nil {
			t.Fatal("cold-link mapping panic leaked primary launch resources or started app")
		}
	}()
	_ = b.Run(app)
}
