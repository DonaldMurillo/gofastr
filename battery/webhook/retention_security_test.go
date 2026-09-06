package webhook

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Terminal-row retention — pinned 2026-09-05 (adversarial pass round 4;
// promoted from that round's red probe once the manager shipped the
// opt-in sweep). The round-4 decision (Q4): an outbox-style OPT-IN knob
// (Options.Retention, zero/off by default), not the queue battery's
// default-on window.
// Family: F19 State accumulation by cheap or unauthenticated writes.
// Property: with Options.Retention set, terminal, fully-settled webhook
// rows (successful deliveries, processed inbound envelopes) are reaped
// once older than the window — they can never become deliverable again,
// so keeping them forever only grows the table. Dead deliveries and
// failed envelopes are deliberately NOT reaped: Replay/forensics need the
// body, retention is not a dead-letter TTL.
// Surfaces: Manager.tick → sweepTerminal (the battery's only periodic
// pass), SQLStore/MemoryStore ReapTerminalBefore (success deliveries),
// SQLInboundStore/MemoryInboundStore ReapTerminalBefore (processed
// envelopes, swept via Options.InboundStore).
// ============================================================================

// TestTerminalRowsReapedAfterWindow seeds terminal rows older than a 30-day
// retention window, runs the manager's periodic pass, and asserts they are gone.
func TestTerminalRowsReapedAfterWindow(t *testing.T) {
	ctx := context.Background()
	old := time.Now().UTC().Add(-45 * 24 * time.Hour).Truncate(time.Second)

	db, store := openSQLStore(t)
	sub := Subscriber{
		ID: "sub-old", URL: "https://example.com/hook", Secret: "s",
		Events: []string{"*"}, Active: true, Created: old,
	}
	if err := store.AddSubscriber(ctx, sub); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	if err := store.AddDelivery(ctx, Delivery{
		ID: "d-old", SubscriberID: sub.ID, Event: "evt.old", Payload: []byte(`{}`),
		Status: StatusSuccess, NextAttemptAt: time.Time{},
		CreatedAt: old, UpdatedAt: old,
	}); err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE webhook_deliveries SET created_at = ?, updated_at = ?, next_attempt_at = NULL WHERE id = ?`,
		old, old, "d-old"); err != nil {
		t.Fatalf("age delivery: %v", err)
	}

	idb, inbound := openInboundSQLStore(t)
	if err := inbound.AddEnvelope(ctx, InboundEnvelope{
		ID: "env-old", Source: "github", Payload: []byte(`{}`),
		Status: InboundStatusProcessed, ReceivedAt: old, UpdatedAt: old,
	}); err != nil {
		t.Fatalf("seed envelope: %v", err)
	}
	if _, err := idb.Exec(
		`UPDATE webhook_inbound SET received_at = ?, updated_at = ? WHERE id = ?`,
		old, old, "env-old"); err != nil {
		t.Fatalf("age envelope: %v", err)
	}

	// Drive every periodic pass the battery has — the worker tick — with the
	// retention knob ON (the queue battery's own 30-day default window as the
	// size under test) and the inbound store wired so the same pass sweeps it.
	mgr := New(store, Options{
		AllowPrivateNetworks: true,
		Retention:            30 * 24 * time.Hour,
		InboundStore:         inbound,
	})
	for range 3 {
		mgr.tick(ctx)
	}

	rows, err := store.ListDeliveries(ctx, sub.ID, 100)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("SECURITY: [webhook] a successful delivery from %v survived 3 worker ticks: no retention "+
			"sweep exists, so every Publish adds a permanent row per subscriber and the table grows until the "+
			"disk fills (battery/queue reaps terminal occurrences at 30d by default; outbox ships WithRetention)",
			old.UTC())
	}
	envs, err := inbound.ListEnvelopes(ctx, "", 0)
	if err != nil {
		t.Fatalf("list envelopes: %v", err)
	}
	for _, e := range envs {
		if e.ID == "env-old" {
			t.Errorf("SECURITY: [webhook] a processed inbound envelope from %v survives with no reaper: every "+
				"verified third-party POST writes a permanent row (payload up to 1 MiB) and nothing ever deletes it",
				old.UTC())
		}
	}
}
