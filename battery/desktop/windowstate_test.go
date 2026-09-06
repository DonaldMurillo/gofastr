package desktop_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Window state that survives a relaunch (Config.RememberWindows),
// driven through the harness: the fake shell plays the user dragging a
// window (MoveWindow fires OnWindowFrame the way the native delegate
// does), a second battery on the SAME data dir plays the relaunch, and
// the enter redirect proves the remembered path.

// wsScreen is a one-line screen.
type wsScreen struct{ heading string }

func (s wsScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(s.heading))
}

// syncBuffer is a mutex-guarded slog destination.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
func wsApp(t *testing.T, cfg desktop.Config, dir string, logger *slog.Logger) (*framework.App, *desktop.Battery) {
	t.Helper()
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", dir)
	site := appui.NewApp("Window state")
	layout := appui.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", wsScreen{"Home"}, layout)
	site.Register("/two", wsScreen{"Two"}, layout)
	site.Register("/settings", wsScreen{"Settings"}, layout)

	opts := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: "windowstate"})}
	if logger != nil {
		opts = append(opts, framework.WithLogger(logger))
	}
	app := framework.NewApp(opts...)
	app.Mount(uihost.New(site))
	if cfg.ID == "" {
		cfg.ID = "windowstate.test"
	}
	if cfg.Shell == nil {
		cfg.Shell = desktoptest.NewShell()
	}
	d := desktop.New(cfg)
	app.RegisterBattery(d)
	return app, d
}

