package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The app as the window runs it: desktoptest.Run drives the battery's
// real Run flow (listener, boot handshake, frozen registry, menus,
// tray) with the fake shell as the OS. Every request here carries the
// window's session and therefore the local identity, the way the
// WebView's do; main_test.go covers the same app in its --serve shape.

func newHarness(t *testing.T) *desktoptest.Harness {
	t.Helper()
	// buildApp opens the data dir before Run, so the override must be
	// in place first (Run would only set it when unset).
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	shell := desktoptest.NewShell()
	app, d, err := buildApp(shell)
	if err != nil {
		t.Fatal(err)
	}
	return desktoptest.Run(t, app, d)
}

// createNote saves a note through the app's REST route the way the
// editor's form does, and returns its id.
func createNote(t *testing.T, h *desktoptest.Harness, title, body string) string {
	t.Helper()
	created := h.Post("/api/notes", map[string]any{"title": title, "body": body}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(created.Body, &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", created.Body)
	}
	return id
}

func TestWindowBootsIntoTheNotesList(t *testing.T) {
	h := newHarness(t)

	home := h.Get("/").AssertStatus(t, http.StatusOK)
	for _, want := range []string{"Notes", "New Note", `/__gofastr/desktop/bridge.js?v=`, `src="/desktop-notes.js?v=`} {
		home.AssertContains(t, want)
	}
	// The rows the window shows belong to the local identity: a note
	// saved from the window is on the list and on the detail screen.
	id := createNote(t, h, "First from the window", "body")
	h.Get("/").AssertStatus(t, http.StatusOK).AssertContains(t, "First from the window")
	h.Get("/notes/"+id).AssertStatus(t, http.StatusOK).AssertContains(t, "<title>First from the window")

	// Another local process on the port sees nothing.
	req, _ := http.NewRequest(http.MethodGet, h.URL("/api/notes"), nil)
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger GET /api/notes = %d, want 403", resp.StatusCode)
	}

	// The plugin's capability is in the frozen manifest and callable.
	var found bool
	for _, c := range h.Manifest().Capabilities {
		found = found || c.Name == "systeminfo"
	}
	if !found {
		t.Fatal("manifest lacks the systeminfo plugin capability")
	}
	var cpu struct {
		Count int `json:"count"`
	}
	h.Call("systeminfo", "cpuCount", nil).MustResult(t, &cpu)
	if cpu.Count < 1 {
		t.Fatalf("cpuCount = %d", cpu.Count)
	}
}

func TestFileMenuNewNoteNavigates(t *testing.T) {
	h := newHarness(t)
	h.ClickMenu("File", "New note")
	h.Wait("the navigate eval", func() bool { return len(h.Navigations()) == 1 })
	h.PressKey("cmd+n")
	h.Wait("the accelerator's eval", func() bool { return len(h.Navigations()) == 2 })
	if got := h.Navigations(); got[0] != "/notes/new" || got[1] != "/notes/new" {
		t.Fatalf("navigations = %v", got)
	}
	h.ClickTray("New note")
	h.Wait("the tray item's eval", func() bool { return len(h.Navigations()) == 3 })
	// The screen the item opens renders through the window's session.
	h.Get("/notes/new").AssertStatus(t, http.StatusOK).AssertContains(t, "Copy link")
}

func TestExportAllWritesMarkdownAndEmits(t *testing.T) {
	h := newHarness(t)
	createNote(t, h, "Alpha", "first body")
	createNote(t, h, "Beta", "")

	target := filepath.Join(t.TempDir(), "notes.md")
	h.Shell.SetSaveFile(target, nil)
	h.ClickMenu("File", "Export all…")

	ev := h.WaitEvent("notes_exported")
	var payload struct {
		Path string `json:"path"`
	}
	if err := ev.Unmarshal(&payload); err != nil || payload.Path != target {
		t.Fatalf("notes_exported payload = %s (%v)", ev.Payload, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	md := string(data)
	if !strings.Contains(md, "# Alpha\n\nfirst body\n") || !strings.Contains(md, "# Beta\n") {
		t.Fatalf("export = %q", md)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Fatalf("export mode = %o, want 0600", info.Mode().Perm())
	}
	if got := h.Shell.DialogCalls(); got != 1 {
		t.Fatalf("dialog calls = %d", got)
	}

	// A cancelled dialog exports nothing and emits nothing.
	h.Shell.SetSaveFile("", desktop.ErrCancelled)
	h.PressKey("cmd+e")
	h.Wait("the second dialog", func() bool { return h.Shell.DialogCalls() == 2 })
	if n := len(h.Events()); n != 1 {
		t.Fatalf("events after a cancelled export = %d, want 1", n)
	}
}

func TestSettingsWindowFromMenuTrayAndPage(t *testing.T) {
	h := newHarness(t)

	// The app menu's Settings item (cmd+,) opens the configured window
	// on /settings, sized as declared.
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return len(h.WindowIDs()) == 2 })
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 || calls[0].ID != "settings" || calls[0].URL != h.URL("/settings") {
		t.Fatalf("OpenWindow calls = %+v", calls)
	}
	if spec := calls[0].Spec; spec.Title != "Settings" || spec.Width != 520 || spec.Height != 460 {
		t.Fatalf("settings spec = %+v", spec)
	}
	// The window's page is the battery's preferences form.
	h.Get("/settings").AssertStatus(t, http.StatusOK).AssertContains(t, "Notify on save")

	// The File menu row and the tray row focus the same window.
	h.ClickMenu("File", "Settings…")
	h.ClickTray("Settings…")
	if n := len(h.Shell.OpenWindowCalls()); n != 1 {
		t.Fatalf("OpenWindow calls after the menu and tray rows = %d, want 1", n)
	}
	if got := h.Window("settings").Focuses(); got != 2 {
		t.Fatalf("settings focuses = %d, want 2", got)
	}
	// The user closes it; the page reopens a fresh one.
	h.CloseWindow("settings")
	var opened struct {
		ID string `json:"id"`
	}
	h.Call("windows", "openSettings", nil).MustResult(t, &opened)
	if opened.ID != "settings" || len(h.Shell.OpenWindowCalls()) != 2 {
		t.Fatalf("reopen = %+v, calls = %d", opened, len(h.Shell.OpenWindowCalls()))
	}
}

