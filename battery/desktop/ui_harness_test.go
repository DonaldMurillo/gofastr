package desktop_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The battery's UI pieces under the harness: the settings window from
// every entry point, the tray, the notification gate as the page sees
// it, dialogs feeding the fs allow-list, the clipboard, the window
// title mirror, and the served bridge script. Every step runs the real
// Run flow (listener, handshake, frozen registry, chokepoint) with the
// fake shell standing in for the OS.

type uiScreen struct{ heading string }

func (s uiScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text(s.heading))
}

// uiApp assembles a three-screen app around cfg. cfg.Shell defaults to
// a fresh desktoptest.Shell, cfg.ID to a fixed test id.
func uiApp(t *testing.T, cfg desktop.Config) (*framework.App, *desktop.Battery) {
	t.Helper()
	t.Setenv("GOFASTR_ISOLATION", "off")
	site := appui.NewApp("UIHarness")
	layout := appui.NewLayout("app").WithContainer()
	site.SetDefaultLayout(layout)
	site.Register("/", uiScreen{"UI home"}, layout)
	site.Register("/two", uiScreen{"Screen two"}, layout)
	site.Register("/settings", uiScreen{"Settings screen"}, layout)

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "uiharness"}))
	app.Mount(uihost.New(site))
	if cfg.ID == "" {
		cfg.ID = "ui.harness.test"
	}
	if cfg.Shell == nil {
		cfg.Shell = desktoptest.NewShell()
	}
	d := desktop.New(cfg)
	app.RegisterBattery(d)
	return app, d
}