// windowsJSON reads the data dir's windows.json.
func windowsJSON(t *testing.T, dir, id string) map[string]struct {
	Frame *desktop.Frame `json:"frame"`
	Path  string         `json:"path"`
} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, id, "windows.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Windows map[string]struct {
			Frame *desktop.Frame `json:"frame"`
			Path  string         `json:"path"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("windows.json does not decode: %v", err)
	}
	return f.Windows
}

// TestRememberedFramesRestoreAcrossRuns is the headline behavior: the
// user moves the main window and the settings window, quits, relaunches
// (a second battery on the same data dir), and both windows reopen
// where they were left.
func TestRememberedFramesRestoreAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	settings := &desktop.WindowSpec{Path: "/settings", Title: "Settings", Width: 420, Height: 320}
	app, d := wsApp(t, desktop.Config{
		Title:           "WS",
		Settings:        settings,
		RememberWindows: true,
	}, dir, nil)
	h := desktoptest.Run(t, app, d)

	main := desktop.Frame{X: 140, Y: 90, Width: 800, Height: 600}
	settingsFrame := desktop.Frame{X: 40, Y: 300, Width: 500, Height: 400}
	h.MoveWindow("main", main)
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return h.Window("settings") != nil })
	h.MoveWindow("settings", settingsFrame)
	// The debounced write lands within windowWriteDelay plus slack.
	h.Wait("windows.json to hold both frames", func() bool {
		wins := windowsJSON(t, dir, "windowstate.test")
		return wins["main"].Frame != nil && *wins["main"].Frame == main &&
			wins["settings"].Frame != nil && *wins["settings"].Frame == settingsFrame
	})
	if err := h.Quit(); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// The relaunch: a fresh battery and shell over the same data dir.
	app2, d2 := wsApp(t, desktop.Config{
		Title:           "WS",
		Settings:        settings,
		RememberWindows: true,
	}, dir, nil)
	h2 := desktoptest.Run(t, app2, d2)
	cfg, ran := h2.Shell.Config()
	if !ran {
		t.Fatal("the second run never reached the shell")
	}
	if cfg.Frame == nil || *cfg.Frame != main {
		t.Fatalf("second run WindowConfig.Frame = %+v, want %+v", cfg.Frame, main)
	}
	h2.OpenSettings()
	calls := h2.Shell.OpenWindowCalls()
	if len(calls) != 1 || calls[0].ID != "settings" {
		t.Fatalf("second run OpenWindow calls = %+v", calls)
	}
	if calls[0].Spec.Frame == nil || *calls[0].Spec.Frame != settingsFrame {
		t.Fatalf("second run settings spec.Frame = %+v, want %+v", calls[0].Spec.Frame, settingsFrame)
	}
}

// TestRememberedPathBecomesBootRedirect: the main window's reported
// path is the enter redirect on the next launch; a secondary window's
// report is not stored; an invalid path is refused; and the quit flush
// carries a path whose debounce never elapsed (setPath returns only
// after the store recorded it).
func TestRememberedPathBecomesBootRedirect(t *testing.T) {
	dir := t.TempDir()
	app, d := wsApp(t, desktop.Config{
		Title:           "WS",
		Settings:        &desktop.WindowSpec{Path: "/settings", Title: "Settings", Width: 300, Height: 200},
		RememberWindows: true,
	}, dir, nil)
	h := desktoptest.Run(t, app, d)
	h.Get("/two").AssertStatus(t, http.StatusOK)
	if r := h.Call("window", "setPath", map[string]any{"path": "/two"}); r.OK != true {
		t.Fatalf("setPath /two: %s %s", r.Code, r.Message)
	}
	// Only the main window's report counts.
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return h.Window("settings") != nil })
	h.CallFrom("settings", "window", "setPath", map[string]any{"path": "/settings"}).AssertOK(t)
	// The navigate grammar is enforced on the way in.
	h.Call("window", "setPath", map[string]any{"path": "https://evil.example"}).AssertCode(t, desktop.CodeInvalidInput)
	h.Call("window", "setPath", map[string]any{"path": "//evilsite"}).AssertCode(t, desktop.CodeInvalidInput)

	// Quit immediately: the 500 ms debounce never fires, the flush on
	// quit must carry the path.
	if err := h.Quit(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	wins := windowsJSON(t, dir, "windowstate.test")
	if wins["main"].Path != "/two" {
		t.Fatalf("stored main path = %q, want /two (quit flush)", wins["main"].Path)
	}

	app2, d2 := wsApp(t, desktop.Config{Title: "WS", RememberWindows: true}, dir, nil)
	h2 := desktoptest.Run(t, app2, d2)
	if got := h2.BootRedirect(); got != "/two" {
		t.Fatalf("second run boot redirect = %q, want /two", got)
	}

	// A launch with no remembered path redirects to /.
	app3, d3 := wsApp(t, desktop.Config{Title: "WS", RememberWindows: true}, t.TempDir(), nil)
	h3 := desktoptest.Run(t, app3, d3)
	if got := h3.BootRedirect(); got != "/" {
		t.Fatalf("fresh boot redirect = %q, want /", got)
	}
}

// TestRememberWindowsOffWritesNothing: the flag off means no file, no
// restore, and no OnWindowFrame wiring.
func TestRememberWindowsOffWritesNothing(t *testing.T) {
	dir := t.TempDir()
	app, d := wsApp(t, desktop.Config{Title: "WS"}, dir, nil)
	h := desktoptest.Run(t, app, d)
	h.MoveWindow("main", desktop.Frame{X: 1, Y: 2, Width: 300, Height: 200})
	h.Call("window", "setPath", map[string]any{"path": "/two"}).AssertOK(t)
	time.Sleep(700 * time.Millisecond) // longer than the debounce
	if err := h.Quit(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "windowstate.test", "windows.json")); !os.IsNotExist(err) {
		t.Fatalf("windows.json exists with RememberWindows off (stat err = %v)", err)
	}
	cfg, _ := h.Shell.Config()
	if cfg.Frame != nil || cfg.OnWindowFrame != nil {
		t.Fatalf("WindowConfig carries frame state with the flag off: %+v", cfg.Frame)
	}
}

// TestWindowStateFileIsOwnerOnly: the store file is 0600.
func TestWindowStateFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	app, d := wsApp(t, desktop.Config{Title: "WS", RememberWindows: true}, dir, nil)
	h := desktoptest.Run(t, app, d)
	h.MoveWindow("main", desktop.Frame{X: 5, Y: 5, Width: 400, Height: 300})
	h.Wait("windows.json", func() bool { return windowsJSON(t, dir, "windowstate.test") != nil })
	if err := h.Quit(); err != nil {
		t.Fatalf("run: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, "windowstate.test", "windows.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("windows.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

// TestCorruptWindowStateFileIgnored: garbage in windows.json is a Warn
// and a clean default launch, not a crash and not a restore.
func TestCorruptWindowStateFileIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "windowstate.test"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "windowstate.test", "windows.json"), []byte(`{"windows": {"main": {"fr`), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	app, d := wsApp(t, desktop.Config{Title: "WS", RememberWindows: true}, dir, logger)
	h := desktoptest.Run(t, app, d)
	cfg, ran := h.Shell.Config()
	if !ran {
		t.Fatal("run never reached the shell")
	}
	if cfg.Frame != nil {
		t.Fatalf("a corrupt file restored a frame: %+v", cfg.Frame)
	}
	if got := h.BootRedirect(); got != "/" {
		t.Fatalf("boot redirect after a corrupt file = %q, want /", got)
	}
	if msg := buf.String(); !strings.Contains(msg, "window state") {
		t.Fatalf("no Warn about the corrupt file; logs were:\n%s", msg)
	}
}
