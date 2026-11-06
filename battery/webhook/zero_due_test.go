package webhook

import (
	"context"
	"testing"
	"time"
)

func TestZeroAttemptIsDue(t *testing.T) {
	for _, mode := range []string{"due", "claim"} {
		for _, backend := range []string{"memory", "sql"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				var store Store = NewMemoryStore()
				if backend == "sql" {
					_, store = openSQLStore(t)
				}
				ctx := context.Background()
				now := time.Now().UTC()
				for _, delivery := range []Delivery{
					{ID: "unset", Status: StatusPending},
					{ID: "cleared", Status: StatusPending, NextAttemptAt: now.Add(time.Hour)},
					{ID: "future", Status: StatusPending, NextAttemptAt: now.Add(time.Hour)},
					{ID: "dead", Status: StatusDead},
					{ID: "success", Status: StatusSuccess},
				} {
					delivery.SubscriberID, delivery.Event = "s", "event"
					delivery.Payload = []byte(`{}`)
					delivery.CreatedAt, delivery.UpdatedAt = now, now
					if err := store.AddDelivery(ctx, delivery); err != nil {
						t.Fatal(err)
					}
					if delivery.ID == "cleared" {
						delivery.NextAttemptAt = time.Time{}
						if err := store.UpdateDelivery(ctx, delivery); err != nil {
							t.Fatal(err)
						}
					}
				}
				var due []Delivery
				var err error
				if mode == "claim" {
					due, err = store.(LeasedStore).ClaimDueDeliveries(ctx, now, 10, time.Minute)
				} else {
					due, err = store.DueDeliveries(ctx, now, 10)
				}
				if err != nil {
					t.Fatal(err)
				}
				found := make(map[string]bool)
				for _, delivery := range due {
					found[delivery.ID] = true
					if mode == "claim" && (delivery.Attempts != 1 || !delivery.NextAttemptAt.Equal(now.Add(time.Minute))) {
						t.Fatalf("claim did not lease and count attempt: %+v", delivery)
					}
				}
				if len(due) != 2 || !found["unset"] || !found["cleared"] {
					t.Fatalf("due IDs = %v, want unset and cleared only", found)
				}
			})
		}
	}
}
