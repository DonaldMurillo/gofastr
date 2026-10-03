package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The app as the window runs it: desktoptest.Run drives the battery's
// real Run flow (listener, boot handshake, frozen registry, menus,
// tray) with the fake shell as the OS. Every request carries the
// window's session and therefore the local identity, the way the
// WebView's do; engine_test.go covers the same engine with an injected
// clock, and main_test.go the --serve shape.

func newHarness(t *testing.T) (*desktoptest.Harness, *Engine) {
	t.Helper()
	// buildApp opens the data dir before Run, so the override must be
	// in place first (Run would only set it when unset).
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	shell := desktoptest.NewShell()
	app, d, eng, err := buildApp(shell)
	if err != nil {
		t.Fatal(err)
	}
	return desktoptest.Run(t, app, d), eng
}

// saveHarnessSettings sets the app's preferences through the page
// capability, the way the settings screen's script would.
func saveHarnessSettings(t *testing.T, h *desktoptest.Harness, work, breakMin int, notify, trayCountdown bool) {
	t.Helper()
	h.Call("preferences", "set", map[string]any{"values": map[string]any{
		"work_minutes": work, "break_minutes": breakMin,
		"notify_on_done": notify, "tray_countdown": trayCountdown,
	}}).AssertOK(t)
}

// between slices s between the first after and the next stop.
func between(s, after, stop string) string {
	i := strings.Index(s, after)
	if i < 0 {
		return ""
	}
	rest := s[i+len(after):]
	j := strings.Index(rest, stop)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func TestStartThroughBridgeOpensWidgetAndTicks(t *testing.T) {
	h, eng := newHarness(t)

	task := h.Post("/api/tasks", map[string]any{"title": "Bridge start"}).AssertStatus(t, http.StatusCreated)
	taskID := between(task.Body, `"id":"`, `"`)

	// focus.start through the real chokepoint: a session appears and
	// the floating timer widget opens with the widget style.
	var started struct {
		State State `json:"state"`
	}
	h.Call("focus", "start", map[string]any{"taskId": taskID}).MustResult(t, &started)
	if started.State.Phase != phaseWork || started.State.TaskTitle != "Bridge start" {
		t.Fatalf("focus.start state = %+v", started.State)
	}
	h.Wait("the widget window", func() bool { return len(h.WindowIDs()) == 2 })
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 || calls[0].ID != "w2" || calls[0].Spec.Path != "/widget" {
		t.Fatalf("OpenWindow calls = %+v", calls)
	}
	spec := calls[0].Spec
	if spec.Width != 320 || spec.Height != 300 {
		t.Fatalf("widget size = %dx%d, want 320x300", spec.Width, spec.Height)
	}
	if spec.Style.Chrome != desktop.ChromeNone || !spec.Style.Panel || !spec.Style.Transparent {
		t.Fatalf("widget style = %+v", spec.Style)
	}
	if !spec.Style.AllSpaces {
		t.Fatal("the timer widget must be visible on every Space")
	}

	// One tick: the tray shows the countdown and every window hears
	// focus_tick. The once-a-second loop may have ticked first, so any
	// mm:ss within the first minute of the session is the proof.
	eng.Tick(localUserCtx(t.Context(), h.Battery))
	h.Wait("the tray countdown", func() bool {
		return trayTitleIsMinute(h.Shell.TrayTitle(), 24, 25)
	})
	h.Wait("focus_tick in the main window", func() bool {
		return windowHasEvent(h, "main", "focus_tick")
	})
	if !windowHasEvent(h, "w2", "focus_tick") {
		t.Fatal("the widget window did not receive focus_tick")
	}
}

func windowHasEvent(h *desktoptest.Harness, id, name string) bool {
	for _, ev := range h.Window(id).Events() {
		if ev.Name == name {
			return true
		}
	}
	return false
}

func TestSessionEndNotifiesAndEmitsFocusDone(t *testing.T) {
	h, eng := newHarness(t)
	saveHarnessSettings(t, h, 1, 1, true, true)

	h.Call("focus", "start", nil).AssertOK(t)
	// Freeze the clock past the one-minute session: the loop's own
	// ticks see the same frozen instant, so the completion is
	// deterministic whichever tick lands it.
	base := time.Now()
	eng.now = func() time.Time { return base.Add(2 * time.Minute) }
	eng.Tick(localUserCtx(t.Context(), h.Battery))

	ev := h.WaitEvent("focus_done")
	var done struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := ev.Unmarshal(&done); err != nil || done.Kind != "work" || done.Message != "Work session done. Break for 1 minutes." {
		t.Fatalf("focus_done payload = %s (%v)", ev.Payload, err)
	}
	notes := h.Shell.Notifications()
	if len(notes) != 1 || notes[0].Title != "Focus" || notes[0].Body != done.Message {
		t.Fatalf("notifications = %+v", notes)
	}
	// The break auto-started (it ends at base+3m); the tray counts it
	// down and the frozen clock holds it at 01:00.
	h.Wait("the break countdown", func() bool { return h.Shell.TrayTitle() == "01:00" })

	// The break's end goes idle, notifies "back to work", and puts the
	// tray title back to the app name.
	eng.now = func() time.Time { return base.Add(5 * time.Minute) }
	eng.Tick(localUserCtx(t.Context(), h.Battery))
	ev = h.WaitEvent("focus_done")
	if err := ev.Unmarshal(&done); err != nil || done.Kind != "break" || done.Message != "Break over. Back to work." {
		t.Fatalf("break focus_done = %s (%v)", ev.Payload, err)
	}
	notes = h.Shell.Notifications()
	if len(notes) != 2 || notes[1].Body != "Break over. Back to work." {
		t.Fatalf("notifications after the break = %+v", notes)
	}
	if got := h.Shell.TrayTitle(); got != "Focus" {
		t.Fatalf("tray title after idle = %q, want Focus", got)
	}
}

func TestNotifyOffSilencesTheNotification(t *testing.T) {
	h, eng := newHarness(t)
	saveHarnessSettings(t, h, 1, 5, false, true)

	h.Call("focus", "start", nil).AssertOK(t)
	base := time.Now()
	eng.now = func() time.Time { return base.Add(2 * time.Minute) }
	eng.Tick(localUserCtx(t.Context(), h.Battery))

	h.WaitEvent("focus_done") // the page still hears it
	if n := len(h.Shell.Notifications()); n != 0 {
		t.Fatalf("notifications with notify off = %d", n)
	}
}

// TestSettingsMinutesFlowIntoSessions: preferences changed through the
// page capability drive the engine's next session (work and break
// minutes), the coverage the old settings-row tests carried.
func TestSettingsMinutesFlowIntoSessions(t *testing.T) {
	h, _ := newHarness(t)
	saveHarnessSettings(t, h, 30, 7, true, true)

	h.Call("focus", "start", nil).AssertOK(t)
	body := h.Get("/api/sessions").AssertStatus(t, http.StatusOK).Body
	if !strings.Contains(body, `"minutes":30`) {
		t.Fatalf("work session minutes after preferences.set: %s", body)
	}
}

func TestTrayCountdownOffLeavesTrayTitleAlone(t *testing.T) {
	h, eng := newHarness(t)
	saveHarnessSettings(t, h, 25, 5, true, false)

	h.Call("focus", "start", nil).AssertOK(t)
	eng.Tick(localUserCtx(t.Context(), h.Battery))
	h.WaitEvent("focus_tick")

	if got := h.Shell.TrayTitle(); got != "Focus" {
		t.Fatalf("tray title with tray_countdown off = %q, want Focus", got)
	}
	// The page still ticks.
	if !windowHasEvent(h, "main", "focus_tick") {
		t.Fatal("focus_tick missing with tray_countdown off")
	}
}

func TestDeepLinkStartsTheTask(t *testing.T) {
	h, eng := newHarness(t)
	task := h.Post("/api/tasks", map[string]any{"title": "Linked task"}).AssertStatus(t, http.StatusCreated)
	taskID := between(task.Body, `"id":"`, `"`)

	h.OpenURL("gofastr-focus://start?task=" + taskID)
	h.Wait("the session", func() bool { return eng.State(localUserCtx(t.Context(), h.Battery)).Phase == phaseWork })
	h.Wait("the main window navigates to /", func() bool {
		for _, p := range h.Navigations() {
			if p == "/" {
				return true
			}
		}
		return false
	})
	st := eng.State(localUserCtx(t.Context(), h.Battery))
	if st.TaskID != taskID || st.TaskTitle != "Linked task" {
		t.Fatalf("deep-link state = %+v", st)
	}
	// A deep link on the default mapping navigates without starting
	// anything: /history is a screen.
	h.OpenURL("gofastr-focus://history")
	h.Wait("the default mapping", func() bool {
		for _, p := range h.Navigations() {
			if p == "/history" {
				return true
			}
		}
		return false
	})
}

func TestSettingsWindowAndStrangerAndManifest(t *testing.T) {
	h, _ := newHarness(t)

	// The settings window from the app menu's own item, the File menu
	// row, and the tray row.
	h.OpenSettings()
	h.Wait("the settings window", func() bool { return len(h.WindowIDs()) == 2 })
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 || calls[0].ID != "settings" || calls[0].Spec.Path != "/settings" {
		t.Fatalf("OpenWindow calls = %+v", calls)
	}
	if spec := calls[0].Spec; spec.Title != "Settings" || spec.Width != 480 || spec.Height != 600 {
		t.Fatalf("settings spec = %+v", spec)
	}
	h.ClickMenu("File", "Settings…")
	h.ClickTray("Settings…")
	if got := h.Window("settings").Focuses(); got != 2 {
		t.Fatalf("settings focuses = %d, want 2", got)
	}

	// The File and View menu items do what they declare.
	h.ClickMenu("File", "New task")
	h.PressKey("cmd+n")
	h.Wait("two new-task navigations", func() bool {
		n := 0
		for _, p := range h.Navigations() {
			if p == "/tasks/new" {
				n++
			}
		}
		return n == 2
	})
	h.ClickMenu("View", "History")
	h.Wait("the history navigation", func() bool {
		for _, p := range h.Navigations() {
			if p == "/history" {
				return true
			}
		}
		return false
	})
	h.ClickMenu("View", "Timer widget")
	h.Wait("the widget from the View menu", func() bool { return len(h.Shell.OpenWindowCalls()) == 2 })
	// The accelerator focuses the already-open widget (OpenWindow is
	// idempotent per path), it does not stack a second one.
	h.PressKey("cmd+t")
	h.Wait("the cmd+t focus", func() bool { return h.Window("w2").Focuses() == 1 })
	if n := len(h.Shell.OpenWindowCalls()); n != 2 {
		t.Fatalf("OpenWindow calls after the accelerator = %d, want 2 (focus)", n)
	}

	// The tray's Start / Pause toggles a session.
	// The tray row's Handler runs on a goroutine: wait for the row the
	// toggle creates, not for a list body (which is never empty, so a
	// wait on it passed before the session existed and the assertion
	// below raced it under load).
	h.ClickTray("Start / Pause")
	h.Wait("a running session", func() bool {
		return strings.Contains(h.Get("/api/sessions").AssertStatus(t, http.StatusOK).Body, `"completed":false`)
	})
	// Same shape for the pause: every sessions row carries a pausedLeft
	// column, so a wait on that key was true before the toggle ran and
	// the phase assertion below raced the handler's goroutine. Wait on
	// the engine's own answer.
	h.ClickTray("Start / Pause")
	h.Wait("the paused session", func() bool {
		var st struct {
			State State `json:"state"`
		}
		res := h.Call("focus", "state", nil)
		return res.OK && json.Unmarshal(res.Result, &st) == nil && st.State.Phase == phasePaused
	})

	// The plugin capability is in the frozen manifest and callable.
	var found bool
	for _, c := range h.Manifest().Capabilities {
		found = found || c.Name == "focus"
	}
	if !found {
		t.Fatal("manifest lacks the focus plugin capability")
	}
	var state struct {
		State State `json:"state"`
	}
	h.Call("focus", "state", nil).MustResult(t, &state)
	if state.State.Phase != phasePaused {
		t.Fatalf("focus.state = %+v, want paused", state.State)
	}

	// Another local process on the port sees nothing.
	req, _ := http.NewRequest(http.MethodGet, h.URL("/api/tasks"), nil)
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger GET /api/tasks = %d, want 403", resp.StatusCode)
	}
}

// trayTitleIsMinute reports whether the tray title reads mm:ss with
// the minute part between lo and hi.
func trayTitleIsMinute(title string, lo, hi int) bool {
	if len(title) != 5 || title[2] != ':' {
		return false
	}
	minutes := int(title[0]-'0')*10 + int(title[1]-'0')
	return minutes >= lo && minutes <= hi
}

// The dashboard's task table island refreshes through the resource
// engine's table handler mounted at /api/tables/tasks: the window's
// session reaches it and a stranger on the port does not.
func TestTaskTableIslandServesTheWindow(t *testing.T) {
	h, _ := newHarness(t)
	h.Post("/api/tasks", map[string]any{"title": "Table row"}).AssertStatus(t, http.StatusCreated)
	h.Get("/api/tables/tasks").AssertStatus(t, http.StatusOK).AssertContains(t, "Table row")

	req, _ := http.NewRequest(http.MethodGet, h.URL("/api/tables/tasks"), nil)
	resp, err := h.Stranger().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger GET /api/tables/tasks = %d, want 403", resp.StatusCode)
	}
}
