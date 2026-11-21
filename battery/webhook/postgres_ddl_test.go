package webhook

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The outbound and inbound stores dialect-switch their timestamp columns but
// hardcoded the payload column as BLOB. Postgres has no BLOB type, so
// NewSQLStore/NewSQLInboundStore failed at ensureTables against every Postgres
// app, the battery was unusable on the dialect it is most likely deployed on.
//
// These construct against a real server rather than asserting on the DDL
// string: the property is "the emitted schema is one Postgres accepts", and
// only Postgres can decide that.

func TestSQLStoreConstructsOnPostgres(t *testing.T) {
	db := pgtest.DB(t)
	codec, err := NewAESGCMSecretCodec(make([]byte, 32))
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	store, err := NewSQLStore(db, WithSQLSecretCodec(codec))
	if err != nil {
		t.Fatalf("NewSQLStore on Postgres: %v", err)
	}

	ctx := context.Background()
	sub := Subscriber{ID: "s1", URL: "https://example.com/hook", Secret: "shh", Events: []string{"a"}, Active: true, Created: time.Now().UTC()}
	if err := store.AddSubscriber(ctx, sub); err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	// The payload column is the one that was mistyped, prove bytes survive a
	// round-trip, not only that the CREATE TABLE parsed.
	del := Delivery{ID: "d1", SubscriberID: "s1", Event: "a", Payload: []byte(`{"k":"v"}`), Status: "pending", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store.AddDelivery(ctx, del); err != nil {
		t.Fatalf("AddDelivery: %v", err)
	}
	got, err := store.ListDeliveries(ctx, "s1", 10)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(got) != 1 || string(got[0].Payload) != `{"k":"v"}` {
		t.Fatalf("payload did not round-trip: %+v", got)
	}
}

func TestSQLInboundStoreConstructsOnPostgres(t *testing.T) {
	db := pgtest.DB(t)
	store, err := NewSQLInboundStore(db)
	if err != nil {
		t.Fatalf("NewSQLInboundStore on Postgres: %v", err)
	}
	ctx := context.Background()
	env := InboundEnvelope{
		ID: "e1", Source: "stripe", DedupeKey: "evt_1",
		Payload: []byte(`{"id":"evt_1"}`), Status: "pending",
		ReceivedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := store.AddEnvelope(ctx, env); err != nil {
		t.Fatalf("AddEnvelope: %v", err)
	}
	got, err := store.GetEnvelope(ctx, "e1")
	if err != nil {
		t.Fatalf("GetEnvelope: %v", err)
	}
	if got == nil || string(got.Payload) != `{"id":"evt_1"}` {
		t.Fatalf("payload did not round-trip: %+v", got)
	}
}

func forWebhookSQL(t *testing.T, check func(*testing.T, *sql.DB, *SQLStore)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		db, store := openSQLStore(t)
		check(t, db, store)
	})
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t)
		store, err := NewSQLStore(db, WithSQLAllowPlaintext())
		if err != nil {
			t.Fatal(err)
		}
		check(t, db, store)
	})
}

