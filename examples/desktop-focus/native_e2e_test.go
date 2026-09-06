//go:build desktop_e2e && darwin && arm64

package main

import (
	"encoding/json"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The app inside the REAL app shell: buildApp(nil) picks the darwin
// shell, NativeMain boots it once for the test binary, and every page
// step runs in the actual WKWebView (the dashboard's Start button, the
// ticked countdown, the widget's post into the main window, the
// settings checkboxes). Desktop tests never run against a browser
// stand-in.
//
// Run by hand on a Mac with a display:
//
//	CGO_ENABLED=0 go test -count=1 -tags desktop_e2e -run . ./examples/desktop-focus/
//
// FOCUS_SHOTS=<dir> also writes the main window, the widget, and the
// settings window as PNGs there, the pixels a reviewer reads.

func TestMain(m *testing.M) {
	os.Exit(desktoptest.NativeMain(m, func() (*framework.App, *desktop.Battery, error) {
		app, d, _, err := buildApp(nil)
		return app, d, err
	}))
}

func jsString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "<not a string>"
	}
	return s
}

// ensureIdle skips whatever session runs so a test starts from idle
// (the tests share one app).
func ensureIdle(t *testing.T, h *desktoptest.NativeHarness) {
	t.Helper()
	for range 4 {
		var st struct {
			State State `json:"state"`
		}
		res := h.Call("focus", "state", nil)
		if !res.OK {
			t.Fatalf("focus.state: %s %s", res.Code, res.Message)
		}
		if err := json.Unmarshal(res.Result, &st); err != nil {
			t.Fatal(err)
		}
		if st.State.Phase == phaseIdle {
			return
		}
		h.Call("focus", "skip", nil)
	}
	t.Fatal("the engine never went idle")
}

// secondaryWindow returns the one open secondary window's id.
func secondaryWindow(t *testing.T, h *desktoptest.NativeHarness) string {
	t.Helper()
	for _, id := range h.WindowIDs() {
		if id != "main" {
			return id
		}
	}
	t.Fatalf("no secondary window; open: %v", h.WindowIDs())
	return ""
}

