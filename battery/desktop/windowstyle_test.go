package desktop_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// Window styles, portable: the Widget helper, the windows.open style
// input through the real chokepoint, and Config.Style / Config.Widgets
// flowing through Run. The darwin mask/level math has its own unit
// test (shell_darwin_style_test.go) and the live-panel proof is the
// native e2e step.

// TestWidgetHelperBuildsFloatingPanel pins desktop.Widget's spec: the
// one-line recipe a floating widget is supposed to be.
func TestWidgetHelperBuildsFloatingPanel(t *testing.T) {
	spec := desktop.Widget("/widget", 320, 200)
	if spec.Path != "/widget" || spec.Width != 320 || spec.Height != 200 {
		t.Fatalf("spec = %+v", spec)
	}
	s := spec.Style
	if s.Chrome != desktop.ChromeNone {
		t.Errorf("chrome = %v, want ChromeNone (borderless)", s.Chrome)
	}
	if !s.Panel {
		t.Error("Widget must set Panel: a widget never steals focus")
	}
	if !s.Transparent {
		t.Error("Widget must set Transparent: the page paints its own surface")
	}
	// Resizable nil means true, which is meaningless on a borderless
	// window but must not be a non-nil false that later code could read
	// as intent.
	if s.Resizable != nil {
		t.Errorf("Resizable = %v, want nil (default)", *s.Resizable)
	}
}

// TestWindowsOpenAppliesStyleObject drives windows.open with a style
// through the page's bridge and asserts what the shell received.
func TestWindowsOpenAppliesStyleObject(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "Style"})
	h := desktoptest.Run(t, app, d)

	resizable := false
	var opened struct {
		ID string `json:"id"`
	}
	h.Call("windows", "open", map[string]any{
		"path":   "/two",
		"width":  260,
		"height": 140,
		"style": map[string]any{
			"chrome":      "hiddenTitle",
			"float":       true,
			"transparent": true,
			"resizable":   false,
			"allSpaces":   true,
			"x":           10,
			"y":           20,
		},
	}).MustResult(t, &opened)
	if opened.ID == "" {
		t.Fatalf("windows.open returned no id: %+v", opened)
	}
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 1 {
		t.Fatalf("OpenWindow calls = %d, want 1", len(calls))
	}
	got := calls[0].Spec
	if got.Width != 260 || got.Height != 140 {
		t.Errorf("spec size = %dx%d, want 260x140", got.Width, got.Height)
	}
	s := got.Style
	if s.Chrome != desktop.ChromeHiddenTitle {
		t.Errorf("chrome = %v, want ChromeHiddenTitle", s.Chrome)
	}
	if !s.Float || !s.Transparent || !s.AllSpaces {
		t.Errorf("flags = float:%v transparent:%v allSpaces:%v, want all true", s.Float, s.Transparent, s.AllSpaces)
	}
	if s.Resizable == nil || *s.Resizable != resizable {
		t.Errorf("resizable = %v, want false", s.Resizable)
	}
	if s.X == nil || *s.X != 10 || s.Y == nil || *s.Y != 20 {
		t.Errorf("origin = %v,%v, want 10,20", s.X, s.Y)
	}
}

// TestWindowsOpenRefusesBadStyle pins the input guards: an unknown
// chrome, a half-set origin, and a non-object style are invalid_input,
// and none of them reaches the shell.
func TestWindowsOpenRefusesBadStyle(t *testing.T) {
	app, d := uiApp(t, desktop.Config{Title: "Style"})
	h := desktoptest.Run(t, app, d)

	for name, style := range map[string]any{
		"unknown chrome": map[string]any{"chrome": "frobnicate"},
		"x without y":    map[string]any{"x": 5},
		"y without x":    map[string]any{"y": 5},
		"far origin":     map[string]any{"x": 5, "y": 99999999},
		"not an object":  "none",
	} {
		res := h.Call("windows", "open", map[string]any{"path": "/two", "style": style})
		if res.Code != desktop.CodeInvalidInput {
			t.Errorf("%s: code = %q, want invalid_input", name, res.Code)
		}
	}
	if n := len(h.Shell.OpenWindowCalls()); n != 0 {
		t.Fatalf("a refused style still opened %d window(s)", n)
	}
}

// TestConfigStyleFlowsToMainWindow: the main window config the shell
// receives carries Config.Style verbatim.
func TestConfigStyleFlowsToMainWindow(t *testing.T) {
	app, d := uiApp(t, desktop.Config{
		Title: "Styled",
		Style: desktop.WindowStyle{Chrome: desktop.ChromeHiddenTitle, Float: true},
	})
	h := desktoptest.Run(t, app, d)
	cfg, ok := h.Shell.Config()
	if !ok {
		t.Fatal("Run never reached the shell")
	}
	if cfg.Style.Chrome != desktop.ChromeHiddenTitle || !cfg.Style.Float {
		t.Fatalf("WindowConfig.Style = %+v, want hiddenTitle + float", cfg.Style)
	}
}

// TestConfigWidgetsOpenAfterBoot: Widgets open in order after the boot
// navigation, through OpenWindow, with the normal id assignment.
func TestConfigWidgetsOpenAfterBoot(t *testing.T) {
	x, y := 8, 40
	app, d := uiApp(t, desktop.Config{
		Title: "Widgets",
		Widgets: []desktop.WindowSpec{
			desktop.Widget("/two", 320, 200),
			{Path: "/settings", Title: "Side", Width: 200, Height: 150, Style: desktop.WindowStyle{X: &x, Y: &y}},
		},
	})
	h := desktoptest.Run(t, app, d)

	// The boot navigation happened before any widget opened.
	if navs := h.Window("main").NavURLs(); len(navs) == 0 {
		t.Fatal("the main window never received the boot navigation")
	}
	h.Wait("the widget windows", func() bool { return len(h.WindowIDs()) == 3 })
	calls := h.Shell.OpenWindowCalls()
	if len(calls) != 2 {
		t.Fatalf("OpenWindow calls = %d, want 2", len(calls))
	}
	if calls[0].ID != "w2" || calls[1].ID != "w3" {
		t.Fatalf("widget ids = %q, %q, want w2, w3 (the normal rule)", calls[0].ID, calls[1].ID)
	}
	if calls[0].Spec.Style.Chrome != desktop.ChromeNone || !calls[0].Spec.Style.Panel {
		t.Fatalf("first widget spec = %+v, want the Widget helper's panel style", calls[0].Spec)
	}
	if calls[0].URL != h.URL("/two") || calls[1].URL != h.URL("/settings") {
		t.Fatalf("widget URLs = %q, %q", calls[0].URL, calls[1].URL)
	}
	if s := calls[1].Spec.Style; s.X == nil || *s.X != 8 || s.Y == nil || *s.Y != 40 {
		t.Fatalf("second widget origin = %v, %v, want 8, 40", s.X, s.Y)
	}
}
