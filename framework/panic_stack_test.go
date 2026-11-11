package framework

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// A recovered module panic logs where it happened. GOTRACEBACK prints
// nothing for a panic that was recovered, so the stack must reach the
// log or the error names a panic nobody can find. The panic value stays
// out of both: it may hold a secret.
func TestModulePanicLogsStack(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	bm := NewBatteryManager()
	_ = bm.Register(&panicBattery{name: "panicky"})
	err := bm.InitAll(&App{})
	if err == nil {
		t.Fatal("a panicking Init returned no error")
	}
	if strings.Contains(err.Error(), "GOTRACEBACK") {
		t.Errorf("the error points at GOTRACEBACK, which prints nothing for a recovered panic: %v", err)
	}
	log := buf.String()
	if !strings.Contains(log, "panicBattery).Init") {
		t.Errorf("the log does not carry the panicking frame:\n%s", log)
	}
	if strings.Contains(log+err.Error(), "init kaboom") {
		t.Errorf("SECURITY: the panic value reached the log or the error:\n%s\n%v", log, err)
	}
}

// startPanicBattery panics in OnStart.
type startPanicBattery struct{ countBattery }

func (s *startPanicBattery) OnStart(context.Context) error { panic("start kaboom") }
func (s *startPanicBattery) OnStop(context.Context) error  { return nil }

// The stack goes to the App's logger, the destination WithLogger chose,
// not to slog.Default: an app that routes its logs elsewhere would
// otherwise lose the only record of where a module panicked.
func TestModulePanicLogsToAppLogger(t *testing.T) {
	var deflt, own bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&deflt, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	app := NewApp(WithLogger(slog.New(slog.NewTextHandler(&own, nil))))
	_ = app.Batteries.Register(&panicBattery{name: "panicky"})
	if err := app.Batteries.InitAll(app); err == nil {
		t.Fatal("a panicking Init returned no error")
	}
	if !strings.Contains(own.String(), "panicBattery).Init") {
		t.Errorf("the App's logger lacks the Init panic's stack:\n%s", own.String())
	}

	var plug bytes.Buffer
	plugged := NewApp(WithLogger(slog.New(slog.NewTextHandler(&plug, nil))))
	_ = plugged.Plugins.Register(&panicBattery{name: "panicky-plugin"})
	if err := plugged.Plugins.InitAll(plugged); err == nil {
		t.Fatal("a panicking plugin Init returned no error")
	}
	if !strings.Contains(plug.String(), "panicBattery).Init") {
		t.Errorf("the App's logger lacks the plugin Init panic's stack:\n%s", plug.String())
	}

	starter := NewApp(WithLogger(slog.New(slog.NewTextHandler(&own, nil))))
	_ = starter.Batteries.Register(&startPanicBattery{countBattery{name: "starter"}})
	if err := starter.Batteries.InitAll(starter); err != nil {
		t.Fatal(err)
	}
	if err := starter.Batteries.StartAll(context.Background()); err == nil {
		t.Fatal("a panicking OnStart returned no error")
	}
	if !strings.Contains(own.String(), "startPanicBattery).OnStart") {
		t.Errorf("the App's logger lacks the OnStart panic's stack:\n%s", own.String())
	}
	if strings.Contains(deflt.String(), "recovered panic") {
		t.Errorf("a recovered panic went to slog.Default:\n%s", deflt.String())
	}
}

// locatedHandler is a slog handler that says where its records land, the
// way battery/log's default file sink does.
type locatedHandler struct {
	slog.Handler
	where string
}

func (h locatedHandler) LogLocation() string { return h.where }

// The error says where the stack went. battery/log writes it to a file
// under the OS state dir, not to the terminal, so "its stack is logged"
// sent a developer whose boot died looking at stderr for a record that
// sat in ~/.local/state/<app>/server.log.
func TestModulePanicNamesLogFile(t *testing.T) {
	var buf bytes.Buffer
	where := "/state/Shop/server.log"
	app := NewApp(WithLogger(slog.New(locatedHandler{slog.NewTextHandler(&buf, nil), where})))
	_ = app.Batteries.Register(&panicBattery{name: "panicky"})
	err := app.Batteries.InitAll(app)
	if err == nil {
		t.Fatal("a panicking Init returned no error")
	}
	if !strings.Contains(err.Error(), where) {
		t.Errorf("the error does not name the log file: %v", err)
	}
	if !strings.Contains(buf.String(), "panicBattery).Init") {
		t.Errorf("the stack did not reach the logger:\n%s", buf.String())
	}

	plain := NewApp(WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	_ = plain.Batteries.Register(&panicBattery{name: "panicky"})
	if err := plain.Batteries.InitAll(plain); err == nil || strings.Contains(err.Error(), "logged to") {
		t.Errorf("a logger with no location named one: %v", err)
	}

	// The location is quoted: a path or custom handler carrying terminal
	// controls must not write them live into the boot error.
	forged := NewApp(WithLogger(slog.New(locatedHandler{slog.NewTextHandler(&buf, nil), "/state/\x1b[2J\nok/server.log"})))
	_ = forged.Batteries.Register(&panicBattery{name: "panicky"})
	if err := forged.Batteries.InitAll(forged); err == nil || strings.ContainsAny(err.Error(), "\x1b\n") {
		t.Errorf("the boot error carries a live control byte: %q", err)
	}
}