// savePNG writes an image when FOCUS_SHOTS names a directory.
func savePNG(t *testing.T, name string, take func() []byte) {
	t.Helper()
	dir := os.Getenv("FOCUS_SHOTS")
	if dir == "" {
		return
	}
	data := take()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func encodePNG(t *testing.T, nw *desktoptest.NativeWindow) []byte {
	t.Helper()
	img := nw.Snapshot(t)
	var b strings.Builder
	if err := png.Encode(&pngWriter{&b}, img); err != nil {
		t.Fatal(err)
	}
	return []byte(b.String())
}

type pngWriter struct{ b *strings.Builder }

func (w *pngWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// reloadTo loads path for real (location.assign, not the client-side
// navigate, which serves a visited path from the screen cache) and
// waits for the runtime and the desktop module in the new document.
func reloadTo(t *testing.T, h *desktoptest.NativeHarness, path string) {
	t.Helper()
	_, _ = h.EvalQuiet("window.__preReload = true; location.assign(" + jsQuote(path) + "); return true")
	h.Wait("the reloaded page "+path, func() bool {
		r, err := h.EvalQuiet(`return !window.__preReload && location.pathname === ` + jsQuote(path) + ` && !!(window.__gofastr && window.__gofastr.desktop)`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	})
}

// visible reports whether the element the selector matches is
// rendered (display not none), the real answer to "is the button
// there", not the attribute's.
func visible(h *desktoptest.NativeHarness, selector string) bool {
	r, err := h.EvalQuiet(`const el = document.querySelector(` + jsQuote(selector) + `); return !!el && getComputedStyle(el).display !== "none"`)
	var b bool
	return err == nil && json.Unmarshal(r, &b) == nil && b
}

func nativeTask(t *testing.T, h *desktoptest.NativeHarness, title string) string {
	t.Helper()
	resp := h.Post("/api/tasks", map[string]any{"title": title, "estimate": 2})
	if resp.Status != http.StatusCreated {
		t.Fatalf("create task: %d %s", resp.Status, resp.Body)
	}
	id := between(resp.Body, `"id":"`, `"`)
	if id == "" {
		t.Fatalf("create returned no id: %s", resp.Body)
	}
	return id
}

// TestDashboardStartsTimerAndCountdownTicks: the dashboard's Start
// button drives the real bridge, the widget panel opens, the countdown
// follows the engine's ticks in the real page, and Pause flips the
// phase label.
func TestDashboardStartsTimerAndCountdownTicks(t *testing.T) {
	h := desktoptest.Native(t)
	ensureIdle(t, h)
	nativeTask(t, h, "Write the release notes")

	reloadTo(t, h, "/")
	h.Wait("the Start button", func() bool { return h.ExistsQuiet(`[data-focus-action="start"]`) })
	// Idle: only Start is rendered. The buttons carry the hidden
	// attribute server-side, and the page must honour it (a component
	// display rule used to beat the attribute).
	if !visible(h, `[data-focus-action="start"]`) || visible(h, `[data-focus-action="pause"]`) ||
		visible(h, `[data-focus-action="resume"]`) || visible(h, `[data-focus-action="skip"]`) {
		t.Fatal("idle: want Start visible and Pause, Resume, Skip hidden")
	}
	savePNG(t, "focus-main-idle.png", func() []byte { return encodePNG(t, h.Window("main")) })

	h.Click(`[data-focus-action="start"]`)
	h.Wait("the session row", func() bool {
		r := h.Get("/api/sessions")
		return r.Status == http.StatusOK && strings.Contains(r.Body, `"kind":"work"`)
	})
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })
	widgetID := secondaryWindow(t, h)
	state, err := h.WindowState(widgetID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Class != "NSPanel" || state.StyleMask&1 != 0 || state.Key || state.ContentClass != "WKWebView" {
		t.Fatalf("widget window state = %+v, want a borderless NSPanel that is not key and holds the web view", state)
	}
	if mainState, err := h.WindowState("main"); err != nil || mainState.ContentClass != "WKWebView" {
		t.Fatalf("main window state = %+v (%v), want the web view as its content", mainState, err)
	}

	h.Wait("the countdown to tick", func() bool {
		s := h.TextQuiet(`[data-focus-countdown]`)
		return len(s) == 5 && s[2] == ':' && s != "25:00"
	})
	wid := h.Window(widgetID)
	h.Wait("the widget's countdown", func() bool {
		r, err := wid.EvalQuiet(`const el = document.querySelector("[data-focus-countdown]"); return el ? el.textContent : ""`)
		s := jsString(r)
		return err == nil && len(s) == 5 && s[2] == ':'
	})
	h.Wait("the running buttons", func() bool {
		return !visible(h, `[data-focus-action="start"]`) && visible(h, `[data-focus-action="pause"]`) &&
			!visible(h, `[data-focus-action="resume"]`) && visible(h, `[data-focus-action="skip"]`)
	})
	savePNG(t, "focus-main-running.png", func() []byte { return encodePNG(t, h.Window("main")) })
	savePNG(t, "focus-widget.png", func() []byte { return encodePNG(t, wid) })

	h.Click(`[data-focus-action="pause"]`)
	h.WaitText(`[data-focus-phase]`, "paused")
	h.Wait("the paused buttons", func() bool {
		return visible(h, `[data-focus-action="resume"]`) && !visible(h, `[data-focus-action="pause"]`)
	})

	ensureIdle(t, h)
	h.CloseWindow(widgetID)
	h.Wait("the widget window to close", func() bool { return len(h.WindowIDs()) == 1 })
}

// TestWidgetOpenTaskPostReachesMain: the widget's Open task button
// posts show_task through the bridge to the main window, whose
// listener navigates to the task.
func TestWidgetOpenTaskPostReachesMain(t *testing.T) {
	h := desktoptest.Native(t)
	ensureIdle(t, h)
	taskID := nativeTask(t, h, "Widget target")
	reloadTo(t, h, "/")

	res := h.Call("focus", "start", map[string]any{"taskId": taskID})
	if !res.OK {
		t.Fatalf("focus.start: %s %s", res.Code, res.Message)
	}
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })
	widgetID := secondaryWindow(t, h)
	wid := h.Window(widgetID)
	h.Wait("the widget's open-task button", func() bool {
		r, err := wid.EvalQuiet(`const el = document.querySelector("[data-focus-open]"); return el ? el.getAttribute("data-focus-open") : ""`)
		return err == nil && jsString(r) == taskID
	})
	wid.Click(t, `[data-focus-open]`)
	h.WaitLocation("/tasks/" + taskID)

	ensureIdle(t, h)
	h.CloseWindow(widgetID)
	h.Wait("the widget window to close", func() bool { return len(h.WindowIDs()) == 1 })
}

