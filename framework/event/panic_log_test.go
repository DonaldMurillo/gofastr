package event

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// A panicking subscriber must not veto the write (Emit returns nil), but
// the panic must be LOGGED — a silently no-op'd "send welcome email"
// handler is otherwise undebuggable.
func TestEmitLogsSubscriberPanic(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(prev)

	eb := NewEventBus()
	eb.On(EntityCreated, func(context.Context, Event) error {
		panic("subscriber boom")
	})

	if err := eb.Emit(context.Background(), Event{Type: EntityCreated}); err != nil {
		t.Fatalf("a panicking subscriber must not fail Emit (would roll back the write), got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "panicked") || !strings.Contains(out, "subscriber boom") {
		t.Fatalf("subscriber panic was not logged: %q", out)
	}
}
