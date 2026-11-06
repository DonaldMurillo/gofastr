package webhook

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type panicDiagnosticStore struct{ Store }

func (panicDiagnosticStore) DueDeliveries(context.Context, time.Time, int) ([]Delivery, error) {
	panic("store\nforged\x1b\u202e")
}

func (panicDiagnosticStore) GetSubscriber(context.Context, string) (*Subscriber, error) {
	panic("store\nforged\x1b\u202e")
}

func (panicDiagnosticStore) UpdateDelivery(context.Context, Delivery) error {
	panic("store\nforged\x1b\u202e")
}

func (panicDiagnosticStore) releaseClaim(context.Context, Delivery, time.Time) error {
	panic("store\nforged\x1b\u202e")
}

func TestStorePanicLogsAreScrubbed(t *testing.T) {
	for _, operation := range []string{"tick", "release", "attempt", "save"} {
		t.Run(operation, func(t *testing.T) {
			var diagnostic string
			store := panicDiagnosticStore{}
			manager := New(store, Options{Logger: func(format string, args ...any) {
				diagnostic = fmt.Sprintf(format, args...)
			}})
			delivery := Delivery{ID: "delivery-1", Status: StatusPending}
			switch operation {
			case "tick":
				manager.tick(t.Context())
			case "release":
				manager.releaseClaims(t.Context(), store, []Delivery{delivery})
			case "attempt":
				manager.attempt(t.Context(), delivery)
			case "save":
				manager.saveDelivery(delivery)
			}
			if !strings.Contains(diagnostic, "store") || !strings.Contains(diagnostic, "pending") &&
				!strings.Contains(diagnostic, "lease") || strings.ContainsAny(diagnostic, "\n\x1b\u202e") {
				t.Fatalf("missing or unsafe recovery diagnostic: %q", diagnostic)
			}
		})
	}
}
