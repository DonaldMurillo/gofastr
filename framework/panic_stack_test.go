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
