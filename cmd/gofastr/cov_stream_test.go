package main

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
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
	out := covT_capStdout(t, func() { streamOneTurn(ch) })
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected streamed text, got %q", out)
	}
}

func TestStreamOneTurnErrorEvent(t *testing.T) {
	ch := make(chan control.EventEnvelope, 2)
	ch <- covT_env(t, control.Error{Reason: "boom", Message: "bad"})
	close(ch)
	// Error path writes to stderr and returns; just ensure no panic.
	covT_capStdout(t, func() { streamOneTurn(ch) })
}
