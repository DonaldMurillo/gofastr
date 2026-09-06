package desktop

import (
	"strings"
	"testing"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The windows and tray surfaces: OpenWindow path validation and
// dedupe-by-path, OpenSettings, the windows capability's edge codes,
// tray.setTitle's unsupported answer, and the shell wiring Run does
// (OnSettings opens the settings window; the tray config and the tray
// announce reach the shell).

// markReadyForWindows puts the battery in the state Run's ready
// callback leaves it in: a main window and a resolved loopback addr.
func markReadyForWindows(b *Battery, win *fakeWindow) {
	b.windowMu.Lock()
	defer b.windowMu.Unlock()
	b.window = win
	b.windows["main"] = win
	b.addr = "127.0.0.1:9"
}

func TestOpenWindowValidatesPath(t *testing.T) {
	b, _ := newTestBattery(t)
	for _, bad := range []string{"", "notes", "//x", "/a/../b", "http://x/y", "/a\n"} {
		_, err := b.OpenWindow(WindowSpec{Path: bad})
		de, ok := err.(*Error)
		if !ok || de.Code != CodeInvalidInput {
			t.Fatalf("OpenWindow(%q) err = %v, want invalid_input", bad, err)
		}
	}
}

func TestOpenWindowBeforeRunUnsupported(t *testing.T) {
	b, _ := newTestBattery(t)
	_, err := b.OpenWindow(WindowSpec{Path: "/settings"})
	de, ok := err.(*Error)
	if !ok || de.Code != CodeUnsupported {
		t.Fatalf("OpenWindow before Run err = %v, want unsupported", err)
	}
}

func TestOpenWindowDedupesByPathAndFocuses(t *testing.T) {
	b, shell := newTestBattery(t)
	markReadyForWindows(b, &fakeWindow{id: "main", title: "Test"})

	w1, err := b.OpenWindow(WindowSpec{Path: "/settings"})
	if err != nil {
		t.Fatal(err)
	}
	if w1.ID() != "w2" {
		t.Fatalf("first secondary id = %q, want w2", w1.ID())
	}
	if calls := shell.openWindowCallsList(); len(calls) != 1 || calls[0].id != "w2" || calls[0].url != "http://127.0.0.1:9/settings" {
		t.Fatalf("shell OpenWindow calls = %+v", calls)
	}

	// The same path focuses the live window; no second shell call.
	w2, err := b.OpenWindow(WindowSpec{Path: "/settings"})
	if err != nil {
		t.Fatal(err)
	}
	if w2 != w1 {
		t.Fatal("second open returned a different window")
	}
	if calls := shell.openWindowCallsList(); len(calls) != 1 {
		t.Fatalf("dedupe by path opened a second window: %+v", calls)
	}
	fw := w1.(*fakeWindow)
	if fw.focusCount() != 1 {
		t.Fatalf("focus calls = %d, want 1", fw.focusCount())
	}

	// A different path gets the next id.
	w3, err := b.OpenWindow(WindowSpec{Path: "/other"})
	if err != nil {
		t.Fatal(err)
	}
	if w3.ID() != "w3" {
		t.Fatalf("second secondary id = %q, want w3", w3.ID())
	}

	// Windows: main first, then secondaries in opening order.
	ws := b.Windows()
	if len(ws) != 3 || ws[0].ID() != "main" || ws[1].ID() != "w2" || ws[2].ID() != "w3" {
		var ids []string
		for _, w := range ws {
			ids = append(ids, w.ID())
		}
		t.Fatalf("Windows ids = %v, want [main w2 w3]", ids)
	}

	// A reported close drops the registration: the next open on that
	// path is a fresh window.
	b.handleWindowClosed("w2")
	ws = b.Windows()
	if len(ws) != 2 || ws[1].ID() != "w3" {
		t.Fatal("handleWindowClosed did not drop the window")
	}
	w4, err := b.OpenWindow(WindowSpec{Path: "/settings"})
	if err != nil {
		t.Fatal(err)
	}
	if w4.ID() != "w4" {
		t.Fatalf("re-open after close id = %q, want w4", w4.ID())
	}
}

func TestOpenSettings(t *testing.T) {
	b, shell := newTestBattery(t) // no Config.Settings
	if _, err := b.OpenSettings(); err == nil {
		t.Fatal("OpenSettings without Config.Settings accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("OpenSettings err = %v, want unsupported", err)
	}

	b2 := New(Config{
		ID:       "settings.example.app",
		Title:    "Settings App",
		Shell:    shell,
		Logger:   testLogger(t),
		Settings: &WindowSpec{Path: "/settings", Width: 520, Height: 460},
	})
	b2.grants = newMemGrantStore()
	markReadyForWindows(b2, &fakeWindow{id: "main"})
	w, err := b2.OpenSettings()
	if err != nil {
		t.Fatal(err)
	}
	if w.ID() != "settings" {
		t.Fatalf("settings window id = %q, want settings", w.ID())
	}
	calls := shell.openWindowCallsList()
	if len(calls) != 1 || calls[0].id != "settings" || !strings.HasSuffix(calls[0].url, "/settings") {
		t.Fatalf("shell calls = %+v", calls)
	}
	if calls[0].spec.Title != "Settings" {
		t.Fatalf("default settings title = %q, want Settings", calls[0].spec.Title)
	}
}

func TestWindowsCapabilityEdges(t *testing.T) {
	b, _ := newTestBattery(t)
	markReadyForWindows(b, &fakeWindow{id: "main", title: "Test"})

	// close refuses the main window.
	_, err := callMethod(t, b, "windows", "close", `{"id":"main"}`)
	if de, ok := err.(*Error); !ok || de.Code != CodeInvalidInput {
		t.Fatalf("close(main) err = %v, want invalid_input", err)
	}
	// Unknown ids are not_found.
	if _, err := callMethod(t, b, "windows", "focus", `{"id":"nope"}`); err == nil {
		t.Fatal("focus(unknown) accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeNotFound {
		t.Fatalf("focus(unknown) err = %v, want not_found", err)
	}
	if _, err := callMethod(t, b, "windows", "close", `{"id":"nope"}`); err == nil {
		t.Fatal("close(unknown) accepted")
	}
	// open validates the path.
	if _, err := callMethod(t, b, "windows", "open", `{"path":"http://evil/x"}`); err == nil {
		t.Fatal("open with an off-origin path accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeInvalidInput {
		t.Fatalf("open(bad path) err = %v, want invalid_input", err)
	}
	// openSettings is unsupported without Config.Settings.
	if _, err := callMethod(t, b, "windows", "openSettings", `{}`); err == nil {
		t.Fatal("openSettings without Config.Settings accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("openSettings err = %v, want unsupported", err)
	}

	// list: main first, then a secondary the open created.
	res, err := callMethod(t, b, "windows", "open", `{"path":"/two","title":"Two"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(windowsOpenOutput).ID; got != "w2" {
		t.Fatalf("open id = %q, want w2", got)
	}
	list, err := callMethod(t, b, "windows", "list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	wins := list.(windowsListOutput).Windows
	if len(wins) != 2 || wins[0].ID != "main" || wins[1].ID != "w2" || wins[1].Title != "Two" {
		t.Fatalf("windows.list = %+v", wins)
	}

	// close a secondary through the capability: it unregisters.
	if _, err := callMethod(t, b, "windows", "close", `{"id":"w2"}`); err != nil {
		t.Fatal(err)
	}
	list, _ = callMethod(t, b, "windows", "list", `{}`)
	if n := len(list.(windowsListOutput).Windows); n != 1 {
		t.Fatalf("windows after close = %d, want 1", n)
	}
}

func TestTrayCapabilityWithoutTray(t *testing.T) {
	b, _ := newTestBattery(t) // no Config.Tray
	if _, err := callMethod(t, b, "tray", "setTitle", `{"title":"x"}`); err == nil {
		t.Fatal("tray.setTitle without a tray accepted")
	} else if de, ok := err.(*Error); !ok || de.Code != CodeUnsupported {
		t.Fatalf("tray.setTitle err = %v, want unsupported", err)
	}
}

func TestTraySetTitleThroughBattery(t *testing.T) {
	shell := newFakeShell()
	b := New(Config{
		ID:     "tray.example.app",
		Shell:  shell,
		Logger: testLogger(t),
		Tray:   &Tray{Title: "Notes"},
	})
	b.grants = newMemGrantStore()
	if err := b.SetTrayTitle("Two notes"); err != nil {
		t.Fatal(err)
	}
	if got := shell.trayTitleNow(); got != "Two notes" {
		t.Fatalf("tray title = %q, want Two notes", got)
	}
}

// TestRunWiresSettingsAndTray drives a full Run (fake shell) with
// Config.Settings and Config.Tray set: the settings activation the
// native shells fire from the app menu opens the settings window, the
// tray config (with its RoleShow row) reaches the shell, and the
// ready callback announces the tray title.
func TestRunWiresSettingsAndTray(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	site := appui.NewApp("SettingsE2E")
	layout := appui.NewLayout("public").WithContainer()
	site.Register("/", e2eScreen{}, layout)
	host := uihost.New(site)

	shell := newFakeShell()
	b := New(Config{
		ID:       "wiring.example.app",
		Title:    "Wiring",
		Shell:    shell,
		Logger:   testLogger(t),
		Settings: &WindowSpec{Path: "/settings", Title: "Settings", Width: 300, Height: 200},
		Tray: &Tray{
			Title: "Wire",
			Menu: &Menu{Items: []MenuItem{
				{Title: "Show Wiring", Role: RoleShow},
				{Role: RoleQuit},
			}},
		},
	})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "wiring"}))
	app.Mount(host)
	app.RegisterBattery(b)

	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(app) }()

	waitFor(t, "shell.Run to start", func() bool {
		_, ok := shell.config()
		return ok
	})
	cfg, _ := shell.config()

	// The tray reached the shell with its RoleShow row.
	if cfg.Tray == nil || cfg.Tray.Title != "Wire" {
		t.Fatalf("WindowConfig.Tray = %+v", cfg.Tray)
	}
	if cfg.Tray.Menu == nil || len(cfg.Tray.Menu.Items) != 2 || cfg.Tray.Menu.Items[0].Role != RoleShow {
		t.Fatalf("tray menu = %+v", cfg.Tray.Menu)
	}
	// The ready callback announced the tray (SetTrayTitle; the named
	// Warn path for hosts without one).
	waitFor(t, "tray announce", func() bool { return shell.trayTitleNow() == "Wire" })

	// A settings activation (what the native shells fire for the app
	// menu's Settings… item and RoleSettings rows) opens the window.
	cfg.OnSettings()
	waitFor(t, "settings window to open", func() bool {
		return len(shell.openWindowCallsList()) == 1
	})
	call := shell.openWindowCallsList()[0]
	if call.id != "settings" || !strings.HasPrefix(call.url, "http://127.0.0.1:") || !strings.HasSuffix(call.url, "/settings") {
		t.Fatalf("settings open call = %+v", call)
	}
	// Run is still blocked (the settings window did not end it).
	select {
	case err := <-runErr:
		t.Fatalf("Run returned early: %v", err)
	default:
	}

	shell.Quit()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after Quit")
	}
}
