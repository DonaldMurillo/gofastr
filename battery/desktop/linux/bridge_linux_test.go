//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type bridgeScreen struct{}

func (bridgeScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("GoFastr Linux bridge"))
}

// Kept in its own test so a failure names the battery bridge, not the shell.
// This covers the real battery boot cookie, authenticated JS bridge and native
// capability calls, beyond the shell's standalone WebKit transport test.
func TestNativeGoFastrBridge(t *testing.T) {
	if os.Getenv("GOFASTR_LINUX_NATIVE") != "1" {
		t.Skip("set GOFASTR_LINUX_NATIVE=1 in a graphical session")
	}
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	t.Setenv("GOFASTR_ISOLATION", "off")
	site := appui.NewApp("Linux bridge")
	layout := appui.NewLayout("main", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	site.Register("/", bridgeScreen{}, layout)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "linux-bridge"}))
	app.Mount(uihost.New(site))
	shell := New().(*linuxShell)
	battery := desktop.New(desktop.Config{ID: "dev.gofastr.linux-bridge-test", Title: "Linux Bridge Test", Shell: shell})
	app.RegisterBattery(battery)
	result := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	go func() {
		defer shell.Quit()
		result <- exerciseBridge(ctx, battery, shell)
	}()
	if err := battery.Run(app); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
func exerciseBridge(ctx context.Context, battery *desktop.Battery, shell *linuxShell) error {
	var evaluator desktop.PageEvaluator
	for {
		if w, ok := battery.Window(); ok {
			evaluator = w.(desktop.PageEvaluator)
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	for {
		value, err := evaluator.EvalAsync(ctx, `return !!(window.__gofastr && __gofastr.desktop && __gofastr.desktop.window)`)
		if err == nil && string(value) == "true" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
	value, err := evaluator.EvalAsync(ctx, `return await __gofastr.desktop.window.title()`)
	if err != nil || string(value) != `{"title":"Linux Bridge Test"}` {
		return fmt.Errorf("native bridge title: %s (%v)", value, err)
	}
	shell.ScriptPrompts(desktop.DecisionAllowOnce, desktop.DecisionAllowOnce)
	if _, err = evaluator.EvalAsync(ctx, `return await __gofastr.desktop.clipboard.writeText({text:"real GoFastr bridge"})`); err != nil {
		return fmt.Errorf("bridge clipboard write: %w", err)
	}
	value, err = evaluator.EvalAsync(ctx, `return await __gofastr.desktop.clipboard.readText()`)
	if err != nil || string(value) != `{"text":"real GoFastr bridge"}` {
		return fmt.Errorf("bridge clipboard read: %s (%v)", value, err)
	}
	value, err = evaluator.EvalAsync(ctx, `return location.origin`)
	if err != nil {
		return err
	}
	// The URL has no boot token and this independent client has no session cookie.
	if len(value) < 2 {
		return fmt.Errorf("invalid origin %s", value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, string(value[1:len(value)-1])+"/", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		return fmt.Errorf("unauthenticated page status %d, want 403", response.StatusCode)
	}
	return nil
}
