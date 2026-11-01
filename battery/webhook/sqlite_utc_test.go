package webhook

import (
	"context"
	"testing"
	"time"
)

// SQLite stores a timestamp as text with its offset, and the claim
// compares next_attempt_at as text (2026-10-27 bug bash B26). Two
// workers whose clocks carry different zones, or one clock across a
// DST change, read each other's leases and due times hours off. The
// store binds every timestamp in UTC.

var (
	utcBase = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	zoneW   = time.FixedZone("W", -10*3600)
	zoneE   = time.FixedZone("E", 10*3600)
)

func addPendingAt(t *testing.T, s *SQLStore, id string, at time.Time) {
	t.Helper()
	if err := s.AddDelivery(context.Background(), Delivery{ID: id, SubscriberID: "s", Event: "e", Payload: []byte("p"),
		Status: StatusPending, NextAttemptAt: at, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteClaimAcrossZones(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	addPendingAt(t, s, "d1", utcBase.In(zoneE))

	// Due one second ago, read by a worker ten hours west.
	got, err := s.ClaimDueDeliveries(ctx, utcBase.Add(time.Second).In(zoneW), 10, time.Minute)
	if err != nil || len(got) != 1 {
		t.Fatalf("west worker claimed %v, %v; want the due delivery", got, err)
	}
	// Its lease runs a minute; a worker ten hours east looks a second later.
	again, err := s.ClaimDueDeliveries(ctx, utcBase.Add(2*time.Second).In(zoneE), 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("east worker re-claimed %s inside the west worker's lease", again[0].ID)
	}
	if due, err := s.DueDeliveries(ctx, utcBase.Add(2*time.Second).In(zoneE), 10); err != nil || len(due) != 0 {
		t.Fatalf("DueDeliveries inside the lease = %v, %v; want none", due, err)
	}
}

func TestSQLiteFencesHoldWithZonedClock(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	addPendingAt(t, s, "d1", utcBase)
	addPendingAt(t, s, "d2", utcBase)
	got, err := s.ClaimDueDeliveries(ctx, utcBase.In(zoneW), 10, time.Minute)
	if err != nil || len(got) != 2 {
		t.Fatalf("claim = %v, %v", got, err)
	}
	byID := map[string]Delivery{got[0].ID: got[0], got[1].ID: got[1]}

	// A retry settle is fenced on the lease the claim wrote.
	d := byID["d1"]
	d.Status = StatusPending
	d.LastError = "http 500"
	d.NextAttemptAt = utcBase.Add(time.Hour).In(zoneW)
	d.UpdatedAt = utcBase.In(zoneW)
	if err := s.UpdateDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	// So is the release of a claim the worker never started.
	if err := s.releaseClaim(ctx, byID["d2"], utcBase.Add(time.Second).In(zoneE)); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDeliveries(ctx, "s", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range list {
		switch row.ID {
		case "d1":
			if row.LastError != "http 500" || !row.NextAttemptAt.Equal(utcBase.Add(time.Hour)) {
				t.Errorf("d1 retry settle missed its fence: %+v", row)
			}
		case "d2":
			if row.Attempts != 0 || !row.NextAttemptAt.Equal(utcBase.Add(time.Second)) {
				t.Errorf("d2 release missed its fence: %+v", row)
			}
		}
	}

	// The released claim is due again a second later, read from the west;
	// the retry is not due for an hour, read from the east.
	got, err = s.ClaimDueDeliveries(ctx, utcBase.Add(2*time.Second).In(zoneW), 10, 2*time.Hour)
	if err != nil || len(got) != 1 || got[0].ID != "d2" {
		t.Fatalf("claim after the release = %v, %v; want d2 alone", got, err)
	}
	got, err = s.ClaimDueDeliveries(ctx, utcBase.Add(30*time.Minute).In(zoneE), 10, time.Minute)
	if err != nil || len(got) != 0 {
		t.Fatalf("claim before the retry is due = %v, %v; want none", got, err)
	}
}

func TestSQLiteListOrderAcrossZones(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	addPendingAt(t, s, "older", utcBase.In(zoneE))
	addPendingAt(t, s, "newer", utcBase.Add(time.Minute).In(zoneW))
	list, err := s.ListDeliveries(ctx, "s", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if list[0].ID != "newer" {
		t.Fatalf("newest-first list starts with %s", list[0].ID)
	}
}

func TestSQLiteReapAcrossZones(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	if err := s.AddDelivery(ctx, Delivery{ID: "done", SubscriberID: "s", Event: "e", Payload: []byte("p"),
		Status: StatusSuccess, CreatedAt: utcBase.In(zoneE), UpdatedAt: utcBase.In(zoneE)}); err != nil {
		t.Fatal(err)
	}
	// A row the settle write marks delivered, stamped in the same zone.
	addPendingAt(t, s, "settled", utcBase)
	if err := s.UpdateDelivery(ctx, Delivery{ID: "settled", Status: StatusSuccess, UpdatedAt: utcBase.In(zoneE)}); err != nil {
		t.Fatal(err)
	}
	// An hour before the rows settled: nothing is old enough.
	n, err := s.ReapTerminalBefore(ctx, utcBase.Add(-time.Hour).In(zoneW))
	if err != nil || n != 0 {
		t.Fatalf("reap before the rows settled = %d, %v; want 0", n, err)
	}
	n, err = s.ReapTerminalBefore(ctx, utcBase.Add(time.Hour).In(zoneW))
	if err != nil || n != 2 {
		t.Fatalf("reap after the rows settled = %d, %v; want 2", n, err)
	}
}

func TestSQLiteInboundAcrossZones(t *testing.T) {
	_, s := openInboundSQLStore(t)
	ctx := context.Background()
	older := sampleEnvelope("older")
	older.ReceivedAt, older.UpdatedAt = utcBase.In(zoneE), utcBase.In(zoneE)
	newer := sampleEnvelope("newer")
	newer.ReceivedAt, newer.UpdatedAt = utcBase.Add(time.Minute).In(zoneW), utcBase.Add(time.Minute).In(zoneW)
	for _, e := range []InboundEnvelope{older, newer} {
		if err := s.AddEnvelope(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListEnvelopes(ctx, "", 0)
	if err != nil || len(list) != 2 || list[0].ID != "newer" {
		t.Fatalf("newest-first envelopes = %v, %v", list, err)
	}

	// Both settle processed, and a third is stored processed, all stamped
	// east; a cutoff an hour later read from the west reaps all three, and
	// one an hour earlier reaps none.
	for _, e := range []InboundEnvelope{older, newer} {
		e.Status = InboundStatusProcessed
		e.UpdatedAt = utcBase.Add(time.Minute).In(zoneE)
		if err := s.UpdateEnvelope(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	direct := sampleEnvelope("direct")
	direct.Status = InboundStatusProcessed
	direct.ReceivedAt, direct.UpdatedAt = utcBase.In(zoneE), utcBase.In(zoneE)
	if err := s.AddEnvelope(ctx, direct); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReapTerminalBefore(ctx, utcBase.Add(-time.Hour).In(zoneW)); err != nil || n != 0 {
		t.Fatalf("reap before the envelopes settled = %d, %v; want 0", n, err)
	}
	if n, err := s.ReapTerminalBefore(ctx, utcBase.Add(time.Hour).In(zoneW)); err != nil || n != 3 {
		t.Fatalf("reap after the envelopes settled = %d, %v; want 3", n, err)
	}
}

// A database an earlier release wrote holds pending rows in the writer's
// zone. Opening the store rewrites their due times in UTC, so an upgrade
// east of UTC does not hold every queued retry back by the offset.
func TestSQLiteStoreRewritesZonedDueTimes(t *testing.T) {
	db, s := openSQLStore(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, s.deliveryInsert(), "old", "s", "e", []byte("p"), 0,
		string(StatusPending), "", utcBase.In(zoneE), utcBase.In(zoneE), utcBase.In(zoneE), "", 0, ""); err != nil {
		t.Fatal(err)
	}
	s2, err := NewSQLStore(db, WithSQLAllowPlaintext())
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.ClaimDueDeliveries(ctx, utcBase.Add(time.Second), 10, time.Minute)
	if err != nil || len(got) != 1 {
		t.Fatalf("claim after reopen = %v, %v; want the row an earlier release queued", got, err)
	}

	// Another process claimed the row between the read and the rewrite:
	// the rewrite of the text it read must not move the lease back.
	if err := s2.rewriteDueTime("old", "2026-10-30 22:00:00+10:00", utcBase); err != nil {
		t.Fatal(err)
	}
	if again, err := s2.ClaimDueDeliveries(ctx, utcBase.Add(2*time.Second), 10, time.Minute); err != nil || len(again) != 0 {
		t.Fatalf("claim inside the lease = %v, %v; a stale rewrite moved the lease back", again, err)
	}
}
