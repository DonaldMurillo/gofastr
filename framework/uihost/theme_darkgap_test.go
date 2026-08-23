package uihost

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// recordingHandler collects every record routed to the default logger so
// tests can assert on boot-time warnings.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(_ string) slog.Handler { return h }

func (h *recordingHandler) warns() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Record
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			out = append(out, r)
		}
	}
	return out
}

// installRecordingLogger swaps slog's default for a recorder and restores
// the original on cleanup.
func installRecordingLogger(t *testing.T) *recordingHandler {
	t.Helper()
	h := &recordingHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(old) })
	return h
}

// --- #215: partial DarkColors warns once at boot ---

func TestBootWarnsOnPartialDarkPalette(t *testing.T) {
	h := installRecordingLogger(t)

	th := theme.Default()
	delete(th.DarkColors, "surface-soft")
	delete(th.DarkColors, "code-border")

	a := app.NewApp("dark-gap-test")
	a.WithTheme(th)
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	New(a)

	warns := h.warns()
	if len(warns) != 1 {
		t.Fatalf("got %d warnings, want exactly 1 (records: %v)", len(warns), h.records)
	}
	msg := warns[0].Message
	if !strings.Contains(msg, "no dark value") || !strings.Contains(msg, "Theme.DarkColors") {
		t.Errorf("warning message = %q, want it to name the gap and the fix", msg)
	}
	var tokens []string
	warns[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "tokens" {
			if v, ok := a.Value.Any().([]string); ok {
				tokens = v
			}
		}
		return true
	})
	for _, want := range []string{"surface-soft", "code-border"} {
		if !slices.Contains(tokens, want) {
			t.Errorf("warning tokens %v omit %q", tokens, want)
		}
	}
}

func TestBootNoWarnOnCompleteDarkPalette(t *testing.T) {
	h := installRecordingLogger(t)

	a := app.NewApp("dark-complete-test")
	a.WithTheme(theme.Default())
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	New(a)

	if warns := h.warns(); len(warns) != 0 {
		t.Fatalf("complete dark palette warned at boot: %v", warns)
	}
}
