package desktop_test

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

func TestRelaunchDropsTransientFrames(t *testing.T) {
	directory := t.TempDir()
	config := desktop.Config{RememberWindows: true, Settings: &desktop.WindowSpec{Path: "/settings"}}
	app, battery := wsApp(t, config, directory, nil)
	harness := desktoptest.Run(t, app, battery)
	main := desktop.Frame{X: 100, Y: 120, Width: 800, Height: 600}
	settings := desktop.Frame{X: 200, Y: 220, Width: 400, Height: 300}
	harness.MoveWindow("main", main)
	harness.OpenSettings()
	harness.MoveWindow("settings", settings)
	for index := range 2 {
		window, err := battery.OpenWindow(desktop.WindowSpec{Path: fmt.Sprintf("/two?old=%d", index)})
		if err != nil {
			t.Fatal(err)
		}
		harness.MoveWindow(window.ID(), desktop.Frame{X: 300 + index*50, Y: 320, Width: 500, Height: 400})
	}
	harness.Wait("all first-run frames persisted", func() bool {
		windows := windowsJSON(t, directory, "windowstate.test")
		return windows["main"].Frame != nil && windows["settings"].Frame != nil &&
			windows["w2"].Frame != nil && windows["w3"].Frame != nil
	})
	if err := harness.Quit(); err != nil {
		t.Fatal(err)
	}
	if windows := windowsJSON(t, directory, "windowstate.test"); windows["w2"].Frame == nil || windows["w3"].Frame == nil {
		t.Fatal("first launch did not persist transient frame fixtures")
	}
	app, battery = wsApp(t, config, directory, nil)
	harness = desktoptest.Run(t, app, battery)
	nativeConfig, _ := harness.Shell.Config()
	if nativeConfig.Frame == nil || *nativeConfig.Frame != main {
		t.Fatalf("main frame lost: %+v", nativeConfig.Frame)
	}
	harness.OpenSettings()
	for index := range 2 {
		if _, err := battery.OpenWindow(desktop.WindowSpec{Path: fmt.Sprintf("/two?new=%d", index)}); err != nil {
			t.Fatal(err)
		}
	}
	explicit := desktop.Frame{X: 30, Y: 40, Width: 450, Height: 350}
	if _, err := battery.OpenWindow(desktop.WindowSpec{Path: "/two?explicit", Frame: &explicit}); err != nil {
		t.Fatal(err)
	}
	for _, call := range harness.Shell.OpenWindowCalls() {
		switch call.ID {
		case "settings":
			if call.Spec.Frame == nil || *call.Spec.Frame != settings {
				t.Errorf("settings frame lost: %+v", call.Spec.Frame)
			}
		case "w2", "w3":
			if call.Spec.Frame != nil {
				t.Errorf("%s inherited an unrelated launch's frame: %+v", call.ID, *call.Spec.Frame)
			}
		case "w4":
			if call.Spec.Frame == nil || *call.Spec.Frame != explicit {
				t.Errorf("explicit frame lost: %+v", call.Spec.Frame)
			}
		default:
			t.Errorf("unexpected window %q", call.ID)
		}
	}
}
