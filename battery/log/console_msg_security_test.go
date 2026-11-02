package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// TestConsoleSinkNeutralizesMsgKeyAndFallback extends the round-4
// console probe (which covered attr VALUES) to every other piece of an
// entry the console renderer writes: the msg, the attr keys, the time
// and level fields, and the malformed-entry fallback. Each was copied
// out of the JSON text raw, and slog's encoder escapes only C0, so a
// C1 CSI (U+009B) or a bidi override (U+202E) in a message built from
// request data — slog.Info("login failed for " + name) — reached the
// developer's terminal live.
func TestConsoleSinkNeutralizesMsgKeyAndFallback(t *testing.T) {
	const csi = "\u009b"
	const rlo = "‮"
	cases := map[string]string{
		"msg":       `{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"login ` + csi + `31m` + rlo + `x"}`,
		"key":       `{"time":"2026-06-17T14:32:07.412Z","level":"INFO","msg":"m","k` + csi + rlo + `":"v"}`,
		"time":      `{"time":"bad` + csi + rlo + `","level":"INFO","msg":"m"}`,
		"level":     `{"time":"2026-06-17T14:32:07.412Z","level":"IN` + csi + rlo + `","msg":"m"}`,
		"malformed": `not json ` + csi + rlo + "\x1b[2J",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			off := false
			s := ConsoleSink(ConsoleOpts{Writer: &buf, Color: &off})
			if err := s.Write([]byte(entry)); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSuffix(buf.String(), "\n")
			if textsafe.ContainsUnsafe(out) {
				t.Errorf("console line carries a live control or bidi rune: %q", out)
			}
		})
	}
}

// TestConsoleSinkMsgReadsAsText pins that the msg is the decoded string,
// not its JSON spelling: a quote in the message printed as \" before.
func TestConsoleSinkMsgReadsAsText(t *testing.T) {
	var buf bytes.Buffer
	off := false
	s := ConsoleSink(ConsoleOpts{Writer: &buf, Color: &off})
	if err := s.Write([]byte(`{"level":"INFO","msg":"said \"hi\""}`)); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, `said "hi"`) {
		t.Errorf("msg rendered as JSON text: %q", got)
	}
}
