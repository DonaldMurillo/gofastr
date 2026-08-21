package webhook

import (
	"context"
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
