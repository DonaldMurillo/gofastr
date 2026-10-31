package webhook

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// staleMemoryStore serves a GetEnvelope snapshot taken before another
// runner settled the envelope: the redelivered job loaded it, then the
// first runner's processed write landed before the processing claim.
type staleMemoryStore struct {
	*MemoryInboundStore
	stale InboundEnvelope
}

func (s staleMemoryStore) GetEnvelope(context.Context, string) (*InboundEnvelope, error) {
	e := s.stale
	return &e, nil
}

type staleSQLStore struct {
	*SQLInboundStore
	stale InboundEnvelope
}

func (s staleSQLStore) GetEnvelope(context.Context, string) (*InboundEnvelope, error) {
	e := s.stale
	return &e, nil
}

// plainInboundStore hides the optional processing claim, so ProcessInbound
// takes its UpdateEnvelope fallback.
type plainInboundStore struct{ InboundStore }

// A redelivered job for an envelope already processed does not run the
// business handler again: a crash before the queue ack, or a lease expiry
// under a slow first runner, redelivers the job, and the processing claim
// fenced the status write but the handler still ran (2026-10-27 bug bash
// B66). Covers both a processed snapshot and one that turned processed
// between the load and the claim.
func TestProcessedEnvelopeSkipsHandler(t *testing.T) {
	now := time.Now().UTC()
	env := InboundEnvelope{ID: "e1", Source: "github", Payload: []byte(`{}`),
		Status: InboundStatusProcessed, Attempts: 1, ReceivedAt: now, UpdatedAt: now}
	stale := env
	stale.Status = InboundStatusReceived
	stale.Attempts = 0

	stores := map[string]func(t *testing.T) InboundStore{
		"memory": func(t *testing.T) InboundStore { return NewMemoryInboundStore() },
		"sql": func(t *testing.T) InboundStore {
			_, s := openInboundSQLStore(t)
			return s
		},
		"store without the claim": func(t *testing.T) InboundStore {
			return plainInboundStore{NewMemoryInboundStore()}
		},
		"memory stale load": func(t *testing.T) InboundStore {
			return staleMemoryStore{NewMemoryInboundStore(), stale}
		},
		"sql stale load": func(t *testing.T) InboundStore {
			_, s := openInboundSQLStore(t)
			return staleSQLStore{s, stale}
		},
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			ctx := context.Background()
			if err := s.AddEnvelope(ctx, env); err != nil {
				t.Fatal(err)
			}
			if runs := runRedelivered(t, s); runs != 0 {
				t.Fatalf("the business handler ran %d times for a processed envelope", runs)
			}
		})
	}
}

// An envelope deleted between the job's load and its processing claim
// is not handled from the stale snapshot.
func TestDeletedEnvelopeSkipsHandler(t *testing.T) {
	now := time.Now().UTC()
	stale := InboundEnvelope{ID: "e1", Source: "github", Payload: []byte(`{}`),
		Status: InboundStatusReceived, ReceivedAt: now, UpdatedAt: now}
	stores := map[string]func(t *testing.T) InboundStore{
		"memory": func(t *testing.T) InboundStore {
			return staleMemoryStore{NewMemoryInboundStore(), stale}
		},
		"sql": func(t *testing.T) InboundStore {
			_, s := openInboundSQLStore(t)
			return staleSQLStore{s, stale}
		},
	}
	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			if runs := runRedelivered(t, open(t)); runs != 0 {
				t.Fatalf("the business handler ran %d times for a deleted envelope", runs)
			}
		})
	}
}

// runRedelivered runs one job for envelope e1 through ProcessInbound,
// expects it acked, and reports how many times the handler ran.
func runRedelivered(t *testing.T, s InboundStore) int {
	t.Helper()
	runs := 0
	h := ProcessInbound(s, func(context.Context, InboundEnvelope) error {
		runs++
		return nil
	}, WithProcessInboundLogger(func(string, ...any) {}))
	payload, _ := json.Marshal(inboundJobPayload{EnvelopeID: "e1", Source: "github"})
	if err := h(context.Background(), queue.Job{ID: "j1", Payload: payload}); err != nil {
		t.Fatalf("redelivered job = %v, want nil so the queue acks it", err)
	}
	return runs
}
