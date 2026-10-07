package framework

import (
	"bytes"
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
