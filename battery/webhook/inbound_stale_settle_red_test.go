//go:build red

package webhook

import (
	"context"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T2). Pinned sibling: TestStaleSettleCannotResurrectDelivered
// (delivery_security_test.go) implements and asserts the terminal-settle fence
// for the OUTBOUND delivery twin — its header calls UpdateDelivery's fencing
// "the store-side sibling of the queue's claim fencing"; the inbound envelope
// stores received only the attempts half of that sweep (pinned by
// TestInboundAttemptsCountEveryPass, attempts_security_test.go) and the
// terminal half never landed on them.
// Property: a stale non-terminal settle never regresses a terminal row — a
// completion written by an overrun-lease runner must not be overwritten by a
// LATER non-terminal write from the stale runner. Failed envelopes are
// retained forever as forensic records and ReapTerminalBefore only deletes
// processed rows, so a regressed row escapes retention permanently and leaves
// a false forensic record.
// Surfaces: battery/webhook/inbound_memory.go::UpdateEnvelope (status written
// unconditionally under the lock), battery/webhook/inbound_sql.go::update()
// (SET status with no terminal-status predicate on either dialect); reach:
// inbound.go::ProcessInbound :381-385 — the late failed settle after the
// queue's lease expiry re-ran the envelope's job.
// Finding: seeded received → two MarkEnvelopeProcessing passes → settle
// processed → late settle failed leaves status=failed on both stores today;
// the outbound twin of the exact same sequence stays StatusSuccess.
// Fix direction: mirror the outbound fence — refuse a non-terminal status
// write when the stored row is already terminal (isTerminalStatus + status
// predicate in the UPDATE, skip-when-terminal under the memory lock).

// TestInboundRedStaleSettleFenced: a late failed settle from a stale runner
// must not regress an envelope the recovery runner already settled processed.
func TestInboundRedStaleSettleFenced(t *testing.T) {
	_, sqlStore := openInboundSQLStore(t)
	memStore := NewMemoryInboundStore()
	for _, tc := range []struct {
		name  string
		store InboundStore
	}{
		{"sqlstore", sqlStore},
		{"memorystore", memStore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			env := InboundEnvelope{
				ID: "env-stale-settle", Source: "github", DedupeKey: "del-stale",
				Payload: []byte(`{}`), Status: InboundStatusReceived,
				ReceivedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
			if err := tc.store.AddEnvelope(ctx, env); err != nil {
				t.Fatalf("setup broken: seed envelope: %v", err)
			}

			// The documented overlapping-passes shape: runner A loads the
			// envelope and marks processing; the queue's lease expiry re-runs
			// the job under it (runner B), so two processing transitions land.
			for range 2 {
				if err := tc.store.(envelopeProcessingClaimer).MarkEnvelopeProcessing(ctx, env.ID); err != nil {
					t.Fatalf("setup broken: mark processing: %v", err)
				}
			}

			// Runner A's stale view, captured before any settle lands.
			stale, err := tc.store.GetEnvelope(ctx, env.ID)
			if err != nil || stale == nil {
				t.Fatalf("setup broken: reload stale view: %v %v", stale, err)
			}

			// Runner B finishes first and records the terminal state.
			done := *stale
			done.Status = InboundStatusProcessed
			done.UpdatedAt = time.Now().UTC()
			if err := tc.store.UpdateEnvelope(ctx, done); err != nil {
				t.Fatalf("setup broken: settle processed: %v", err)
			}

			// Runner A finally wakes and settles a FAILURE for the same row —
			// the ProcessInbound late-failed-settle write (inbound.go:381-385).
			late := *stale
			late.Status = InboundStatusFailed
			late.LastError = "late runner failure"
			late.UpdatedAt = time.Now().UTC()
			if err := tc.store.UpdateEnvelope(ctx, late); err != nil {
				t.Fatalf("setup broken: late settle: %v", err)
			}

			got, err := tc.store.GetEnvelope(ctx, env.ID)
			if err != nil || got == nil {
				t.Fatalf("setup broken: read back: %v %v", got, err)
			}
			if got.Status != InboundStatusProcessed || got.Attempts != 2 {
				t.Errorf("SECURITY: [inbound-stale-settle] stale claimant's late failed settle regressed a "+
					"processed envelope to status=%q attempts=%d lastError=%q: the terminal record must win "+
					"(the outbound delivery twin fences this exact sequence), and a failed row is retained "+
					"forever while ReapTerminalBefore only deletes processed rows — the regressed envelope "+
					"escapes retention permanently and the false failure record poisons the post-mortem",
					got.Status, got.Attempts, got.LastError)
			}

			// Control 1 — the fence is a terminal-status predicate, not a
			// blanket reject: a fresh non-terminal envelope still accepts a
			// failed settle (the queue's retry path depends on it).
			ctl := env
			ctl.ID = "env-stale-settle-ctl"
			ctl.DedupeKey = "del-stale-ctl"
			if err := tc.store.AddEnvelope(ctx, ctl); err != nil {
				t.Fatalf("setup broken: seed control envelope: %v", err)
			}
			if err := tc.store.(envelopeProcessingClaimer).MarkEnvelopeProcessing(ctx, ctl.ID); err != nil {
				t.Fatalf("setup broken: control mark processing: %v", err)
			}
			ctlNow, err := tc.store.GetEnvelope(ctx, ctl.ID)
			if err != nil || ctlNow == nil {
				t.Fatalf("setup broken: control reload: %v %v", ctlNow, err)
			}
			ctlFail := *ctlNow
			ctlFail.Status = InboundStatusFailed
			ctlFail.LastError = "handler error"
			ctlFail.UpdatedAt = time.Now().UTC()
			if err := tc.store.UpdateEnvelope(ctx, ctlFail); err != nil {
				t.Fatalf("setup broken: control settle failed: %v", err)
			}
			ctlGot, err := tc.store.GetEnvelope(ctx, ctl.ID)
			if err != nil || ctlGot == nil {
				t.Fatalf("setup broken: control read back: %v %v", ctlGot, err)
			}
			if ctlGot.Status != InboundStatusFailed || ctlGot.Attempts != 1 || ctlGot.LastError != "handler error" {
				t.Errorf("SECURITY: [inbound-stale-settle] control: fresh envelope after one processing pass "+
					"and a failed settle is status=%q attempts=%d err=%q; want failed/1/handler error — the "+
					"terminal fence must not turn into a blanket reject that buries the retry path",
					ctlGot.Status, ctlGot.Attempts, ctlGot.LastError)
			}
		})
	}
}
