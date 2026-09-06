//go:build desktop_e2e && darwin && arm64

package desktop_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The phase-2 shell scenario, once the hand-rolled e2e's private
// machinery and now driven through the exported surface: the page half
// through the real WKWebView (desktoptest's NativeHarness runs its
// evals inside the page), the user's hands through the shell's
// NativeDriver (ActivateMenu, ClickPrompt, PostDeepLink, WindowState,
// CloseWindowNative). Every step and assertion of the old scenario is
// kept. The two verification oracles that have no driver stay local,
// straight objc through Shell.Main: reading the pasteboard under the
// capability, and isOpaque on a styled window.
//
// The app under test and TestMain live in native_e2e_test.go; the
// scenario runs last in the phase because its final step quits the
// app.

// phaseShellScenario walks the whole shell: boot, the bridge through
// the real page, real permission alerts clicked, the settings window,
// CloseHidesWindow, the tray, a styled panel, a deep link, quit via
// the menu, and the persisted grants after Run drained.
func phaseShellScenario(t desktoptest.TB, h *desktoptest.NativeHarness) {
	// Grants persisted by the earlier phase tests (a scripted deny of
	// clipboard:read) would silence the alerts this scenario answers by
	// hand; reset so all four prompts fire.
	if err := h.Battery.ResetGrantsForTest(context.Background()); err != nil {
		t.Fatalf("reset grants for the scenario: %v", err)
	}

	// The alert answerer: four permission prompts are expected
	// (clipboard:write, clipboard:read, notifications:show, fs:read),
	// each answered by clicking the alert's Allow button through the
	// driver's modal-safe hop.
	promptErrs := make(chan error, 1)
	go func() {
		var first error
		for range 4 {
			if err := h.ClickPrompt("Allow"); err != nil && first == nil {
				first = err
			}
		}
		promptErrs <- first
	}()

	// GC hammer: a full GC every 100ms while the main goroutine parks
	// inside cgocall([NSApp run]) and callbacks run Go on it, the
	// hazard class the fake-cgo layer exists for.
	gcStop := make(chan struct{})
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		cycles := 0
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-gcStop:
				t.Logf("gc hammer: %d cycles over the run", cycles)
				return
			case <-tick.C:
				runtime.GC()
				cycles++
			}
		}
	}()
	defer func() {
		close(gcStop)
		<-gcDone
	}()

	h.Navigate("/")
	t.Logf("boot: page and desktop bridge ready; __gofastr_desktop = %s",
		jsString(h.Eval(t, `return JSON.stringify(window.__gofastr_desktop)`)))

	// Step 1: window.title through the page; setTitle read back
	// natively.
	var title struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("window", "title", nil), "window.title"), &title); err != nil {
		t.Fatalf("window.title result: %v", err)
	}
	if title.Title != e2eTitle {
		t.Fatalf("step1: window.title() = %q, want %q", title.Title, e2eTitle)
	}
	wantCallOK(t, h.Call("window", "setTitle", map[string]any{"title": "Renamed by e2e"}), "window.setTitle")
	h.Wait("step1: the native title", func() bool {
		st, err := h.WindowState("main")
		return err == nil && st.Title == "Renamed by e2e"
	})

	// Step 2: clipboard round-trip through the real pasteboard, with
	// the permission alert answered by a programmatic Allow click.
	const clipText = "gofastr-shell-e2e-clipboard"
	wantCallOK(t, h.Call("clipboard", "writeText", map[string]any{"text": clipText}), "clipboard.writeText")
	var readBack struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("clipboard", "readText", nil), "clipboard.readText"), &readBack); err != nil {
		t.Fatalf("step2: clipboard.readText result: %v", err)
	}
	if readBack.Text != clipText {
		t.Fatalf("step2: clipboard.readText through the page = %q, want %q", readBack.Text, clipText)
	}
	// (the answerer's own errors surface at the scenario's end)
	if got := nativePasteboard(t, h); got != clipText {
		t.Fatalf("step2: native pasteboard = %q, want %q", got, clipText)
	} else {
		t.Logf("step2: clipboard round-trip through the page, pasteboard verified natively")
	}

	// Step 3: notifications.show is unsupported outside a bundle.
	r := h.Call("notifications", "show", map[string]any{"title": "nope"})
	if r.OK || r.Code != desktop.CodeUnsupported {
		t.Fatalf("step3: notifications.show = ok:%v code:%q, want unsupported", r.OK, r.Code)
	}
	t.Logf("step3: notifications.show rejected with unsupported (unbundled), message: %s", r.Message)

	// Step 4: fs.readText on a path allow-listed in-process (dialogs
	// need UI automation; the brief skips driving them).
	fsPath := filepath.Join(e2eDataDir(), "e2e-file.txt")
	if err := os.WriteFile(fsPath, []byte("fs allow-list round trip"), 0o600); err != nil {
		t.Fatalf("step4: write fs fixture: %v", err)
	}
	if err := h.Battery.AllowPath(fsPath); err != nil {
		t.Fatalf("step4: AllowPath: %v", err)
	}
	var fsOut struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("fs", "readText", map[string]any{"path": fsPath}), "fs.readText"), &fsOut); err != nil {
		t.Fatalf("step4: fs.readText result: %v", err)
	}
	if fsOut.Text != "fs allow-list round trip" {
		t.Fatalf("step4: fs.readText = %q", fsOut.Text)
	}

	// Island RPC through the real window (spike claim, re-proven on
	// the battery path).
	before := h.Text("#e2e-count")
	h.Click("button")
	h.Wait("the island RPC to change #e2e-count", func() bool {
		return h.TextQuiet("#e2e-count") != before && h.TextQuiet("#e2e-count") != ""
	})
	t.Logf("island RPC: %s -> %s", before, h.TextQuiet("#e2e-count"))

	// Step 5: menu Navigate, activate the item programmatically and
	// assert the page path changed via the page itself.
	h.ClickMenu("Go", "Two")
	h.WaitLocation("/two")
	if got := h.Text("h1"); got != "Shell E2E Second" {
		t.Fatalf("step5: h1 after the menu navigate = %q", got)
	}
	t.Logf("step5: menu Navigate moved the page to %s (h1: %q)", h.Location(), h.Text("h1"))

	// The Handler item runs its Go func.
	h.ClickMenu("Go", "Ping")
	select {
	case err := <-e2eHandlerRan:
		if err != nil {
			t.Fatalf("menu Handler ctx err: %v", err)
		}
		t.Logf("menu Handler item ran with a live context")
	case <-time.After(5 * time.Second):
		t.Fatal("menu Handler item never ran")
	}

	// Step 6: snapshot, through the page bridge AND natively; the
	// native bytes are written to the spike output dir and decoded.
	var snap struct {
		PNG string `json:"png"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("window", "snapshot", nil), "window.snapshot"), &snap); err != nil || snap.PNG == "" {
		t.Fatalf("step6: window.snapshot returned no png (%v)", err)
	}
	if _, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(snap.PNG))); err != nil {
		t.Fatalf("step6: page snapshot png does not decode: %v", err)
	}
	t.Logf("step6: page snapshot decodes as PNG (%d base64 chars)", len(snap.PNG))
	outDir := spikeOutDir()
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatalf("step6: mkdir %s: %v", outDir, err)
	}
	h.SavePNG(t, filepath.Join(outDir, "shell.png"))
	t.Logf("step6: shell.png written at %s", filepath.Join(outDir, "shell.png"))
	h.OpenSettings() // the app menu's own item focuses the open window
	wantCallOK(t, h.Call("windows", "close", map[string]any{"id": "settings"}), "windows.close(settings)")
	h.Wait("step8: windows.close to drop the settings window", func() bool {
		return h.Window("settings") == nil
	})
	if st, err := h.WindowState("main"); err != nil || !st.Visible {
		t.Fatalf("step8: main window not visible after the settings close (%+v, %v)", st, err)
	}
	t.Logf("step8: settings window closed through the page; main window still visible")
	listJSON := string(wantCallOK(t, h.Call("windows", "list", nil), "windows.list"))
	if !strings.Contains(listJSON, `"id":"main"`) || strings.Contains(listJSON, `"settings"`) {
		t.Fatalf("step8: windows.list = %s, want main (plus the launch widget), no settings", listJSON)
	}

	// Step 9: CloseHidesWindow, the main window's red button hides it
	// instead of quitting, RoleShow (the tray's item) brings it back.
	h.CloseWindow("main")
	h.Wait("step9: performClose to hide the main window", func() bool {
		st, err := h.WindowState("main")
		return err == nil && !st.Visible
	})
	t.Logf("step9: performClose hid the main window; the process is alive in the menu bar")
	h.ClickTray("Show Window")
	h.Wait("step9: tray RoleShow to bring the main window back", func() bool {
		st, err := h.WindowState("main")
		return err == nil && st.Visible
	})

	// Step 10: the tray item, configured title read back natively,
	// then a page-driven setTitle round-trip.
	reader, ok := h.Shell.(interface{ StatusItemTitle() string })
	if !ok {
		t.Fatalf("the shell %T exposes no StatusItemTitle", h.Shell)
	}
	if got := reader.StatusItemTitle(); got != e2eTrayTitle {
		t.Fatalf("step10: status item button title = %q, want %q", got, e2eTrayTitle)
	}
	wantCallOK(t, h.Call("tray", "setTitle", map[string]any{"title": e2eTrayTitle + "2"}), "tray.setTitle")
	h.Wait("step10: the tray title to change", func() bool {
		return reader.StatusItemTitle() == e2eTrayTitle+"2"
	})

	// Step 11: window styles, a borderless floating transparent panel
	// opened through the page's bridge, every style claim read back
	// off the live NSPanel, and its page reporting its OWN window id
	// (the per-window user script).
	var panel struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(wantCallOK(t, h.Call("windows", "open", map[string]any{
		"path": "/two", "title": "Floater", "width": 300, "height": 180,
		"style": map[string]any{"chrome": "none", "panel": true, "transparent": true, "float": true},
	}), "windows.open with a style"), &panel); err != nil || panel.ID == "" {
		t.Fatalf("step11: windows.open with a style = %+v (%v)", panel, err)
	}
	t.Logf("step11: styled windows.open returned id %q", panel.ID)
	h.Wait("step11: the styled window to appear", func() bool { return h.Window(panel.ID) != nil })
	st, err := h.WindowState(panel.ID)
	if err != nil {
		t.Fatalf("step11: the styled window: %v", err)
	}
	const (
		maskTitled             = uint64(1) << 0 // NSWindowStyleMaskTitled
		maskNonactivatingPanel = uint64(1) << 7 // NSWindowStyleMaskNonactivatingPanel
		floatingLevel          = 3              // NSFloatingWindowLevel
	)
	if st.StyleMask&maskTitled != 0 || st.StyleMask != maskNonactivatingPanel {
		t.Fatalf("step11: styleMask = %#x, want %#x (borderless, no titled bit, nonactivating)", st.StyleMask, maskNonactivatingPanel)
	}
	t.Logf("step11: styleMask = %#x: no titled bit, nonactivating panel only", st.StyleMask)
	if st.Level != floatingLevel {
		t.Fatalf("step11: window level = %d, want %d (floating)", st.Level, floatingLevel)
	}
	if st.Class != "NSPanel" {
		t.Fatalf("step11: window class = %q, want NSPanel", st.Class)
	}
	if st.Key {
		t.Fatal("step11: the panel became key; a non-activating widget must not steal key")
	}
	if webviewIsOpaque(t, h, panel.ID) {
		t.Fatal("step11: isOpaque = YES, want NO for Transparent")
	}
	t.Logf("step11: floating level, NSPanel class, not key, isOpaque NO")
	nw := h.Window(panel.ID)
	h.Wait("step11: the panel's page to learn its own id", func() bool {
		raw, err := nw.EvalQuiet(`return window.__gofastr_desktop ? window.__gofastr_desktop.window : "pending"`)
		return err == nil && jsString(raw) == panel.ID
	})
	wantCallOK(t, h.Call("windows", "close", map[string]any{"id": panel.ID}), "windows.close(panel)")
	h.Wait("step11: the styled window to close", func() bool { return h.Window(panel.ID) == nil })

	// Step 12: deep links. A GetURL Apple Event delivered to the
	// handler Run registered at bridge creation navigates the main
	// window and reaches the page as deep_link.
	h.Navigate("/")
	h.RecordEvents(t, "deep_link")
	h.OpenURL(e2eScheme + "://two")
	h.WaitLocation("/two")
	ev := h.WaitEvent("deep_link")
	if got := string(ev.Payload); got != `{"url":"`+e2eScheme+`://two","path":"/two"}` {
		t.Fatalf("step12: deep_link listeners saw %s", got)
	}
	t.Logf("step12: the GetURL Apple Event navigated the main window to /two and delivered deep_link {url, path}")

	// Step 13: quit through the menu's quit role; battery Run should
	// return nil.
	h.ClickMenu(e2eTitle, "quit")
	runErr, returned := h.WaitRun(30 * time.Second)
	if !returned {
		t.Fatal("step13: battery Run did not return after the quit role")
	}
	if runErr != nil {
		t.Fatalf("step13: battery Run returned %v", runErr)
	}
	t.Logf("step13: quit role activated; battery Run returned nil")

	// The alert answerer must have clicked all four.
	select {
	case err := <-promptErrs:
		if err != nil {
			t.Fatalf("the alert answerer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the alert answerer never clicked its four alerts")
	}

	// The grants table: clipboard:write, clipboard:read,
	// notifications:show, fs:read were each prompted and answered
	// Allow, so all four must be persisted in app.db.
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(e2eDataDir(), "app.db")))
	if err != nil {
		t.Fatalf("open app.db for the grants check: %v", err)
	}
	defer db.Close()
	var grantProblems []string
	for _, perm := range []string{"clipboard:write", "clipboard:read", "notifications:show", "fs:read"} {
		var decision string
		err := db.QueryRow(`SELECT decision FROM desktop_grants WHERE permission = ?`, perm).Scan(&decision)
		if err != nil || decision != "allow" {
			grantProblems = append(grantProblems, fmt.Sprintf("%s: decision=%q err=%v, want allow", perm, decision, err))
		} else {
			t.Logf("grant %s persisted as allow in app.db", perm)
		}
	}
	if len(grantProblems) > 0 {
		t.Fatalf("grants check: %s", strings.Join(grantProblems, "; "))
	}
}

// nativePasteboard reads the general pasteboard's string directly,
// independent of the capability under test, on the main thread.
func nativePasteboard(t desktoptest.TB, h *desktoptest.NativeHarness) string {
	const pasteboardString = "public.utf8-plain-text" // NSPasteboardTypeString
	var text string
	if err := shellMain(t, h, func() {
		pb := objc.ID(objc.Send(objc.Class("NSPasteboard"), objc.Sel("generalPasteboard")))
		if pb == 0 {
			return
		}
		if str := objc.Send(pb, objc.Sel("stringForType:"), uintptr(objc.NSString(pasteboardString))); str != 0 {
			text = objc.GoString(objc.ID(str))
		}
	}); err != nil {
		t.Logf("pasteboard read hop: %v", err)
	}
	return text
}

// webviewIsOpaque reads isOpaque on the NSWindow behind a window's
// web view, the one style claim WindowState does not carry.
func webviewIsOpaque(t desktoptest.TB, h *desktoptest.NativeHarness, id string) bool {
	w := h.Window(id)
	if w == nil {
		t.Fatalf("no window %s to read isOpaque from", id)
	}
	var opaque bool
	if err := shellMain(t, h, func() {
		win := objc.Send(objc.ID(w.Native()), objc.Sel("window"))
		if win == 0 {
			return
		}
		opaque = objc.Send(objc.ID(win), objc.Sel("isOpaque")) != 0
	}); err != nil {
		t.Logf("isOpaque read hop: %v", err)
	}
	return opaque
}

// spikeOutDir is where shell.png lands.
func spikeOutDir() string {
	if d := os.Getenv("GOFASTR_SPIKE_OUT"); d != "" {
		return d
	}
	return "/private/tmp/claude-501/-Users-dom-programming-gofastr/3153fbe6-2abc-4626-bc55-2c56eeb94112/scratchpad/spike-out"
}

// shellMain hops fn onto the UI thread. Main is not part of the Shell
// interface (only the native shells have a UI thread); the darwin
// shell exports it, which is what this asserts on.
func shellMain(t desktoptest.TB, h *desktoptest.NativeHarness, fn func()) error {
	mh, ok := h.Shell.(interface{ Main(fn func()) error })
	if !ok {
		t.Fatalf("the shell %T exposes no Main hop", h.Shell)
	}
	return mh.Main(fn)
}
