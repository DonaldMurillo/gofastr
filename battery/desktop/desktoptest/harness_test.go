package desktoptest_test

import (
	"context"
	"net/http"
	"strings"
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

// The harness's own contract, proven on a two-screen app: the boot
// handshake yields a session the page can use and a stranger cannot,
// the native drivers reach the battery's menu dispatch, and the eval
// parsers read back what the battery evaluated.

type screen struct{ heading string }

func (s screen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(s.heading))
}

type testApp struct {
	app        *framework.App
	d          *desktop.Battery
	handlerRan chan struct{}
}

func newTestApp(t *testing.T, cfg desktop.Config) testApp {
	t.Helper()
	t.Setenv("GOFASTR_ISOLATION", "off")
	site := appui.NewApp("HarnessTest")
	layout := appui.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", screen{"Harness home"}, layout)
	site.Register("/two", screen{"Screen two"}, layout)
	site.Register("/settings", screen{"Settings screen"}, layout)

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "harnesstest"}))
	app.Mount(uihost.New(site))

	ran := make(chan struct{}, 1)
	if cfg.ID == "" {
		cfg.ID = "harness.test.app"
	}
	if cfg.Shell == nil {
		cfg.Shell = desktoptest.NewShell()
	}
	if cfg.Menu == nil {
		cfg.Menu = &desktop.Menu{Items: []desktop.MenuItem{
			{Title: "Go", Children: []desktop.MenuItem{
				{Title: "Two", Key: "cmd+2", Navigate: "/two"},
				{Title: "Ping", Handler: func(context.Context) error {
					select {
					case ran <- struct{}{}:
					default:
					}
					return nil
				}},
				{Title: "Prefs", Role: desktop.RoleSettings},
				{Role: desktop.RoleSeparator},
				{Role: desktop.RoleQuit},
			}},
		}}
	}
	d := desktop.New(cfg)
	app.RegisterBattery(d)
	return testApp{app: app, d: d, handlerRan: ran}
}

func TestRunBootsAndGatesThePage(t *testing.T) {
	ta := newTestApp(t, desktop.Config{Title: "Harness"})
	h := desktoptest.Run(t, ta.app, ta.d)

	h.Get("/").AssertStatus(t, http.StatusOK).AssertContains(t, "Harness home")
	if !strings.HasPrefix(h.URL("/x"), "http://127.0.0.1:") {
		t.Fatalf("URL = %q, want a loopback origin", h.URL("/x"))
	}
	if c := h.SessionCookie(); c.Name != "__gofastr_desktop" || c.Value == "" {
		t.Fatalf("session cookie = %+v", c)
	}

	// A stranger on the same port is refused.
	req, _ := http.NewRequest(http.MethodGet, h.URL("/"), nil)
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger GET / = %d, want 403", resp.StatusCode)
	}

	// The bridge answers with the session.
	var title struct {
		Title string `json:"title"`
	}
	h.Call("window", "title", nil).MustResult(t, &title)
	if title.Title != "Harness" {
		t.Fatalf("window.title = %q", title.Title)
	}
	if m := h.Manifest(); len(m.Capabilities) < 7 {
		t.Fatalf("manifest lists %d capabilities, want the seven core ones", len(m.Capabilities))
	}
	h.Call("nope", "nothing", nil).AssertCode(t, desktop.CodeNotFound)
}