func TestSettingsSaveThroughTheWindowSession(t *testing.T) {
	h := newHarness(t)
	d := h.Battery

	// Off through the page capability: a save no longer notifies, and
	// the re-rendered form reflects it.
	h.Call("preferences", "set", map[string]any{"values": map[string]any{"notify_on_save": false}}).AssertOK(t)
	if d.Preferences().Bool("notify_on_save") {
		t.Fatal("notify_on_save did not save false")
	}
	form := h.Get("/settings").AssertStatus(t, http.StatusOK).Body
	box := form[strings.Index(form, `id="f-notify_on_save"`):]
	if strings.Contains(box[:strings.Index(box, ">")], "checked") {
		t.Fatal("the form still renders the box checked after saving false")
	}
	createNote(t, h, "Quiet", "")
	if n := len(h.Shell.Notifications()); n != 0 {
		t.Fatalf("notifications with notify_on_save off = %d", n)
	}

	// On again: a save notifies through the shell.
	h.Call("preferences", "set", map[string]any{"values": map[string]any{"notify_on_save": true}}).AssertOK(t)
	createNote(t, h, "Loud", "")
	got := h.Shell.Notifications()
	if len(got) != 1 || got[0].Title != "Saved" || got[0].Body != "Loud" {
		t.Fatalf("notifications = %+v", got)
	}
}

func TestTrayIsConfiguredAndHidesOnClose(t *testing.T) {
	h := newHarness(t)
	cfg, _ := h.Shell.Config()
	if cfg.Tray == nil || cfg.Tray.Title != "Notes" || !cfg.Tray.CloseHidesWindow || len(cfg.Tray.Icon) == 0 {
		t.Fatalf("tray config = %+v", cfg.Tray)
	}
	if got := h.Shell.TrayTitle(); got != "Notes" {
		t.Fatalf("tray title = %q", got)
	}
	h.CloseMainWindow()
	if !h.Window("main").Hidden() {
		t.Fatal("closing the main window did not hide it")
	}
	h.ClickTray("Show Notes")
	if h.Window("main").Hidden() {
		t.Fatal("the tray's Show item did not bring the window back")
	}
	// The page can retitle the tray item.
	h.Call("tray", "setTitle", map[string]any{"title": "Notes (2)"}).AssertOK(t)
	if got := h.Shell.TrayTitle(); got != "Notes (2)" {
		t.Fatalf("tray title after setTitle = %q", got)
	}
	// Quit from the tray ends Run cleanly.
	h.ClickTray("quit")
	if err := h.Quit(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
}

func TestCopyLinkPermissionFlow(t *testing.T) {
	h := newHarness(t)
	link := h.URL("/notes/abc")

	// First use prompts; an unanswered prompt denies and is remembered.
	h.Call("clipboard", "writeText", map[string]any{"text": link}).AssertCode(t, desktop.CodeDenied)
	h.Call("clipboard", "writeText", map[string]any{"text": link}).AssertCode(t, desktop.CodeDenied)
	if n := len(h.Shell.Prompts()); n != 1 {
		t.Fatalf("prompts = %d, want 1 (the deny persisted)", n)
	}
	if n := len(h.Shell.ClipboardWrites()); n != 0 {
		t.Fatalf("clipboard writes behind a deny = %d", n)
	}
}

func TestCopyLinkAllowed(t *testing.T) {
	h := newHarness(t)
	link := h.URL("/notes/abc")
	h.Answer(desktop.DecisionAllow)
	h.Call("clipboard", "writeText", map[string]any{"text": link}).AssertOK(t)
	h.Call("clipboard", "writeText", map[string]any{"text": link}).AssertOK(t)
	if got := h.Shell.ClipboardWrites(); len(got) != 2 || got[0] != link {
		t.Fatalf("clipboard writes = %v", got)
	}
	if n := len(h.Shell.Prompts()); n != 1 {
		t.Fatalf("prompts = %d, want 1 (the allow persisted)", n)
	}
}