// TestSettingsWindowSavesBothCheckboxes: the settings WINDOW (the app
// menu's item), the battery's preferences form in the real WebView:
// flip both checkboxes, change work minutes, Save, reload, and the
// form shows what d.Preferences() answers.
func TestSettingsWindowSavesBothCheckboxes(t *testing.T) {
	h := desktoptest.Native(t)
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return h.Window("settings") != nil })
	sw := h.Window("settings")
	// The form AND the runtime: a click on Save before the intercept
	// attached would be a plain browser POST, not the app's save.
	formReady := func() bool {
		r, err := sw.EvalQuiet(`return !!document.getElementById("f-notify_on_done") && !!document.getElementById("f-tray_countdown") && !!document.getElementById("f-work_minutes") && !!(window.__gofastr && window.__gofastr.desktop)`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	}
	h.Wait("the preferences form", formReady)
	savePNG(t, "focus-settings.png", func() []byte { return encodePNG(t, sw) })

	checked := func(id string) bool {
		r, err := sw.EvalQuiet(`return document.getElementById(` + jsQuote(id) + `).checked`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	}
	setWorkMinutes := func(n int) {
		r, err := sw.EvalQuiet(`var el = document.getElementById("f-work_minutes"); el.value = ` + jsQuote(strconv.Itoa(n)) + `; el.dispatchEvent(new Event("input", {bubbles: true})); return el.value`)
		var got string
		if err != nil || json.Unmarshal(r, &got) != nil || got != strconv.Itoa(n) {
			t.Fatalf("set work minutes to %d: %v %s", n, err, r)
		}
	}
	reloadForm := func() {
		_, _ = sw.EvalQuiet("window.__preReload = true; location.reload(); return true")
		h.Wait("the reloaded form", func() bool {
			r, err := sw.EvalQuiet(`return !window.__preReload`)
			var b bool
			return err == nil && json.Unmarshal(r, &b) == nil && b && formReady()
		})
	}
	readWorkMinutes := func() string {
		r, err := sw.EvalQuiet(`return document.getElementById("f-work_minutes").value`)
		var got string
		if err != nil || json.Unmarshal(r, &got) != nil {
			return ""
		}
		return got
	}

	// Whatever the preferences hold now, drive both boxes to off and
	// the minutes to 45, save: the values persist exactly.
	setBoth := func(want bool, minutes int) {
		for _, id := range []string{"f-notify_on_done", "f-tray_countdown"} {
			if checked(id) != want {
				sw.Click(t, "#"+id)
			}
		}
		setWorkMinutes(minutes)
		sw.Click(t, `form button[type="submit"]`)
		wantState := map[bool]string{true: "on", false: "off"}[want]
		h.Wait("the preferences to persist as "+wantState+" / "+strconv.Itoa(minutes), func() bool {
			p := h.Battery.Preferences()
			return p.Bool("notify_on_done") == want && p.Bool("tray_countdown") == want && p.Int("work_minutes") == minutes
		})
		// The post-save landing is /settings again: reload it so the
		// form reflects the stored values, not the cached DOM.
		reloadForm()
		if checked("f-notify_on_done") != want || checked("f-tray_countdown") != want {
			t.Fatalf("after saving %v the form renders notify=%v tray=%v", want, checked("f-notify_on_done"), checked("f-tray_countdown"))
		}
		if got := readWorkMinutes(); got != strconv.Itoa(minutes) {
			t.Fatalf("after saving %d the form renders work minutes %q", minutes, got)
		}
	}
	setBoth(false, 45)
	setBoth(true, 25)
	h.CloseWindow("settings")
	h.Wait("the settings window to close", func() bool { return h.Window("settings") == nil })
}

// TestSettingsRefusedSaveShowsFieldError: the failure half of the
// settings form in the real WebView. The number input's native min/max
// already block a plain out-of-range number before any submit, but
// "1e2" passes native validation (it is the number 100, in range)
// while the route refuses it as not a whole number: that is the
// refusal a real user can still produce, and the runtime's formerrors
// module must paint the route's envelope into the field the user is
// looking at (a role=alert paragraph beside the input, aria-invalid on
// it, nothing applied). Fixing the value and saving again clears the
// error. If the envelope's field key ever stopped matching the form
// control name, this is the test that fails.
func TestSettingsRefusedSaveShowsFieldError(t *testing.T) {
	h := desktoptest.Native(t)
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return h.Window("settings") != nil })
	sw := h.Window("settings")
	h.Wait("the preferences form", func() bool {
		r, err := sw.EvalQuiet(`return !!document.getElementById("f-work_minutes") && !!(window.__gofastr && window.__gofastr.desktop)`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	})

	setWork := func(n string) {
		r, err := sw.EvalQuiet(`var el = document.getElementById("f-work_minutes"); el.value = ` + jsQuote(n) + `; el.dispatchEvent(new Event("input", {bubbles: true})); return el.value`)
		var got string
		if err != nil || json.Unmarshal(r, &got) != nil || got != n {
			t.Fatalf("set work minutes to %s: %v %s", n, err, r)
		}
	}

	// The refused save: "1e2" is native-valid, not a whole number to
	// the route.
	before := h.Battery.Preferences().Int("work_minutes")
	setWork("1e2")
	sw.Click(t, `form button[type="submit"]`)
	h.Wait("the field error to render", func() bool {
		r, err := sw.EvalQuiet(`var p = document.getElementById("f-work_minutes-error");
			return !!p && p.getAttribute("role") === "alert" && p.textContent.indexOf("whole number") !== -1
				&& p.offsetParent !== null
				&& document.getElementById("f-work_minutes").getAttribute("aria-invalid") === "true"`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	})
	if got := h.Battery.Preferences().Int("work_minutes"); got != before {
		t.Fatalf("a refused save applied work_minutes %d, want the stored %d", got, before)
	}

	// The retry: a valid value saves and the error is gone.
	setWork("30")
	sw.Click(t, `form button[type="submit"]`)
	h.Wait("work_minutes to persist as 30", func() bool {
		return h.Battery.Preferences().Int("work_minutes") == 30
	})
	h.Wait("the field error to clear", func() bool {
		r, err := sw.EvalQuiet(`return !document.getElementById("f-work_minutes-error") && document.getElementById("f-work_minutes").getAttribute("aria-invalid") !== "true"`)
		var b bool
		return err == nil && json.Unmarshal(r, &b) == nil && b
	})
	h.CloseWindow("settings")
	h.Wait("the settings window to close", func() bool { return h.Window("settings") == nil })
}

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestStartFromTaskPageAndRow: the two places a user meets Start
// after saving a task (the task page it lands on, and the dashboard
// row) both start a session and open the widget, in the real shell.
func TestStartFromTaskPageAndRow(t *testing.T) {
	h := desktoptest.Native(t)
	ensureIdle(t, h)
	taskID := nativeTask(t, h, "Start from the task page")

	reloadTo(t, h, "/tasks/"+taskID)
	h.Wait("the task page's Start", func() bool { return visible(h, `[data-focus-start="`+taskID+`"]`) })
	h.Click(`[data-focus-start="` + taskID + `"]`)
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })
	h.Wait("the session on the task", func() bool {
		r := h.Get("/api/sessions")
		return r.Status == http.StatusOK && strings.Contains(r.Body, `"taskId":"`+taskID+`"`)
	})
	ensureIdle(t, h)
	h.CloseWindow(secondaryWindow(t, h))
	h.Wait("the widget window to close", func() bool { return len(h.WindowIDs()) == 1 })

	reloadTo(t, h, "/")
	h.Wait("the row's Start", func() bool { return visible(h, `[data-focus-start="`+taskID+`"]`) })
	h.Click(`[data-focus-start="` + taskID + `"]`)
	h.Wait("the widget window again", func() bool { return len(h.WindowIDs()) == 2 })
	h.WaitText(`[data-focus-task]`, "Start from the task page")
	savePNG(t, "focus-main-task-running.png", func() []byte { return encodePNG(t, h.Window("main")) })
	ensureIdle(t, h)
	h.CloseWindow(secondaryWindow(t, h))
	h.Wait("the widget window to close", func() bool { return len(h.WindowIDs()) == 1 })
}

