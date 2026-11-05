package desktoptest_test

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

func TestSettingsReopenCreatesFreshWindow(t *testing.T) {
	app := newTestApp(t, desktop.Config{Settings: &desktop.WindowSpec{Path: "/settings", Title: "Preferences"}})
	harness := desktoptest.Run(t, app.app, app.d)
	harness.OpenSettings()
	previous := harness.Window("settings")
	previous.SetTitle("Old window")
	previous.Eval("oldGeneration()")
	harness.CloseWindow("settings")
	harness.OpenSettings()
	current := harness.Window("settings")
	if current == previous || current.Closed() || current.Title() != "Preferences" {
		t.Errorf("reopened window = %p closed=%v title=%q; previous=%p", current, current.Closed(), current.Title(), previous)
	}
	if slices.Contains(current.Evals(), "oldGeneration()") || slices.Contains(current.Titles(), "Old window") {
		t.Error("reopened window inherited the previous generation's record")
	}
	for _, window := range []struct {
		value *desktoptest.Window
		want  bool
	}{{previous, true}, {current, false}} {
		state, ok := any(window.value).(interface{ IsClosed() bool })
		if !ok || state.IsClosed() != window.want {
			t.Errorf("window closed state known=%v, want closed=%v", ok, window.want)
		}
	}
	config, _ := harness.Shell.Config()
	config.OnWindowClosed("settings")
	if !slices.Contains(harness.WindowIDs(), "settings") {
		t.Error("delayed close callback removed the live replacement")
	}
}