func TestDueDeliveryOrderParity(t *testing.T) {
	check := func(t *testing.T, store Store) {
		ctx := context.Background()
		now := utcBase
		for _, d := range []Delivery{
			{ID: "a-due", NextAttemptAt: now.Add(-time.Second)},
			{ID: "z-due", NextAttemptAt: now.Add(-time.Minute)},
			{ID: "z-unset"},
			{ID: "a-unset"},
			{ID: "now", NextAttemptAt: now},
			{ID: "future", NextAttemptAt: now.Add(time.Hour)},
		} {
			d.SubscriberID, d.Event, d.Payload = "s", "event", []byte("{}")
			d.Status, d.CreatedAt, d.UpdatedAt = StatusPending, now, now
			if err := store.AddDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
		}

		want := []string{"a-unset", "z-unset", "z-due", "a-due"}
		due, err := store.DueDeliveries(ctx, now, len(want))
		if got := deliveryIDs(due); err != nil || !slices.Equal(got, want) {
			t.Errorf("due = %v, %v; want %v", got, err, want)
		}
		claimed, err := store.(LeasedStore).ClaimDueDeliveries(ctx, now, len(want), time.Minute)
		if got := deliveryIDs(claimed); err != nil || !slices.Equal(got, want) {
			t.Errorf("claimed = %v, %v; want %v", got, err, want)
		}
		for _, d := range claimed {
			if d.Attempts != 1 || !d.NextAttemptAt.Equal(now.Add(time.Minute)) {
				t.Errorf("claimed row lost its attempt or lease: %+v", d)
			}
		}
		due, err = store.DueDeliveries(ctx, now, 0)
		if got := deliveryIDs(due); err != nil || !slices.Equal(got, []string{"now"}) {
			t.Errorf("due after claim = %v, %v; want [now]", got, err)
		}
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemoryStore()) })
	forWebhookSQL(t, func(t *testing.T, db *sql.DB, store *SQLStore) {
		check(t, store)
		t.Run("custom table", func(t *testing.T) {
			custom, err := NewSQLStore(db, WithSQLDeliveriesTable("due"), WithSQLAllowPlaintext())
			if err != nil {
				t.Fatal(err)
			}
			check(t, custom)
		})
	})
}

func deliveryIDs(rows []Delivery) []string {
	ids := make([]string, 0, len(rows))
	for _, d := range rows {
		ids = append(ids, d.ID)
	}
	return ids
}

func TestLegacyWebhookTimesParity(t *testing.T) {
	forWebhookSQL(t, func(t *testing.T, db *sql.DB, store *SQLStore) {
		ctx := context.Background()
		inbound, err := NewSQLInboundStore(db)
		if err != nil {
			t.Fatal(err)
		}

		cutoff := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
		for _, row := range []struct {
			id, raw string
			at      time.Time
		}{
			{"old", "2026-11-19 09:00:00+00:00", cutoff.Add(-time.Hour)},
			{"recent", "2026-11-19 08:00:00-05:00", cutoff.Add(3 * time.Hour)},
		} {
			for _, status := range []DeliveryStatus{StatusSuccess, StatusDead} {
				id := row.id + "-" + string(status)
				if err := store.AddDelivery(ctx, Delivery{ID: id, SubscriberID: string(status), Event: "event",
					Payload: []byte("{}"), Status: status, CreatedAt: row.at, UpdatedAt: row.at}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("UPDATE webhook_deliveries SET created_at=$1, updated_at=$1 WHERE id=$2", row.raw, id); err != nil {
					t.Fatal(err)
				}
			}
			envelope := sampleEnvelope(row.id)
			envelope.Status, envelope.ReceivedAt, envelope.UpdatedAt = InboundStatusProcessed, row.at, row.at
			if err := inbound.AddEnvelope(ctx, envelope); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE webhook_inbound SET received_at=$1, updated_at=$1 WHERE id=$2", row.raw, row.id); err != nil {
				t.Fatal(err)
			}
		}

		store, err = NewSQLStore(db, WithSQLAllowPlaintext())
		if err != nil {
			t.Fatal(err)
		}
		inbound, err = NewSQLInboundStore(db)
		if err != nil {
			t.Fatal(err)
		}
		deliveries, err := store.ListDeliveries(ctx, string(StatusSuccess), 1)
		if got := deliveryIDs(deliveries); err != nil || !slices.Equal(got, []string{"recent-" + string(StatusSuccess)}) {
			t.Errorf("delivery list = %v, %v; want recent success", got, err)
		}
		dead, err := store.ListDeadDeliveries(ctx, 1)
		if got := deliveryIDs(dead); err != nil || !slices.Equal(got, []string{"recent-" + string(StatusDead)}) {
			t.Errorf("dead list = %v, %v; want recent dead", got, err)
		}
		envelopes, err := inbound.ListEnvelopes(ctx, string(InboundStatusProcessed), 1)
		if err != nil || len(envelopes) != 1 || envelopes[0].ID != "recent" {
			t.Errorf("envelope list = %+v, %v; want recent", envelopes, err)
		}
		if n, err := store.ReapTerminalBefore(ctx, cutoff); n != 1 || err != nil {
			t.Errorf("delivery retention deleted %d, %v; want 1", n, err)
		}
		deliveries, err = store.ListDeliveries(ctx, string(StatusSuccess), 0)
		if got := deliveryIDs(deliveries); err != nil || !slices.Equal(got, []string{"recent-" + string(StatusSuccess)}) {
			t.Errorf("deliveries after retention = %v, %v; want recent success", got, err)
		}
		if n, err := inbound.ReapTerminalBefore(ctx, cutoff); n != 1 || err != nil {
			t.Errorf("envelope retention deleted %d, %v; want 1", n, err)
		}
		if recent, err := inbound.GetEnvelope(ctx, "recent"); recent == nil || err != nil {
			t.Errorf("recent envelope disappeared: %v, %v", recent, err)
		}
	})
}

