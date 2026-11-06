package event_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestBridgeMarshalLogKeepsPublishing(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)

	buses, _, stop := attachBuses(t, 2)
	defer stop()
	origin, remote := buses[0], buses[1]
	delivered := make(chan struct{}, 1)
	remote.On("valid", func(context.Context, event.Event) error {
		delivered <- struct{}{}
		return nil
	})
	for _, data := range []any{make(chan int), func() {}} {
		if err := origin.Emit(context.Background(), event.Event{Type: "invalid", Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	if err := origin.Emit(context.Background(), event.Event{Type: "valid", Data: "still published"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("valid event after marshal failures was not published")
	}
	message := output.String()
	if strings.Count(message, "non-marshalable") != 1 || strings.Contains(message, "disabled") || !strings.Contains(message, "later events") || !strings.Contains(message, "logging once") {
		t.Fatalf("marshal log must describe a per-event skip and log-once policy: %s", message)
	}
}
