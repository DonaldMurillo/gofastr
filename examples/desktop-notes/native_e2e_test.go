//go:build desktop_e2e && darwin && arm64

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The app's pages inside the REAL app shell: buildApp(nil) picks the
// darwin shell, NativeMain boots it once for the whole test binary,
// and every step runs in the actual WKWebView (the settings form's
// real checkbox and Save button, the widget's real form and Close
// button). These replace the headless-Chrome pair
// (browser_e2e_test.go, widget_browser_e2e_test.go, deleted): desktop
// tests never run against a browser stand-in.
//
// Run by hand on a Mac with a display:
//
//	CGO_ENABLED=0 go test -count=1 -tags desktop_e2e -run . ./examples/desktop-notes/

func TestMain(m *testing.M) {
	os.Exit(desktoptest.NativeMain(m, func() (*framework.App, *desktop.Battery, error) {
		return buildApp(nil)
	}))
}

// jsStringValue decodes one JSON string result from the page.
func jsStringValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "<not a string>"
	}
	return s
}

// jsLit renders s as a JavaScript string literal.
func jsLit(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// notifyOnSave reads the owner's row through the window's own session
// (the page's fetch, not a hand-shaped request).
func settingsNotifyOnSave(t *testing.T, h *desktoptest.NativeHarness) (bool, bool) {
	t.Helper()
	resp := h.Get("/api/settings")
	if resp.Status != http.StatusOK {
		return false, false
	}
	var rows struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &rows); err != nil || len(rows.Data) != 1 {
		return false, false
	}
	v, ok := rows.Data[0]["notifyOnSave"].(bool)
	return v, ok
}

// reloadForm reloads the settings page for real. A client-side
// navigate to a visited path serves the cached DOM (the checkbox as it
// was), and this flow asserts what the server renders after a save;
// the marker proves the wait is looking at the NEW document.
func reloadForm(t *testing.T, h *desktoptest.NativeHarness) {
	t.Helper()
	_, _ = h.EvalQuiet("window.__preReload = true; location.reload(); return true")
	h.Wait("the reloaded form", func() bool {
		r, err := h.EvalQuiet(`return !window.__preReload && document.getElementById("f-notify_on_save") !== null`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	})
}

// readBox reads the real checkbox's checked state from the page.
func readBox(t *testing.T, h *desktoptest.NativeHarness) bool {
	t.Helper()
	raw := h.Eval(t, `return document.getElementById("f-notify_on_save").checked`)
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("checkbox state %s does not decode: %v", raw, err)
	}
	return b
}

// TestSettingsCheckboxSavesBothWays is the exact flow of the deleted
// browser test, now in the WKWebView: the real checkbox, the real
// Save button, the runtime's real form intercept, both saves persist,
// and with notifications back on a save reaches the shell.
func TestSettingsCheckboxSavesBothWays(t *testing.T) {
	h := desktoptest.Native(t)

	// The settings screen, navigated client-side in the real window.
	// Navigate away first: a same-path client-side navigate swaps nothing.
	h.Navigate("/")
	h.Navigate("/settings")
	h.Wait("the checkbox to render", func() bool { return h.ExistsQuiet("#f-notify_on_save") })
	if readBox(t, h) {
		t.Log("a fresh settings row renders the box checked (default true)")
	} else {
		// The row may exist from an earlier phase under the same data
		// dir; the flow below still exercises both directions.
		t.Log("the box renders unchecked (an existing row)")
	}

	// Off: uncheck, save, the row reads false.
	h.Click("#f-notify_on_save")
	h.Click(`form button[type="submit"]`)
	h.Wait("notify_on_save to persist as false", func() bool {
		v, ok := settingsNotifyOnSave(t, h)
		return ok && !v
	})

	// On again: this is the save the user could not make before the
	// browser test existed. Reload the form for real (a client-side
	// navigate serves the cached DOM), check the box, save, the row
	// reads true and the form renders it checked.
	reloadForm(t, h)
	if readBox(t, h) {
		t.Fatal("the form renders the box checked after saving false")
	}
	h.Click("#f-notify_on_save")
	h.Click(`form button[type="submit"]`)
	h.Wait("notify_on_save to persist as true", func() bool {
		v, ok := settingsNotifyOnSave(t, h)
		return ok && v
	})
	reloadForm(t, h)
	if !readBox(t, h) {
		t.Fatal("the form does not render the box checked after saving true")
	}

	// And with it on, a save notifies through the shell (unbundled, so
	// Show records into the log and answers unsupported).
	created := h.Post("/api/notes", map[string]any{"title": "After settings", "body": ""})
	if created.Status != http.StatusCreated {
		t.Fatalf("create note: %d %s", created.Status, created.Body)
	}
	found := false
	for _, n := range h.Notifications() {
		if n.Body == "After settings" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notifications after re-enabling = %+v", h.Notifications())
	}
}

