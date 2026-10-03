package desktop_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// desktoptest is the exported double app-level tests wire into
// desktop.Config{Shell: ...}. The battery's own suite runs an
// unexported twin it cannot share with the package (importing
// desktoptest from the internal test package is a cycle), so this file
// pins the exported double against the same contract the battery's
// tests hold the internal one to: it satisfies desktop.Shell, Run hands
// back a working Window, and Quit ends Run.

var _ desktop.Shell = (*desktoptest.Shell)(nil)

func TestDesktoptestDoubleMatchesContract(t *testing.T) {
	shell := desktoptest.NewShell()

	winCh := make(chan *desktoptest.Window, 1)
	runDone := make(chan error, 1)
	go func() {
		runDone <- shell.Run(context.Background(), desktop.WindowConfig{Title: "T", Width: 10, Height: 10}, func(w desktop.Window) {
			winCh <- w.(*desktoptest.Window)
		})
	}()

	var win *desktoptest.Window
	select {
	case win = <-winCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run never called ready")
	}

	if err := win.SetTitle("Changed"); err != nil {
		t.Errorf("SetTitle: %v", err)
	}
	if err := win.Navigate("/enter"); err != nil {
		t.Errorf("Navigate: %v", err)
	}
	if err := win.Eval("1+1"); err != nil {
		t.Errorf("Eval: %v", err)
	}
	if png, err := win.Snapshot(context.Background()); err != nil || len(png) == 0 {
		t.Errorf("Snapshot: %v (%d bytes)", err, len(png))
	}
	if got := win.Title(); got != "Changed" {
		t.Errorf("Title = %q, want Changed", got)
	}
	if urls := win.NavURLs(); len(urls) != 1 || urls[0] != "/enter" {
		t.Errorf("NavURLs = %v", urls)
	}
	if evals := win.Evals(); len(evals) != 1 {
		t.Errorf("Evals = %v", evals)
	}

	cfg, ok := shell.Config()
	if !ok || cfg.Title != "T" || cfg.Width != 10 {
		t.Fatalf("Config after Run = %+v, ok=%v", cfg, ok)
	}

	shell.Quit()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Quit")
	}
}
