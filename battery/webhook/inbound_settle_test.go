package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// A shutdown that lands as the business handler finishes must not drop
// the envelope's outcome: the write ran on the job's context, failed once
// it was cancelled, and left the envelope "processing" for work that had
// already happened (2026-10-27 bug bash, settlectx).
func TestInboundOutcomeSurvivesCancel(t *testing.T) {
	for name, tc := range map[string]struct {
		outcome error
		want    string
	}{
		"processed": {nil, InboundStatusProcessed},
		"failed":    {errors.New("boom"), InboundStatusFailed},
	} {
		t.Run(name, func(t *testing.T) {
			_, s := openInboundSQLStore(t)
			now := time.Now().UTC()
			if err := s.AddEnvelope(context.Background(), InboundEnvelope{ID: "e1", Source: "github", Payload: []byte(`{}`),
				Status: InboundStatusReceived, ReceivedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(inboundJobPayload{EnvelopeID: "e1", Source: "github"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := ProcessInbound(s, func(context.Context, InboundEnvelope) error {
				cancel() // shutdown arrives as the work finishes
				return tc.outcome
			}, WithProcessInboundLogger(func(string, ...any) {}))
			_ = h(ctx, queue.Job{ID: "j1", Payload: payload})

			got, err := s.GetEnvelope(context.Background(), "e1")
			if err != nil || got == nil {
				t.Fatalf("read back: %v, %v", got, err)
			}
			if got.Status != tc.want {
				t.Fatalf("status = %q after cancel, want %q: the outcome write was dropped", got.Status, tc.want)
			}
		})
	}
}
