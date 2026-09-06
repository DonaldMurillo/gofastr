package desktop_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// Cross-window events and messages: Emit reaches every open window,
// EmitTo targets one, and the windows.post/broadcast/self methods
// carry pages' messages through the server (never window to window).
// Every test runs the real Run flow through the harness.

// openPath opens a secondary window on path through the page's own API
// and returns its id.
func openPath(t *testing.T, h *desktoptest.Harness, path string) string {
	t.Helper()
	var opened struct {
		ID string `json:"id"`
	}
	h.Call("windows", "open", map[string]any{"path": path}).MustResult(t, &opened)
	if opened.ID == "" {
		t.Fatalf("windows.open(%q) returned no id", path)
	}
	return opened.ID
}

// eventsNamed filters a window's events by name.
func eventsNamed(evs []desktoptest.Event, name string) []desktoptest.Event {
	var out []desktoptest.Event
	for _, ev := range evs {
		if ev.Name == name {
			out = append(out, ev)
		}
	}
	return out
}

func TestEmitReachesEveryOpenWindow(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")
	w3 := openPath(t, h, "/settings")

	if err := d.Emit("ping_all", map[string]any{"n": 1}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	ev := h.WaitEvent("ping_all")
	if string(ev.Payload) != `{"n":1}` {
		t.Fatalf("main window payload = %s", ev.Payload)
	}
	for _, id := range []string{w2, w3} {
		got := eventsNamed(h.Window(id).Events(), "ping_all")
		if len(got) != 1 || string(got[0].Payload) != `{"n":1}` {
			t.Fatalf("window %s events = %+v, want one ping_all with the payload", id, got)
		}
	}
}

func TestEmitToDeliversToOneWindow(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	if err := d.EmitTo(w2, "ping_one", map[string]any{"n": 2}); err != nil {
		t.Fatalf("EmitTo: %v", err)
	}
	got := eventsNamed(h.Window(w2).Events(), "ping_one")
	if len(got) != 1 || string(got[0].Payload) != `{"n":2}` {
		t.Fatalf("w2 events = %+v, want one ping_one with the payload", got)
	}
	if got := eventsNamed(h.Events(), "ping_one"); len(got) != 0 {
		t.Fatalf("EmitTo leaked to the main window: %+v", got)
	}
}

func TestEmitToUnknownWindowIsNotFound(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")
	h.CloseWindow(w2)

	var de *desktop.Error
	if err := d.EmitTo("nope", "evt", nil); !errors.As(err, &de) || de.Code != desktop.CodeNotFound {
		t.Fatalf("EmitTo(unknown) = %v, want not_found", err)
	}
	if err := d.EmitTo(w2, "evt", nil); !errors.As(err, &de) || de.Code != desktop.CodeNotFound {
		t.Fatalf("EmitTo(closed %s) = %v, want not_found", w2, err)
	}
}

func TestWindowsPostDeliversToTarget(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	h.Call("windows", "post", map[string]any{
		"to": w2, "name": "user_msg", "payload": map[string]any{"text": "hi"},
	}).AssertOK(t)
	got := eventsNamed(h.Window(w2).Events(), "user_msg")
	if len(got) != 1 || string(got[0].Payload) != `{"text":"hi"}` {
		t.Fatalf("w2 events = %+v, want one user_msg with the payload", got)
	}
	if got := eventsNamed(h.Events(), "user_msg"); len(got) != 0 {
		t.Fatalf("post leaked to the sender's window: %+v", got)
	}

	// A target that is not an open window is not_found.
	h.Call("windows", "post", map[string]any{"to": "zz", "name": "user_msg"}).
		AssertCode(t, desktop.CodeNotFound)
}

func TestWindowsBroadcastExcludesCaller(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")
	w3 := openPath(t, h, "/settings")

	var out struct {
		Delivered int `json:"delivered"`
	}
	// From main: w2 and w3 hear it, main does not.
	h.Call("windows", "broadcast", map[string]any{
		"name": "bcast", "payload": map[string]any{"x": 1},
	}).MustResult(t, &out)
	if out.Delivered != 2 {
		t.Fatalf("broadcast from main delivered %d, want 2", out.Delivered)
	}
	if got := eventsNamed(h.Events(), "bcast"); len(got) != 0 {
		t.Fatalf("the caller's own window heard the broadcast: %+v", got)
	}
	for _, id := range []string{w2, w3} {
		if got := eventsNamed(h.Window(id).Events(), "bcast"); len(got) != 1 {
			t.Fatalf("window %s broadcast events = %+v, want one", id, got)
		}
	}

	// From w2: main and w3 hear it, w2 does not.
	h.CallFrom(w2, "windows", "broadcast", map[string]any{"name": "bcast2"}).MustResult(t, &out)
	if out.Delivered != 2 {
		t.Fatalf("broadcast from w2 delivered %d, want 2", out.Delivered)
	}
	if got := eventsNamed(h.Window(w2).Events(), "bcast2"); len(got) != 0 {
		t.Fatalf("the caller's own window heard its broadcast: %+v", got)
	}
	for _, id := range []string{"main", w3} {
		w := h.Window(id)
		if got := eventsNamed(w.Events(), "bcast2"); len(got) != 1 {
			t.Fatalf("window %s broadcast2 events = %+v, want one", id, got)
		}
	}
}

func TestWindowsSelfAnswersCallersWindow(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	var self struct {
		ID string `json:"id"`
	}
	// The harness's Call plays the main window's page.
	h.Call("windows", "self", nil).MustResult(t, &self)
	if self.ID != "main" {
		t.Fatalf("self from main = %q", self.ID)
	}
	// A second window's page claims its own id, and the claim is
	// answered as reported (it is a claim, not an identity).
	h.CallFrom(w2, "windows", "self", nil).MustResult(t, &self)
	if self.ID != w2 {
		t.Fatalf("self from %s = %q", w2, self.ID)
	}
	h.CallFrom("w9", "windows", "self", nil).MustResult(t, &self)
	if self.ID != "w9" {
		t.Fatalf("self from an unopened but grammatical id = %q, want the claim", self.ID)
	}

	// No header at all means main.
	req, err := http.NewRequest(http.MethodPost, h.URL("/__gofastr/desktop/call/windows/self"), strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp := h.Do(req).AssertStatus(t, http.StatusOK)
	env := struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}{}
	if err := resp.JSON(&env); err != nil {
		t.Fatal(err)
	}
	if env.Result.ID != "main" {
		t.Fatalf("self with no window header = %q, want main", env.Result.ID)
	}

	// A header off the window id grammar reads as main, never an error.
	h.CallFrom("Not A Window!!", "windows", "self", nil).MustResult(t, &self)
	if self.ID != "main" {
		t.Fatalf("self with an off-grammar header = %q, want main", self.ID)
	}
}

func TestWindowsPostRejectsBadEventNames(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	for _, bad := range []string{"", "BadName", "a.b", "9x", "with space", strings.Repeat("n", 65)} {
		h.Call("windows", "post", map[string]any{"to": w2, "name": bad}).
			AssertCode(t, desktop.CodeInvalidInput)
		h.Call("windows", "broadcast", map[string]any{"name": bad}).
			AssertCode(t, desktop.CodeInvalidInput)
	}
	if evs := h.Window(w2).Events(); len(evs) != 0 {
		t.Fatalf("a rejected name still delivered: %+v", evs)
	}
	if evs := h.Events(); len(evs) != 0 {
		t.Fatalf("a rejected name still delivered to main: %+v", evs)
	}
	// An underscore name is the one legal spelling; dots are not.
	h.Call("windows", "post", map[string]any{"to": "main", "name": "under_scored"}).AssertOK(t)
	h.WaitEvent("under_scored")
}

func TestWindowsPostCapsPayloadSize(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	// The cap is on the COMPACT JSON of the payload: {"t":"aaa..."} is
	// 8 bytes of wrapper plus the string.
	atCap := strings.Repeat("a", (64<<10)-len(`{"t":""}`))
	h.Call("windows", "post", map[string]any{
		"to": "main", "name": "at_cap", "payload": map[string]any{"t": atCap},
	}).AssertOK(t)
	h.WaitEvent("at_cap")

	overCap := strings.Repeat("a", (64<<10)-len(`{"t":""}`)+1)
	h.Call("windows", "post", map[string]any{
		"to": w2, "name": "over_cap", "payload": map[string]any{"t": overCap},
	}).AssertCode(t, desktop.CodeInvalidInput)
	h.Call("windows", "broadcast", map[string]any{
		"name": "over_cap", "payload": map[string]any{"t": overCap},
	}).AssertCode(t, desktop.CodeInvalidInput)
	if got := eventsNamed(h.Window(w2).Events(), "over_cap"); len(got) != 0 {
		t.Fatal("an over-cap payload was delivered to w2")
	}
}

func TestPostToWindowClosingMidEvalIsNotFound(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "X"})
	h := desktoptest.Run(t, app, d)
	w2 := openPath(t, h, "/two")

	// The window closes between the bridge's lookup and the eval: the
	// user clicked the close button as the delivery landed. The race
	// must read as not_found, not internal.
	h.Window(w2).SetEvalHook(func(js string) error {
		h.CloseWindow(w2)
		return errors.New("the web view is gone")
	})
	h.Call("windows", "post", map[string]any{"to": w2, "name": "ping"}).
		AssertCode(t, desktop.CodeNotFound)
}