// TestNewTaskFormLandsOnTheTimer is the user's first flow: New task,
// the real form, Save. The page it lands on must carry the timer (the
// Now card and the row's Start), and Start there opens the widget.
func TestNewTaskFormLandsOnTheTimer(t *testing.T) {
	h := desktoptest.Native(t)
	ensureIdle(t, h)

	reloadTo(t, h, "/tasks/new")
	h.Wait("the new-task form", func() bool { return h.ExistsQuiet("#f-title") })
	// Only the title, the way a user does it: the estimate stays blank
	// and takes its default.
	h.Fill("#f-title", "Typed in the form")
	h.Submit("form")
	// Where the real Save lands, with the page's own words when it
	// does not: the failure message names the path and the visible
	// text so the next reader sees what the user saw.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && h.Location() != "/tasks" {
		time.Sleep(100 * time.Millisecond)
	}
	if loc := h.Location(); loc != "/tasks" {
		body := h.TextQuiet("body")
		if len(body) > 400 {
			body = body[:400]
		}
		t.Fatalf("after Save the page is at %q, want /tasks; page text: %q", loc, body)
	}
	h.Wait("the timer on the landing page", func() bool {
		return visible(h, `[data-focus-action="start"]`) && h.ExistsQuiet(`[data-focus-start]`)
	})
	if !strings.Contains(h.Text("body"), "Typed in the form") {
		t.Fatal("the landing page does not list the task just saved")
	}
	h.Click(`[data-focus-start]`)
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })
	h.WaitText(`[data-focus-task]`, "Typed in the form")
	savePNG(t, "focus-after-new-task.png", func() []byte { return encodePNG(t, h.Window("main")) })

	ensureIdle(t, h)
	h.CloseWindow(secondaryWindow(t, h))
	h.Wait("the widget window to close", func() bool { return len(h.WindowIDs()) == 1 })
}

