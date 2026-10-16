package webhook

import (
	"context"
	"testing"
)

func TestOutboundMemoryStoreOwnsRecords(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	events := []string{"a"}
	m.AddSubscriber(ctx, Subscriber{ID: "s", Events: events})
	events[0] = "b"
	s, _ := m.GetSubscriber(ctx, "s")
	if s.Events[0] != "a" {
		t.Error("input aliases Events")
	}
	s.Events[0] = "c"
	s, _ = m.GetSubscriber(ctx, "s")
	if s.Events[0] != "a" {
		t.Error("output aliases Events")
	}
	p := []byte("ok")
	m.AddDelivery(ctx, Delivery{ID: "d", Payload: p})
	p[0] = 'X'
	ds, _ := m.ListDeliveries(ctx, "", 1)
	if string(ds[0].Payload) != "ok" {
		t.Error("input aliases Payload")
	}
	ds[0].Payload[0] = 'Y'
	ds, _ = m.ListDeliveries(ctx, "", 1)
	if string(ds[0].Payload) != "ok" {
		t.Error("output aliases Payload")
	}
}