func TestSettingsWindowFromEveryEntryPoint(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title:    "UI",
		Settings: &desktop.WindowSpec{Path: "/settings", Title: "Preferences", Width: 500, Height: 400},
		Menu: &desktop.Menu{Items: []desktop.MenuItem{
			{Title: "File", Children: []desktop.MenuItem{{Title: "Settings…", Role: desktop.RoleSettings}}},
		}},
		Tray: &desktop.Tray{Title: "UI", Menu: &desktop.Menu{Items: []desktop.MenuItem{
			{Title: "Settings…", Role: desktop.RoleSettings},
		}}},
	})
	h := desktoptest.Run(t, app, d)

	// Entry point 1: the app menu's own Settings item.
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return len(h.WindowIDs()) == 2 })
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 || calls[0].ID != "settings" {
		t.Fatalf("OpenWindow calls = %+v", calls)
	}
	if spec := calls[0].Spec; spec.Title != "Preferences" || spec.Width != 500 || spec.Height != 400 {
		t.Fatalf("settings spec = %+v", spec)
	}
	if want := h.URL("/settings"); calls[0].URL != want {
		t.Fatalf("settings URL = %q, want %q", calls[0].URL, want)
	}
	// The page the window loads renders through the same session.
	h.Get("/settings").AssertStatus(t, http.StatusOK).AssertContains(t, "Settings screen")

	// Entry points 2 to 4 focus the open window instead of opening
	// another: main-menu role, tray role, the page's bridge.
	h.ClickMenu("File", "Settings…")
	h.ClickTray("Settings…")
	var opened struct {
		ID string `json:"id"`
	}
	h.Call("windows", "openSettings", nil).MustResult(t, &opened)
	if opened.ID != "settings" {
		t.Fatalf("windows.openSettings = %+v", opened)
	}
	if n := len(h.Shell.OpenWindowCalls()); n != 1 {
		t.Fatalf("OpenWindow calls after re-entry = %d, want 1 (focus, not open)", n)
	}
	if got := h.Window("settings").Focuses(); got != 3 {
		t.Fatalf("settings window focuses = %d, want 3", got)
	}

	// windows.list shows both; closing main through the page is
	// refused; closing settings through the page drops it.
	var list struct {
		Windows []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"windows"`
	}
	h.Call("windows", "list", nil).MustResult(t, &list)
	if len(list.Windows) != 2 || list.Windows[0].ID != "main" || list.Windows[1].Title != "Preferences" {
		t.Fatalf("windows.list = %+v", list.Windows)
	}
	h.Call("windows", "close", map[string]any{"id": "main"}).AssertCode(t, desktop.CodeInvalidInput)
	h.Call("windows", "close", map[string]any{"id": "settings"}).AssertOK(t)
	if ids := h.WindowIDs(); len(ids) != 1 {
		t.Fatalf("windows after page close = %v", ids)
	}
	if !h.Window("settings").Closed() {
		t.Fatal("page close did not close the fake window")
	}

	// The user closing the window through the OS frees the slot too,
	// so the next entry opens a fresh window.
	h.OpenSettings()
	h.Wait("a second settings window", func() bool { return len(h.Shell.OpenWindowCalls()) == 2 })
	h.CloseWindow("settings")
	h.OpenSettings()
	h.Wait("a third settings window", func() bool { return len(h.Shell.OpenWindowCalls()) == 3 })
	h.Call("windows", "focus", map[string]any{"id": "nope"}).AssertCode(t, desktop.CodeNotFound)
}

func TestSettingsUnconfiguredIsUnsupported(t *testing.T) {
	app, d := uiApp(t, desktop.Config{})
	h := desktoptest.Run(t, app, d)
	h.Call("windows", "openSettings", nil).AssertCode(t, desktop.CodeUnsupported)
	if _, err := d.OpenSettings(); err == nil {
		t.Fatal("OpenSettings without Config.Settings succeeded")
	}
	// A secondary window on any screen still opens through the page.
	var opened struct {
		ID string `json:"id"`
	}
	h.Call("windows", "open", map[string]any{"path": "/two", "title": "Two"}).MustResult(t, &opened)
	if opened.ID != "w2" {
		t.Fatalf("windows.open id = %q, want w2", opened.ID)
	}
	h.Call("windows", "open", map[string]any{"path": "https://evil.example/"}).AssertCode(t, desktop.CodeInvalidInput)
}

func TestTrayTitleFromGoAndFromThePage(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Tray: &desktop.Tray{Title: "Tray", Tooltip: "tip"}})
	h := desktoptest.Run(t, app, d)

	if got := h.Shell.TrayTitle(); got != "Tray" {
		t.Fatalf("tray title after boot = %q", got)
	}
	cfg, _ := h.Shell.Config()
	if cfg.Tray == nil || cfg.Tray.Tooltip != "tip" {
		t.Fatalf("shell got tray %+v", cfg.Tray)
	}
	if err := d.SetTrayTitle("Tray (1)"); err != nil {
		t.Fatal(err)
	}
	if got := h.Shell.TrayTitle(); got != "Tray (1)" {
		t.Fatalf("tray title after Go SetTrayTitle = %q", got)
	}
	h.Call("tray", "setTitle", map[string]any{"title": "Tray (2)"}).AssertOK(t)
	if got := h.Shell.TrayTitle(); got != "Tray (2)" {
		t.Fatalf("tray title after the page's setTitle = %q", got)
	}
	h.Call("tray", "setTitle", map[string]any{"title": strings.Repeat("x", 65)}).AssertCode(t, desktop.CodeInvalidInput)
	if got := h.Shell.TrayTitle(); got != "Tray (2)" {
		t.Fatalf("an over-long title changed the tray to %q", got)
	}
}

func TestTrayUnconfiguredIsUnsupported(t *testing.T) {
	app, d := uiApp(t, desktop.Config{})
	h := desktoptest.Run(t, app, d)
	h.Call("tray", "setTitle", map[string]any{"title": "x"}).AssertCode(t, desktop.CodeUnsupported)
	if got := h.Shell.TrayTitle(); got != "" {
		t.Fatalf("the shell got a tray title %q with no tray configured", got)
	}
}

func TestNotificationGateAsThePageSeesIt(t *testing.T) {
	t.Run("unanswered prompt denies and persists", func(t *testing.T) {
		app, d := uiApp(t, desktop.Config{})
		h := desktoptest.Run(t, app, d)

		show := map[string]any{"title": "Hi", "body": "there"}
		h.Call("notifications", "show", show).AssertCode(t, desktop.CodeDenied)
		prompts := h.Shell.Prompts()
		if len(prompts) != 1 {
			t.Fatalf("prompts = %+v", prompts)
		}
		if p := prompts[0]; p.Capability != "notifications" || p.Method != "show" || p.Permission != "notifications:show" {
			t.Fatalf("prompt = %+v", p)
		}
		// The deny is remembered: no second alert, still refused.
		h.Call("notifications", "show", show).AssertCode(t, desktop.CodeDenied)
		if n := len(h.Shell.Prompts()); n != 1 {
			t.Fatalf("prompts after a persisted deny = %d, want 1", n)
		}
		if n := len(h.Shell.Notifications()); n != 0 {
			t.Fatalf("a denied call still notified %d times", n)
		}
		// In-process callers are the app itself, not the page: no gate.
		if err := d.Notify(context.Background(), desktop.Notification{Title: "From Go"}); err != nil {
			t.Fatal(err)
		}
		if got := h.Shell.Notifications(); len(got) != 1 || got[0].Title != "From Go" {
			t.Fatalf("notifications = %+v", got)
		}
	})

	t.Run("allow once prompts again, allow persists", func(t *testing.T) {
		app, d := uiApp(t, desktop.Config{})
		h := desktoptest.Run(t, app, d)

		show := map[string]any{"title": "Saved", "subtitle": "Notes", "body": "x"}
		h.Answer(desktop.DecisionAllowOnce)
		h.Call("notifications", "show", show).AssertOK(t)
		h.Answer(desktop.DecisionAllow)
		h.Call("notifications", "show", show).AssertOK(t)
		h.Call("notifications", "show", show).AssertOK(t)
		if n := len(h.Shell.Prompts()); n != 2 {
			t.Fatalf("prompts = %d, want 2 (once, then the persisted allow)", n)
		}
		got := h.Shell.Notifications()
		if len(got) != 3 || got[0].Subtitle != "Notes" {
			t.Fatalf("notifications = %+v", got)
		}
		h.Call("notifications", "show", map[string]any{"body": "no title"}).AssertCode(t, desktop.CodeInvalidInput)
	})
}

func TestDialogsFeedTheFSAllowList(t *testing.T) {
	app, d := uiApp(t, desktop.Config{})
	h := desktoptest.Run(t, app, d)

	dir := t.TempDir()
	target := filepath.Join(dir, "export.md")
	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(other, []byte("not picked"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The permission gate runs before the allow-list, so grant fs:read
	// and fs:write up front (the prompts fire on first use, in that
	// order); every later refusal below is the allow-list's own.
	h.Answer(desktop.DecisionAllow, desktop.DecisionAllow)

	// Nothing is readable before a dialog put it on the list.
	if r := h.Call("fs", "readText", map[string]any{"path": other}).AssertCode(t, desktop.CodeDenied); r.Message != "path is not accessible" {
		t.Fatalf("off-list read refused with %q, want the allow-list's message", r.Message)
	}

	h.Shell.SetSaveFile(target, nil)
	var picked struct {
		Path string `json:"path"`
	}
	h.Call("dialogs", "saveFile", map[string]any{"defaultName": "export.md"}).MustResult(t, &picked)
	if picked.Path != target {
		t.Fatalf("saveFile = %q", picked.Path)
	}
	h.Call("fs", "writeText", map[string]any{"path": target, "text": "# hello\n"}).AssertOK(t)
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "# hello\n" {
		t.Fatalf("written file = %q, %v", data, err)
	}
	var read struct {
		Text string `json:"text"`
	}
	h.Call("fs", "readText", map[string]any{"path": target}).MustResult(t, &read)
	if read.Text != "# hello\n" {
		t.Fatalf("readText = %q", read.Text)
	}
	var st struct {
		Size  int64 `json:"size"`
		IsDir bool  `json:"isDir"`
	}
	h.Call("fs", "stat", map[string]any{"path": target}).MustResult(t, &st)
	if st.Size != 8 || st.IsDir {
		t.Fatalf("stat = %+v", st)
	}
	// The sibling the user never picked stays off the list.
	h.Call("fs", "readText", map[string]any{"path": other}).AssertCode(t, desktop.CodeDenied)

	// A cancelled dialog is the cancelled code, and lists nothing.
	h.Shell.SetSaveFile("", desktop.ErrCancelled)
	h.Call("dialogs", "saveFile", nil).AssertCode(t, desktop.CodeCancelled)
	h.Shell.SetOpenFile([]string{other}, nil)
	var opened struct {
		Paths []string `json:"paths"`
	}
	h.Call("dialogs", "openFile", map[string]any{"multiple": false}).MustResult(t, &opened)
	if len(opened.Paths) != 1 || opened.Paths[0] != other {
		t.Fatalf("openFile = %+v", opened)
	}
	h.Call("fs", "readText", map[string]any{"path": other}).MustResult(t, &read)
	if read.Text != "not picked" {
		t.Fatalf("readText after openFile = %q", read.Text)
	}
	if got := h.Shell.DialogCalls(); got != 3 {
		t.Fatalf("dialog calls = %d, want 3", got)
	}
	if n := len(h.Shell.Prompts()); n != 2 {
		t.Fatalf("prompts = %d, want 2 (fs:read, fs:write); dialogs themselves are ungated", n)
	}
}

func TestClipboardRoundTripThroughThePage(t *testing.T) {
	app, d := uiApp(t, desktop.Config{})
	h := desktoptest.Run(t, app, d)

	h.Answer(desktop.DecisionAllow, desktop.DecisionAllow)
	h.Call("clipboard", "writeText", map[string]any{"text": "copied link"}).AssertOK(t)
	if got := h.Shell.ClipboardWrites(); len(got) != 1 || got[0] != "copied link" {
		t.Fatalf("clipboard writes = %v", got)
	}
	h.Shell.SetClipboardText("copied link")
	var read struct {
		Text string `json:"text"`
	}
	h.Call("clipboard", "readText", nil).MustResult(t, &read)
	if read.Text != "copied link" {
		t.Fatalf("readText = %q", read.Text)
	}
	// Two permissions, two prompts, both persisted.
	if n := len(h.Shell.Prompts()); n != 2 {
		t.Fatalf("prompts = %d, want 2", n)
	}
	h.Call("clipboard", "readText", nil).AssertOK(t)
	if n := len(h.Shell.Prompts()); n != 2 {
		t.Fatalf("prompts after persisted allow = %d, want 2", n)
	}
}

func TestWindowTitleAndSnapshotThroughThePage(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "Start"})
	h := desktoptest.Run(t, app, d)

	h.Call("window", "setTitle", map[string]any{"title": "Note one"}).AssertOK(t)
	if got := h.Window("main").Titles(); len(got) != 1 || got[0] != "Note one" {
		t.Fatalf("titles = %v", got)
	}
	var title struct {
		Title string `json:"title"`
	}
	h.Call("window", "title", nil).MustResult(t, &title)
	if title.Title != "Note one" {
		t.Fatalf("window.title = %q", title.Title)
	}
	var snap struct {
		PNG string `json:"png"`
	}
	h.Call("window", "snapshot", nil).MustResult(t, &snap)
	png, err := base64.StdEncoding.DecodeString(snap.PNG)
	if err != nil || string(png) != string(desktoptest.PNG1x1()) {
		t.Fatalf("snapshot bytes differ from the fixture (%v)", err)
	}
}

func TestBridgeScriptAndHostMarker(t *testing.T) {
	app, d := uiApp(t, desktop.Config{})
	h := desktoptest.Run(t, app, d)

	page := h.Get("/").AssertStatus(t, http.StatusOK)
	// The page references the bridge by its nonce-versioned URL, and
	// ships no inline script for it.
	i := strings.Index(page.Body, `/__gofastr/desktop/bridge.js?v=`)
	if i < 0 {
		t.Fatalf("page does not reference the bridge script: %.400s", page.Body)
	}
	src := page.Body[i:]
	src = src[:strings.IndexAny(src, `"'`)]
	js := h.Get(src).AssertStatus(t, http.StatusOK)
	if ct := js.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("bridge.js content type = %q", ct)
	}
	for _, want := range []string{`D["window"]`, `"setTitle"`, `D["notifications"]`, `D["tray"]`, `D.manifest = JSON.parse(`} {
		if !strings.Contains(js.Body, want) {
			t.Fatalf("bridge.js missing %s", want)
		}
	}
	// The host marker a shell injects at document start is the same
	// script a browser-backed harness injects.
	marker := desktop.BootstrapJS("main")
	if !strings.HasPrefix(marker, "window.__gofastr_desktop = JSON.parse(") || !strings.Contains(marker, `\"os\":`) || !strings.Contains(marker, `\"window\":\"main\"`) {
		t.Fatalf("BootstrapJS = %q", marker)
	}
	// A stranger gets the inert answer from the gate, not the script.
	req, _ := http.NewRequest(http.MethodGet, h.URL(src), nil)
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger bridge.js = %d, want 403", resp.StatusCode)
	}
}
