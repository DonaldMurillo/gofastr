package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// The fan-out handler must nest attrs and groups exactly as slog's own
// JSONHandler does. It replayed every attr first and then one dotted
// group, so WithGroup("req").With("user_id", 7) logged user_id at the top
// level and nested groups came out as one "a.b" key (round 7 R7-074).
func TestHandlerNestsGroupsLikeSlog(t *testing.T) {
	strip := slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}
	chains := map[string]func(slog.Handler) *slog.Logger{
		"group then attr": func(h slog.Handler) *slog.Logger {
			return slog.New(h).WithGroup("req").With("user_id", 7)
		},
		"nested groups": func(h slog.Handler) *slog.Logger {
			return slog.New(h).WithGroup("a").WithGroup("b")
		},
		"attr group attr group": func(h slog.Handler) *slog.Logger {
			return slog.New(h).With("svc", "api").WithGroup("req").With("id", 1).WithGroup("db")
		},
	}
	for name, chain := range chains {
		var want bytes.Buffer
		chain(slog.NewJSONHandler(&want, &strip)).Info("hi", "k", "v")

		sink := &memSink{}
		chain(newFanoutHandler([]Sink{sink}, &strip)).Info("hi", "k", "v")
		lines := sink.lines()
		if len(lines) != 1 {
			t.Fatalf("%s: got %d entries, want 1", name, len(lines))
		}
		if got, w := string(lines[0]), strings.TrimRight(want.String(), "\n"); got != w {
			t.Errorf("%s:\n got  %s\n want %s", name, got, w)
		}
	}
}
