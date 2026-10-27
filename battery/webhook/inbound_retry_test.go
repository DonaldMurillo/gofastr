package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// eachInboundStore runs body against the memory, sqlite and postgres
// inbound stores.
func eachInboundStore(t *testing.T, body func(t *testing.T, s InboundStore)) {
	t.Helper()
	stores := []struct {
		name string
		open func(t *testing.T) InboundStore
	}{
		{"memory", func(*testing.T) InboundStore { return NewMemoryInboundStore() }},
		{"sqlite", func(t *testing.T) InboundStore { _, s := openInboundSQLStore(t); return s }},
		{"postgres", func(t *testing.T) InboundStore {
			s, err := NewSQLInboundStore(pgtest.DB(t))
			if err != nil {
				t.Fatalf("NewSQLInboundStore: %v", err)
			}
			return s
		}},
	}
	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) { body(t, tc.open(t)) })
	}
}

// A failed attempt is the queue's cue to retry, not a final state: the
// retry must show as processing, consume an attempt, and record its own
// error.
func TestFailedEnvelopeRetryIsRecorded(t *testing.T) {
	eachInboundStore(t, func(t *testing.T, s InboundStore) {
		ctx := context.Background()
		now := time.Now().UTC()
		if err := s.AddEnvelope(ctx, InboundEnvelope{ID: "e1", Source: "github", Payload: []byte(`{}`),
			Status: InboundStatusReceived, ReceivedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(inboundJobPayload{EnvelopeID: "e1", Source: "github"})
		job := queue.Job{ID: "j1", Payload: payload}

		var seen []string
		run := 0
		h := ProcessInbound(s, func(ctx context.Context, _ InboundEnvelope) error {
			run++
			cur, err := s.GetEnvelope(ctx, "e1")
			if err != nil || cur == nil {
				t.Fatalf("load inside handler: %v, %v", cur, err)
			}
			seen = append(seen, cur.Status)
			return errors.New("boom " + string(rune('0'+run)))
		})
		for range 2 {
			if err := h(ctx, job); err == nil {
				t.Fatal("handler error not returned to the queue")
			}
		}

		got, err := s.GetEnvelope(ctx, "e1")
		if err != nil || got == nil {
			t.Fatalf("read back: %v, %v", got, err)
		}
		if seen[1] != InboundStatusProcessing || got.Attempts != 2 || got.LastError != "boom 2" {
			t.Fatalf("retry saw status %q, envelope attempts=%d err=%q; want processing, 2, \"boom 2\": the failed row fenced out its own retry",
				seen[1], got.Attempts, got.LastError)
		}
	})
}

// A stale runner settles after the recovery runner's processed envelope
// was reaped by retention. The settle must not put the envelope back.
func TestStaleEnvelopeSettleAfterReap(t *testing.T) {
	eachInboundStore(t, func(t *testing.T, s InboundStore) {
		ctx := context.Background()
		now := time.Now().UTC()
		if err := s.AddEnvelope(ctx, InboundEnvelope{ID: "e1", Source: "github", Payload: []byte(`{}`),
			Status: InboundStatusReceived, ReceivedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		stale, err := s.GetEnvelope(ctx, "e1")
		if err != nil || stale == nil {
			t.Fatalf("load: %v, %v", stale, err)
		}
		done := *stale
		done.Status = InboundStatusProcessed
		if err := s.UpdateEnvelope(ctx, done); err != nil {
			t.Fatal(err)
		}
		reaper := s.(interface {
			ReapTerminalBefore(context.Context, time.Time) (int64, error)
		})
		if n, err := reaper.ReapTerminalBefore(ctx, now.Add(time.Hour)); err != nil || n != 1 {
			t.Fatalf("reap = %d, %v; want the processed envelope gone", n, err)
		}

		stale.Status = InboundStatusFailed
		stale.LastError = "late runner failure"
		if err := s.UpdateEnvelope(ctx, *stale); err != nil {
			t.Fatal(err)
		}
		if all, err := s.ListEnvelopes(ctx, "", 0); err != nil || len(all) != 0 {
			t.Fatalf("envelopes after a stale settle = %+v, %v; want none: the settle recreated a reaped envelope", all, err)
		}
	})
}