func TestWebhookWriteParity(t *testing.T) {
	forWebhookSQL(t, func(t *testing.T, db *sql.DB, _ *SQLStore) {
		ctx := context.Background()
		store, err := NewSQLStore(db, WithSQLSubscribersTable("excluded"), WithSQLAllowPlaintext())
		if err != nil {
			t.Fatal(err)
		}
		first := Subscriber{ID: "sub", URL: "https://example.com/old", Secret: "old", Active: true, Events: []string{"old"}, Created: utcBase}
		updated := Subscriber{ID: first.ID, URL: "https://example.com/new", Secret: "new", Events: []string{"new"},
			Auth: Auth{Kind: AuthBearer, Token: "new-token"}, Created: utcBase.Add(time.Hour)} // not-a-secret: test fixture token
		for _, sub := range []Subscriber{first, updated} {
			if err := store.AddSubscriber(ctx, sub); err != nil {
				t.Fatal(err)
			}
		}
		got, err := store.GetSubscriber(ctx, first.ID)
		if err != nil || got == nil || got.URL != updated.URL || got.Secret != updated.Secret ||
			got.Auth != updated.Auth || !slices.Equal(got.Events, updated.Events) || got.Active || !got.Created.Equal(first.Created) {
			t.Fatalf("upsert = %+v, %v", got, err)
		}

		d := Delivery{ID: "del", SubscriberID: first.ID, Event: "new", Payload: []byte("{}"),
			Attempts: 3, Status: StatusPending, LastError: "previous", Owner: "user",
			NextAttemptAt: utcBase, CreatedAt: utcBase.Add(-time.Minute), UpdatedAt: utcBase.Add(-time.Second),
			ResponseStatus: 503, ResponseBody: "try again"}
		if err := store.AddDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
		check := func(want Delivery) {
			t.Helper()
			rows, err := store.ListDeliveries(ctx, want.SubscriberID, 1)
			if err != nil || len(rows) != 1 {
				t.Fatalf("read delivery = %+v, %v", rows, err)
			}
			if got := rows[0]; got.ID != want.ID || got.SubscriberID != want.SubscriberID ||
				got.Event != want.Event || !slices.Equal(got.Payload, want.Payload) ||
				got.Status != want.Status || got.LastError != want.LastError || got.Owner != want.Owner ||
				got.Attempts != want.Attempts || got.ResponseStatus != want.ResponseStatus ||
				got.ResponseBody != want.ResponseBody || !got.CreatedAt.Equal(want.CreatedAt) ||
				!got.UpdatedAt.Equal(want.UpdatedAt) || !got.NextAttemptAt.Equal(want.NextAttemptAt) {
				t.Fatalf("delivery = %+v; want %+v", got, want)
			}
		}
		check(d)
		d.LastError, d.ResponseStatus, d.ResponseBody = "retry", 500, "busy"
		d.NextAttemptAt, d.UpdatedAt = utcBase.Add(time.Minute), utcBase
		d.unclaimedAttempt = true
		if err := store.UpdateDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
		d.Attempts++
		check(d)
		d.Status, d.LastError, d.ResponseStatus, d.ResponseBody = StatusSuccess, "", 200, "ok"
		d.NextAttemptAt, d.UpdatedAt = time.Time{}, utcBase.Add(time.Minute)
		d.unclaimedAttempt = false
		if err := store.UpdateDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
		check(d)
	})
}
