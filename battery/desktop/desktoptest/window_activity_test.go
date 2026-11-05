package desktoptest_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

func TestWindowChromeEmitsActivity(t *testing.T) {
	app := newTestApp(t, desktop.Config{
		Tray:     &desktop.Tray{Title: "Tray", CloseHidesWindow: true},
		Settings: &desktop.WindowSpec{Path: "/settings", Title: "Preferences"},
	})
	harness := desktoptest.Run(t, app.app, app.d)
	harness.CloseMainWindow()
	harness.WaitEvent("window_blur")
	if !harness.Window("main").Hidden() {
		t.Fatal("closing to tray did not hide the main window")
	}
	if err := harness.Window("main").Focus(); err != nil {
		t.Fatal(err)
	}
	harness.WaitEvent("window_focus")
	if harness.Window("main").Hidden() {
		t.Fatal("focusing did not reveal the main window")
	}
	harness.OpenSettings()
	if err := harness.Window("settings").Focus(); err != nil {
		t.Fatal(err)
	}
	harness.WaitEvent("window_focus")
}
