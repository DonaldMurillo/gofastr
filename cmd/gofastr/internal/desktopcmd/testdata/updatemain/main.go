// Command updatemain is the app the Linux update end-to-end tests
// install: a desktop battery whose updater reads the feed named by
// UPDATEMAIN_FEED, on the native shell when UPDATEMAIN_NATIVE=1 and the
// desktoptest shell otherwise. Once its main window is open it checks
// the feed, installs a newer release when there is one, and writes what
// it saw to UPDATEMAIN_REPORTS/<pid>.json.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type report struct {
	PID         int    `json:"pid"`
	Predecessor string `json:"predecessor"`
	Available   bool   `json:"available"`
	FeedVersion string `json:"feed_version"`
	Window      bool   `json:"window"`
	Error       string `json:"error,omitempty"`
}

type screen struct{}

func (screen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("update e2e"))
}

func main() {
	r := report{PID: os.Getpid(), Predecessor: os.Getenv("GOFASTR_DESKTOP_UPDATE_PREDECESSOR")}
	site := appui.NewApp("UpdateE2E")
	site.Register("/", screen{}, appui.NewLayout("public", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return l.Primary()
	}))
	var shell desktop.Shell = desktoptest.NewShell()
	if os.Getenv("UPDATEMAIN_NATIVE") == "1" {
		shell = native.Shell()
	}
	b := desktop.New(desktop.Config{
		ID:     "dev.gofastr.updatee2e",
		Title:  "UpdateE2E",
		Shell:  shell,
		Update: &desktop.UpdateConfig{FeedURL: os.Getenv("UPDATEMAIN_FEED"), PublicKey: os.Getenv("UPDATEMAIN_KEY"), Interval: -1},
	})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "updatee2e"}))
	app.Mount(uihost.New(site))
	app.RegisterBattery(b)
	done := make(chan error, 1)
	go func() { done <- b.Run(app) }()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, ok := b.Window(); ok {
			r.Window = true
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	u, err := b.CheckForUpdates(ctx)
	r.Available, r.FeedVersion = u.Available, u.Version
	if err == nil && u.Available {
		err = b.InstallUpdate(ctx)
	}
	if err != nil {
		r.Error = err.Error()
	}
	out, _ := json.Marshal(r)
	_ = os.WriteFile(filepath.Join(os.Getenv("UPDATEMAIN_REPORTS"), strconv.Itoa(r.PID)+".json"), out, 0o600)
	if !u.Available || err != nil {
		shell.Quit()
	}
	if err := <-done; err != nil {
		os.Exit(1)
	}
}
