package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A claimant past the budget dead-letters without sending while the
// claimant it outran is still mid-send. When that send succeeds first,
// the late dead write must not overwrite it: the event was delivered,
// and a dead row invites a replay that POSTs it again.
func TestDeadNeverOverwritesSuccess(t *testing.T) {
	stores := []struct {
		name string
		open func(t *testing.T) Store
	}{
		{"memory", func(*testing.T) Store { return NewMemoryStore() }},
		{"sqlite", func(t *testing.T) Store { _, s := openSQLStore(t); return s }},
		{"postgres", func(t *testing.T) Store {
			s, err := NewSQLStore(pgtest.DB(t), WithSQLAllowPlaintext())
			if err != nil {
				t.Fatalf("NewSQLStore: %v", err)
			}
			return s
		}},
	}
	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.open(t)
			ctx := context.Background()
			now := time.Now().Truncate(time.Second)
			d := Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
				Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
			if err := s.AddDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			d.Status, d.NextAttemptAt = StatusSuccess, time.Time{}
			if err := s.UpdateDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			d.Status, d.LastError = StatusDead, "attempt budget exhausted"
			if err := s.UpdateDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			list, err := s.ListDeliveries(ctx, "s", 10)
			if err != nil || len(list) != 1 {
				t.Fatalf("list = %v, %v", list, err)
			}
			if list[0].Status != StatusSuccess || list[0].LastError != "" {
				t.Errorf("row = %s %q, want the recorded success kept", list[0].Status, list[0].LastError)
			}
			// The other order: the delivery lands after the dead-letter
			// and replaces it.
			d.SubscriberID = "s2"
			d.ID, d.Status = "d2", StatusPending
			if err := s.AddDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			d.Status = StatusDead
			if err := s.UpdateDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			d.Status, d.LastError = StatusSuccess, ""
			if err := s.UpdateDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
			if list, err := s.ListDeliveries(ctx, "s2", 10); err != nil || len(list) != 1 || list[0].Status != StatusSuccess {
				t.Errorf("late delivery over a dead row = %v, %v; want success", list, err)
			}
		})
	}
}