// TestRememberedWindowFrameAndPath: the page reports its path on
// navigation (the desktop runtime module's setPath) and a real window
// move lands in state.json's windows entry under the data dir, the
// state a relaunch restores from.
func TestRememberedWindowFrameAndPath(t *testing.T) {
	h := desktoptest.Native(t)
	ensureIdle(t, h)

	// A client-side navigation: the runtime module reports the path
	// through the bridge after the swap.
	h.Navigate("/tasks")
	h.WaitLocation("/tasks")

	frame := desktop.Frame{X: 90, Y: 120, Width: 950, Height: 700}
	h.MoveWindow("main", frame)
	f, err := h.WindowFrame("main")
	if err != nil {
		t.Fatalf("WindowFrame: %v", err)
	}
	if f != frame {
		t.Fatalf("the window's frame = %+v, want %+v", f, frame)
	}

	dir, err := desktop.DataDir(appID)
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	h.Wait("state.json to hold the frame and the path", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "state.json"))
		if err != nil {
			return false
		}
		return strings.Contains(string(data), `"X": 90`) &&
			strings.Contains(string(data), `"Width": 950`) &&
			strings.Contains(string(data), `"path": "/tasks"`)
	})
	fi, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("state.json mode = %v, want 0600", fi.Mode().Perm())
	}
	t.Logf("state.json at %s holds frame %+v and path /tasks", dir, frame)
}
