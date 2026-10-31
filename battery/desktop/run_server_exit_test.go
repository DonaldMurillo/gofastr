package desktop

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// app.Start's result was read before the window opened and again only
// after it closed, so a server that stopped while the window was up left
// the window on a dead app with no error (2026-10-27 L1). The window
// quits and Run returns.
func TestRunQuitsWhenServerStops(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")

	site := appui.NewApp("DesktopServerExit")
	site.Register("/", e2eScreen{}, containerLayout("public"))
	shell := newFakeShell()
	b := New(Config{ID: "exit.example.app", Title: "Exit", Shell: shell})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "desktopexit"}))
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)

	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(app) }()
	waitFor(t, "the window to open", func() bool {
		w, _ := b.Window()
		return w != nil
	})

	// The server goes away under the open window.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-runErr:
	case <-time.After(10 * time.Second):
		shell.Quit()
		<-runErr
		t.Fatal("the window stayed open after the app server stopped")
	}
}

// lockedBuffer is a bytes.Buffer safe for a logger and a reader.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The watcher stays quiet on the stop Run itself makes after the window
// closes: only a stop nobody asked for is an error.
func TestRunQuitLogsNoServerStop(t *testing.T) {
	t.Setenv("GOFASTR_ISOLATION", "off")

	var logs lockedBuffer
	site := appui.NewApp("DesktopQuiet")
	site.Register("/", e2eScreen{}, containerLayout("public"))
	shell := newFakeShell()
	b := New(Config{ID: "quiet.example.app", Title: "Quiet", Shell: shell})
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "desktopquiet"}),
		framework.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))),
	)
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)

	runErr := make(chan error, 1)
	go func() { runErr <- b.Run(app) }()
	waitFor(t, "the window to open", func() bool {
		w, _ := b.Window()
		return w != nil
	})
	shell.Quit()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after Quit")
	}
	// The watcher runs on its own goroutine after Start returns.
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(logs.String(), "server stopped") {
		t.Fatalf("a normal quit logged a server stop:\n%s", logs.String())
	}
}
