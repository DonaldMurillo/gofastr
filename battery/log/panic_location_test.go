package log_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// panicInit is a battery whose Init panics, the way battery/admin's did
// on an empty path prefix.
type panicInit struct{}

func (panicInit) Name() string              { return "panicky" }
func (panicInit) Init(*framework.App) error { panic("init kaboom") }

// A battery Init panic logs its stack through this plugin, to server.log
// under the state dir, not to the terminal. The boot error must say
// that file, or a developer whose app would not start reads stderr for
// a stack that is not there.
func TestInitPanicErrorNamesServerLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "panicapp"}))
	app.RegisterPlugin(log.New(log.Config{Console: log.ConsoleOff}))
	app.RegisterBattery(panicInit{})
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	err := app.InitPlugins()
	if err == nil {
		t.Fatal("a panicking battery Init returned no error")
	}
	want := filepath.Join(dir, "panicapp", "server.log")
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error does not name %s: %v", want, err)
	}
}
