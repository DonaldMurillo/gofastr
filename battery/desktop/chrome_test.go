package desktop_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// The phase 13 chrome contract against the fake shell through the real
// Run flow: the new WindowStyle/Config fields round-trip to the shell,
// chromeApp runs the real Run flow with the chrome fields under test.
func chromeApp(t *testing.T, cfg desktop.Config) (*desktoptest.Harness, *desktop.Battery) {
	t.Helper()
	app, d := uiApp(t, cfg)
	h := desktoptest.Run(t, app, d)
	return h, d
}

func TestChromeConfigRoundTrip(t *testing.T) {
	h, d := chromeApp(t, desktop.Config{
		Title: "Chrome",
		Style: desktop.WindowStyle{
			Material:          desktop.MaterialSidebar,
			Chrome:            desktop.ChromeUnified,
			TrafficLightInset: &desktop.Inset{X: 12, Y: 8},
		},
		SidebarWidth: 220,
	})
	_ = d
	cfg, ok := h.Shell.Config()
	if !ok {
		t.Fatal("Run never reached the shell")
	}
	if cfg.Style.Material != desktop.MaterialSidebar {
		t.Fatalf("Style.Material = %q, want sidebar", cfg.Style.Material)
	}
	if cfg.Style.Chrome != desktop.ChromeUnified {
		t.Fatalf("Style.Chrome = %d, want ChromeUnified", cfg.Style.Chrome)
	}
	if in := cfg.Style.TrafficLightInset; in == nil || in.X != 12 || in.Y != 8 {
		t.Fatalf("Style.TrafficLightInset = %+v, want {12 8}", cfg.Style.TrafficLightInset)
	}
	if cfg.SidebarWidth != 220 {
		t.Fatalf("SidebarWidth = %d, want 220", cfg.SidebarWidth)
	}
	if got := h.Shell.SidebarWidthOf("main"); got != 220 {
		t.Fatalf("main window sidebar width = %d, want 220 (from Config)", got)
	}

	// A secondary window's spec carries its own zone and style.
	w, err := d.OpenWindow(desktop.WindowSpec{
		Path:   "/two",
		Title:  "Second",
		Width:  400,
		Height: 300,
		Style: desktop.WindowStyle{
			Material:          desktop.MaterialWindow,
			Chrome:            desktop.ChromeUnified,
			TrafficLightInset: &desktop.Inset{X: 20, Y: 16},
		},
		SidebarWidth: 180,
	})
	if err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 {
		t.Fatalf("OpenWindowCalls = %d, want 1", len(calls))
	}
	spec := calls[0].Spec
	if spec.Style.Material != desktop.MaterialWindow || spec.Style.Chrome != desktop.ChromeUnified {
		t.Fatalf("secondary spec style = %+v", spec.Style)
	}
	if in := spec.Style.TrafficLightInset; in == nil || in.X != 20 || in.Y != 16 {
		t.Fatalf("secondary spec inset = %+v, want {20 16}", in)
	}
	if got := h.Shell.SidebarWidthOf(w.ID()); got != 180 {
		t.Fatalf("secondary window sidebar width = %d, want 180", got)
	}
}

func TestSetChromeValidation(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Chrome"})

	for _, tc := range []struct {
		name  string
		input map[string]any
	}{
		{"negative", map[string]any{"sidebarWidth": -1}},
		{"over the cap", map[string]any{"sidebarWidth": 5000}},
		{"a string", map[string]any{"sidebarWidth": "wide"}},
	} {
		h.CallFrom("main", "window", "setChrome", tc.input).AssertCode(t, desktop.CodeInvalidInput)
	}
	if got := h.Shell.SidebarWidthOf("main"); got != 0 {
		t.Fatalf("a refused setChrome changed the width to %d", got)
	}

	// A valid report answers null and applies to the calling window.
	res := h.CallFrom("main", "window", "setChrome", map[string]any{"sidebarWidth": 300}).AssertOK(t)
	if string(res.Result) != "null" {
		t.Fatalf("setChrome result = %s, want null", res.Result)
	}
	if got := h.Shell.SidebarWidthOf("main"); got != 300 {
		t.Fatalf("main window sidebar width = %d, want 300", got)
	}

	// The report reaches only the window that sent it.
	w2 := openPath(t, h, "/two")
	h.CallFrom(w2, "window", "setChrome", map[string]any{"sidebarWidth": 240}).AssertOK(t)
	if got := h.Shell.SidebarWidthOf("main"); got != 300 {
		t.Fatalf("a w2 setChrome moved main to %d", got)
	}
	if got := h.Shell.SidebarWidthOf(w2); got != 240 {
		t.Fatalf("w2 sidebar width = %d, want 240", got)
	}
}

