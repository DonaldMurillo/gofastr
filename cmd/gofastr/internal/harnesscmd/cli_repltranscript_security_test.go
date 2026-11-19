package harnesscmd

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TextDelta and asserts the terminal never sees the control class.
func TestREPLTranscriptScrubTerminal(t *testing.T) {
	sess := ids.NewSessionID()

	envelope := func(e control.Event) control.EventEnvelope {
		t.Helper()
		env, err := control.EncodeEvent(1, e, sess, ids.NewClientID(), time.Now())
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		return env
	}

	shapes := []struct {
		name    string
		payload string
	}{
		{"osc52 clipboard via ESC", "\x1b]52;c;aG90IGNsaXBib2FyZA==\x07"},
		{"csi-8bit alt screen", "\u009b?1049h"},
		{"rlo display reorder", "safe;\u202E;rm -rf ~"},
		{"zero-width smuggle", "ap\u200Bprove\uFEFF"},
	}

	// Happy path: plain text must still stream through unchanged.
	clean := clitest.CaptureStdout(t, func() {
		ch := make(chan control.EventEnvelope, 2)
		ch <- envelope(control.TextDelta{Text: "Hello world 100%"})
		ch <- envelope(control.TurnEnded{})
		close(ch)
		streamOneTurn(ch)
	})
	if !strings.Contains(clean, "Hello world 100%") {
		t.Fatalf("fixture check: clean transcript text was dropped: %q", clean)
	}

	for _, shape := range shapes {
		out := clitest.CaptureStdout(t, func() {
			ch := make(chan control.EventEnvelope, 2)
			ch <- envelope(control.TextDelta{Text: shape.payload})
			ch <- envelope(control.TurnEnded{})
			close(ch)
			streamOneTurn(ch)
		})
		for _, bad := range []rune{'\x1b', '\u009b', '\u009d', '\u202E', '\u200B', '\uFEFF'} {
			if strings.ContainsRune(out, bad) {
				t.Errorf("SECURITY: [injection] %s: hostile rune %#04x reached the terminal via streamOneTurn (no scrub on this path)", shape.name, bad)
			}
		}
	}
}