// TestWidgetSubmitsAndClosesInShell is the deleted Chrome widget test
// in the real shell: the widget window is a real borderless panel, its
// page knows its own window id, the form submits through the real
// intercept, and the Close button closes the window it lives in.
func TestWidgetSubmitsAndClosesInShell(t *testing.T) {
	h := desktoptest.Native(t)

	// Open the widget through the page's own bridge so the id is the
	// one the native side assigned.
	var opened struct {
		ID string `json:"id"`
	}
	raw := h.Call("windows", "open", map[string]any{
		"path":  "/widget",
		"style": map[string]any{"chrome": "none", "panel": true, "transparent": true},
	})
	if !raw.OK {
		t.Fatalf("windows.open rejected: %s %s", raw.Code, raw.Message)
	}
	if err := json.Unmarshal(raw.Result, &opened); err != nil || opened.ID == "" {
		t.Fatalf("windows.open returned no id: %s (%v)", raw.Result, err)
	}

	h.Wait("the widget window", func() bool { return h.Window(opened.ID) != nil })
	nw := h.Window(opened.ID)
	h.Wait("the widget page", func() bool {
		r, err := nw.EvalQuiet("return location.pathname")
		return err == nil && jsStringValue(r) == "/widget"
	})
	// The page knows its own window id, and startDrag survived the
	// generated bridge.js replacing the window namespace; both need
	// the runtime, which loads with the page. wiring carries the last
	// probe for the failure message.
	var wiring string
	wiringOK := func() bool {
		r, err := nw.EvalQuiet(`return JSON.stringify({` +
			`marker: window.__gofastr_desktop ? window.__gofastr_desktop.window : null,` +
			` module: !!(window.__gofastr && window.__gofastr.desktop),` +
			` available: !!(window.__gofastr && window.__gofastr.desktop && window.__gofastr.desktop.available()),` +
			` drag: !!(window.__gofastr && window.__gofastr.desktop && window.__gofastr.desktop.window &&` +
			` typeof window.__gofastr.desktop.window.startDrag === "function")})`)
		if err != nil {
			wiring = "eval: " + err.Error()
			return false
		}
		wiring = jsStringValue(r)
		var w struct {
			Marker    *string `json:"marker"`
			Module    bool    `json:"module"`
			Available bool    `json:"available"`
			Drag      bool    `json:"drag"`
		}
		if err := json.Unmarshal([]byte(wiring), &w); err != nil {
			return false
		}
		return w.Marker != nil && *w.Marker == opened.ID && w.Module && w.Available && w.Drag
	}
	wiringDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(wiringDeadline) && !wiringOK() {
		time.Sleep(50 * time.Millisecond)
	}
	if !wiringOK() {
		t.Fatalf("the widget page never wired (marker, module, available, startDrag): %s", wiring)
	}
	t.Logf("widget wiring: %s", wiring)

	// resets.
	nw.Fill(t, "#widget-note-title", "Typed in the widget")
	nw.Submit(t, "form")
	h.Wait("the note to exist", func() bool {
		resp := h.Get("/api/notes")
		return resp.Status == http.StatusOK && strings.Contains(resp.Body, "Typed in the widget")
	})
	h.Wait("the form to reset", func() bool {
		r, err := nw.EvalQuiet(`return document.getElementById("widget-note-title").value`)
		return err == nil && jsStringValue(r) == ""
	})

	// Close drives windows.close with the page's OWN id; the window
	// goes away natively.
	nw.Click(t, "[data-notes-widget-close]")
	h.Wait("the widget window to close", func() bool {
		return h.Window(opened.ID) == nil
	})
	if ids := h.WindowIDs(); len(ids) != 1 {
		t.Fatalf("windows after the widget close = %v, want main only", ids)
	}
}