func TestWindowFocusBlurEvents(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Chrome"})
	w2 := openPath(t, h, "/two")

	// The native side reports the window becoming key and resigning key.
	h.FocusWindow("main")
	ev := h.WaitEvent("window_focus")
	if string(ev.Payload) != `{"id":"main"}` {
		t.Fatalf("window_focus payload = %s, want main", ev.Payload)
	}
	// Focus events are app-wide: the second window's page hears them too.
	h.FocusWindow(w2)
	h.Wait("the second window's focus to reach main", func() bool {
		return len(eventsNamed(h.Events(), "window_focus")) >= 2
	})
	got := eventsNamed(h.Window(w2).Events(), "window_focus")
	if len(got) < 2 {
		t.Fatalf("w2 saw %d window_focus events, want both", len(got))
	}
	if last := got[len(got)-1]; string(last.Payload) != `{"id":"`+w2+`"}` {
		t.Fatalf("w2's last window_focus = %s, want %q", last.Payload, w2)
	}

	h.BlurWindow(w2)
	ev = h.WaitEvent("window_blur")
	if string(ev.Payload) != `{"id":"`+w2+`"}` {
		t.Fatalf("window_blur payload = %s, want %q", ev.Payload, w2)
	}
}

func TestReduceTransparencyReachesPage(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Chrome"})
	openPath(t, h, "/two")

	if h.Shell.Appearance().ReduceTransparency {
		t.Fatal("the fake shell starts with Reduce Transparency on")
	}
	h.SetReduceTransparency(true)
	ev := h.WaitEvent("reduce_transparency")
	if string(ev.Payload) != `{"on":true}` {
		t.Fatalf("reduce_transparency payload = %s, want on", ev.Payload)
	}
	if !h.Shell.Appearance().ReduceTransparency {
		t.Fatal("SetReduceTransparency(true) did not change Appearance()")
	}
	// The event is app-wide: the second window's page sees the change too.
	h.SetReduceTransparency(false)
	h.Wait("the off event to reach the second window", func() bool {
		return len(eventsNamed(h.Window("w2").Events(), "reduce_transparency")) >= 2
	})

	// The boot marker carries the current value so the first paint is
	// right.
	for _, on := range []bool{true, false} {
		js := desktop.BootstrapJS("main", on)
		// The marker splices a JSON string into JSON.parse, so the
		// quotes arrive escaped; that is the page-side contract.
		want := `reduceTransparency\":` + map[bool]string{true: "true", false: "false"}[on]
		if !strings.Contains(js, want) {
			t.Fatalf("BootstrapJS(main, %v) = %s, missing %s", on, js, want)
		}
	}
}

func TestNewRefusesBadChrome(t *testing.T) {
	neg := -3
	for _, tc := range []struct {
		name string
		cfg  desktop.Config
		want string
	}{
		{"unknown material", desktop.Config{
			ID:    "chrome.bad.test",
			Style: desktop.WindowStyle{Material: desktop.WindowMaterial("frosted")},
		}, "Material"},
		{"negative inset x", desktop.Config{
			ID:    "chrome.bad.test",
			Style: desktop.WindowStyle{TrafficLightInset: &desktop.Inset{X: neg, Y: 4}},
		}, "TrafficLightInset"},
		{"negative inset y", desktop.Config{
			ID:    "chrome.bad.test",
			Style: desktop.WindowStyle{TrafficLightInset: &desktop.Inset{X: 4, Y: neg}},
		}, "TrafficLightInset"},
		{"sidebar width negative", desktop.Config{
			ID:           "chrome.bad.test",
			SidebarWidth: -1,
		}, "SidebarWidth"},
		{"sidebar width over the cap", desktop.Config{
			ID:           "chrome.bad.test",
			SidebarWidth: 4097,
		}, "SidebarWidth"},
		{"bad widget style", desktop.Config{
			ID:      "chrome.bad.test",
			Widgets: []desktop.WindowSpec{{Path: "/two", Style: desktop.WindowStyle{Material: desktop.WindowMaterial("frosted")}}},
		}, "Material"},
		{"bad settings style", desktop.Config{
			ID:       "chrome.bad.test",
			Settings: &desktop.WindowSpec{Path: "/settings", Style: desktop.WindowStyle{TrafficLightInset: &desktop.Inset{X: neg}}},
		}, "TrafficLightInset"},
	} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s: New accepted the config", tc.name)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, tc.want) || !strings.Contains(msg, "desktop:") {
					t.Fatalf("%s: panic = %v, want a desktop: message naming %s", tc.name, r, tc.want)
				}
			}()
			desktop.New(tc.cfg)
		}()
	}

	// The boundaries are fine.
	desktop.New(desktop.Config{ID: "chrome.ok.test", SidebarWidth: 4096, Shell: desktoptest.NewShell()})
	desktop.New(desktop.Config{ID: "chrome.ok.test", SidebarWidth: 0, Shell: desktoptest.NewShell()})
}

func TestOpenWindowRefusesBadStyle(t *testing.T) {
	h, d := chromeApp(t, desktop.Config{Title: "Chrome"})
	_, err := d.OpenWindow(desktop.WindowSpec{
		Path:  "/two",
		Style: desktop.WindowStyle{Material: desktop.WindowMaterial("frosted")},
	})
	if de, ok := err.(*desktop.Error); !ok || de.Code != desktop.CodeInvalidInput {
		t.Fatalf("OpenWindow with an unknown material: %v, want invalid_input", err)
	}
	if calls := h.Shell.OpenWindowCalls(); len(calls) != 0 {
		t.Fatalf("a refused OpenWindow reached the shell %d times", len(calls))
	}
}
