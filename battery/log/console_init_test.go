package log

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// discardSink throws every entry away. Used to satisfy Config.Sinks so
// Init doesn't auto-create a real file under the OS state dir during
// these wiring tests.
type discardSink struct{}

func (discardSink) Write([]byte) error { return nil }
func (discardSink) Close() error       { return nil }

// hasConsoleSink reports whether the plugin's fanout includes a
// *consoleSink. Reads under the handler's lock.
func hasConsoleSink(p *Plugin) bool {
	p.handler.mu.Lock()
	defer p.handler.mu.Unlock()
	for _, s := range p.handler.sinks {
		if _, ok := s.(*consoleSink); ok {
			return true
		}
	}
	return false
}

func newConsoleApp(t *testing.T, cfg Config) (*framework.App, *Plugin) {
	t.Helper()
	p := New(cfg)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(p)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	return app, p
}

func TestInitConsoleOnAttachesSink(t *testing.T) {
	_, p := newConsoleApp(t, Config{
		Sinks:                  []Sink{discardSink{}},
		Console:                ConsoleOn,
		DisableLifecycleEvents: true,
	})
	if !hasConsoleSink(p) {
		t.Fatal("ConsoleOn: no *consoleSink attached to handler")
	}
}

func TestInitConsoleOffSkipsSink(t *testing.T) {
	_, p := newConsoleApp(t, Config{
		Sinks:                  []Sink{discardSink{}},
		Console:                ConsoleOff,
		DisableLifecycleEvents: true,
	})
	if hasConsoleSink(p) {
		t.Fatal("ConsoleOff: *consoleSink should not be attached")
	}
}

func TestInitConsoleAutoRespectsNoColor(t *testing.T) {
	// NO_COLOR forces shouldColor=false even on a real TTY, so the
	// auto path (zero value) must not attach a console sink.
	t.Setenv("NO_COLOR", "1")
	_, p := newConsoleApp(t, Config{
		Sinks:                  []Sink{discardSink{}},
		DisableLifecycleEvents: true, // Console is the zero value ConsoleAuto
	})
	if hasConsoleSink(p) {
		t.Fatal("ConsoleAuto + NO_COLOR: *consoleSink should not be attached")
	}
}
