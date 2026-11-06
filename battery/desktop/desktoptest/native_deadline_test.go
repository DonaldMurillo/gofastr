package desktoptest

import (
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestNativeReadinessDeadlineFails(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "readiness")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stderr
	os.Stderr = output
	defer func() { os.Stderr = previous }()
	harness := &NativeHarness{
		Battery: desktop.New(desktop.Config{ID: "test.native.deadline"}),
		Shell:   NewShell(),
		ready:   make(chan struct{}),
		runDone: make(chan struct{}),
	}
	harness.watchReady()
	if !harness.timedOut.Load() {
		t.Error("readiness deadline did not fail the native run")
	}
	message, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(message), "page readiness") {
		t.Errorf("readiness deadline was silent: %q", message)
	}
}
