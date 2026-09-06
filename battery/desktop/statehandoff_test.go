package desktop

import (
	"testing"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The app state store handoff: Run assigns the store after the app's
// listener is already answering (the window may be loading its boot
// URL while later handlers read the store through stateStore), so the
// assignment must be synchronized by construction, not by the hope
// that no request can pass the boot gate in the gap. This test is a
// reader goroutine that shares no lock with Run; run under -race it
// fails on any plain-field handoff.

func TestStateHandoffSynchronized(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())

	site := appui.NewApp("StateHandoff")
	layout := appui.NewLayout("public").WithContainer()
	site.Register("/", e2eScreen{}, layout)

	shell := newFakeShell()
	b := New(Config{ID: "state.example.app", Title: "State", Shell: shell})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "statehandoff"}))
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)

	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(app) }()

	// The shape of a handler goroutine calling stateStore(): a read
	// with no lock, no channel, no atomic in common with Run's write.
	for b.stateStore() == nil {
	}
	if b.stateStore().Path() == "" {
		t.Fatal("the store opened with an empty path")
	}

	shell.Quit()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after Quit")
	}
}