func TestMenuDriversReachTheBattery(t *testing.T) {
	ta := newTestApp(t, desktop.Config{Settings: &desktop.WindowSpec{Path: "/settings"}})
	h := desktoptest.Run(t, ta.app, ta.d)

	h.ClickMenu("Go", "Two")
	h.Wait("the navigate eval", func() bool { return len(h.Navigations()) == 1 })
	if got := h.Navigations()[0]; got != "/two" {
		t.Fatalf("navigation = %q", got)
	}
	h.PressKey("CMD+2")
	h.Wait("the accelerator's eval", func() bool { return len(h.Navigations()) == 2 })

	h.ClickMenu("Go", "Ping")
	select {
	case <-ta.handlerRan:
	case <-time.After(5 * time.Second):
		t.Fatal("the Handler item never ran")
	}

	h.ClickMenu("Go", "Prefs")
	h.Wait("the settings window", func() bool { return len(h.WindowIDs()) == 2 })
	if ids := h.WindowIDs(); ids[1] != "settings" {
		t.Fatalf("windows = %v", ids)
	}
	h.CloseWindow("settings")
	if ids := h.WindowIDs(); len(ids) != 1 {
		t.Fatalf("windows after close = %v", ids)
	}
	if !h.Window("settings").Closed() {
		t.Fatal("the fake window was not closed")
	}

	// A title-less quit role is addressed by its role name; Run
	// returns nil.
	h.ClickMenu("Go", "quit")
	if err := h.Quit(); err != nil {
		t.Fatalf("Run returned %v after the quit role", err)
	}
}

func TestEventsAndNavigationsParse(t *testing.T) {
	ta := newTestApp(t, desktop.Config{})
	h := desktoptest.Run(t, ta.app, ta.d)

	if err := ta.d.Emit("saved", map[string]any{"path": "/tmp/x", "n": 2}); err != nil {
		t.Fatal(err)
	}
	ev := h.WaitEvent("saved")
	if string(ev.Payload) != `{"n":2,"path":"/tmp/x"}` {
		t.Fatalf("payload = %s", ev.Payload)
	}
	if err := ta.d.Emit("saved", nil); err != nil {
		t.Fatal(err)
	}
	if ev := h.WaitEvent("saved"); string(ev.Payload) != "null" {
		t.Fatalf("second payload = %s", ev.Payload)
	}
	if n := len(h.Events()); n != 2 {
		t.Fatalf("Events = %d, want 2", n)
	}

	// The parsers refuse near misses rather than guessing.
	for _, js := range []string{
		"",
		"window.__gofastr.desktop._dispatch(saved, JSON.parse(\"{}\"))",
		"window.__gofastr.desktop._dispatch(\"saved\", {})",
		"window.__gofastr.desktop._dispatch(\"saved\", JSON.parse(\"{\"))",
		"window.__gofastr.navigate(/two)",
	} {
		if _, ok := desktoptest.ParseEvent(js); ok {
			t.Errorf("ParseEvent accepted %q", js)
		}
		if _, ok := desktoptest.ParseNavigate(js); ok {
			t.Errorf("ParseNavigate accepted %q", js)
		}
	}
	if p, ok := desktoptest.ParseNavigate(`window.__gofastr.navigate("/a?b=1")`); !ok || p != "/a?b=1" {
		t.Fatalf("ParseNavigate = %q, %v", p, ok)
	}
}

func TestCloseMainWindowHonoursTray(t *testing.T) {
	ta := newTestApp(t, desktop.Config{Tray: &desktop.Tray{
		Title:            "T",
		CloseHidesWindow: true,
		Menu:             &desktop.Menu{Items: []desktop.MenuItem{{Title: "Show", Role: desktop.RoleShow}}},
	}})
	h := desktoptest.Run(t, ta.app, ta.d)

	h.CloseMainWindow()
	if !h.Window("main").Hidden() {
		t.Fatal("close did not hide the main window under CloseHidesWindow")
	}
	h.ClickTray("Show")
	if h.Window("main").Hidden() {
		t.Fatal("the show role did not bring the main window back")
	}
	if got := h.Shell.TrayTitle(); got != "T" {
		t.Fatalf("tray title after boot = %q", got)
	}
}

func TestCloseMainWindowQuitsWithoutTray(t *testing.T) {
	ta := newTestApp(t, desktop.Config{})
	h := desktoptest.Run(t, ta.app, ta.d)
	h.CloseMainWindow()
	if err := h.Quit(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
}
