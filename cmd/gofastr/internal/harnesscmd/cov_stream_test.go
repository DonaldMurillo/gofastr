package harnesscmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func covT_env(t *testing.T, e control.Event) control.EventEnvelope {
	t.Helper()
	env, err := control.EncodeEvent(1, e, ids.NewSessionID(), ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestStreamOneTurnTextThenEnd(t *testing.T) {
	ch := make(chan control.EventEnvelope, 3)
	ch <- covT_env(t, control.TextDelta{Text: "hello"})
	ch <- covT_env(t, control.TurnEnded{})
	close(ch)
	out := clitest.CovT_capStdout(t, func() { streamOneTurn(ch) })
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected streamed text, got %q", out)
	}
}

func TestStreamOneTurnErrorEvent(t *testing.T) {
	ch := make(chan control.EventEnvelope, 2)
	ch <- covT_env(t, control.Error{Reason: "boom", Message: "bad"})
	ch <- covT_env(t, control.TextDelta{Text: "after-error"})
	close(ch)
	var out string
	errOut := covT_capStderr(t, func() {
		out = clitest.CovT_capStdout(t, func() { streamOneTurn(ch) })
	})
	if !strings.Contains(errOut, "[error]") || !strings.Contains(errOut, "bad") {
		t.Fatalf("error event should be reported on stderr, got %q", errOut)
	}
	if strings.Contains(out, "after-error") {
		t.Fatalf("the turn should end at the error, but kept streaming: %q", out)
	}
}

func covT_capStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stderr
	os.Stderr = f
	func() { defer func() { os.Stderr = old }(); fn() }()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
