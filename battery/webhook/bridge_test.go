package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestBridge_FansEntityEventsToWebhookSubscribers(t *testing.T) {
	var (
		mu       sync.Mutex
		gotPath  string
		gotEvent string
		gotBody  map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotPath = r.URL.Path
		gotEvent = r.Header.Get("X-GoFastr-Event")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mgr := New(NewMemoryStore(), Options{
		MaxAttempts:          1,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    srv.URL + "/hook",
		Secret: "s",
		Events: []string{event.EntityCreated},
	}); err != nil {
		t.Fatal(err)
	}

	bus := event.NewEventBus()
	cancel := Bridge(bus, mgr)
	defer cancel()

	_ = bus.Emit(context.Background(), event.Event{
		Type: event.EntityCreated,
		Data: map[string]any{"id": 42, "name": "thing"},
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := gotEvent != ""
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/hook" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotEvent != event.EntityCreated {
		t.Fatalf("event: got %q want %q", gotEvent, event.EntityCreated)
	}
	if gotBody["type"] != event.EntityCreated {
		t.Fatalf("body should carry event type, got %+v", gotBody)
	}
	data, _ := gotBody["data"].(map[string]any)
	if data == nil || data["name"] != "thing" {
		t.Fatalf("body should carry event data, got %+v", gotBody)
	}
}

func TestBridge_DefaultsToEntityLifecycle(t *testing.T) {
	var hits atomic.Int32
	mgr := New(NewMemoryStore(), Options{
		MaxAttempts: 1, Backoff: []time.Duration{0}, PollInterval: 5 * time.Millisecond, AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, _ = mgr.Subscribe(context.Background(), Subscriber{URL: srv.URL, Secret: "s"})

	bus := event.NewEventBus()
	cancel := Bridge(bus, mgr) // no events arg → defaults
	defer cancel()

	_ = bus.Emit(context.Background(), event.Event{Type: event.EntityCreated})
	_ = bus.Emit(context.Background(), event.Event{Type: event.EntityUpdated})
	_ = bus.Emit(context.Background(), event.Event{Type: event.EntityDeleted})
	// Unrelated event shouldn't fan out:
	_ = bus.Emit(context.Background(), event.Event{Type: "some.other.event"})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("expected 3 deliveries (one per default event), got %d", got)
	}
}

func TestBridge_CancelUnsubscribes(t *testing.T) {
	var hits int32
	mgr := New(NewMemoryStore(), Options{
		MaxAttempts: 1, Backoff: []time.Duration{0}, PollInterval: 5 * time.Millisecond, AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer mgr.Stop(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_, _ = mgr.Subscribe(context.Background(), Subscriber{URL: srv.URL, Secret: "s"})

	bus := event.NewEventBus()
	cancel := Bridge(bus, mgr, "thing.happened")
	cancel()
	_ = bus.Emit(context.Background(), event.Event{Type: "thing.happened"})

	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("cancel should have detached bridge; got %d hits", hits)
	}
}

// A CRUD event names its record's owner (crud stamps it as ownerId), and
// the bridge queues the delivery for that owner, so erasing the user
// deletes the delivery that carries a copy of their row (2026-10-28
// review of the erasure fix: the bridge published every delivery
// ownerless).
func TestBridgeRecordsEventOwner(t *testing.T) {
	store := NewMemoryStore()
	mgr := New(store, Options{AllowPrivateNetworks: true})
	sub, err := mgr.Subscribe(context.Background(), Subscriber{URL: "https://example.com/hook", Secret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	bus := event.NewEventBus()
	defer Bridge(bus, mgr)()

	_ = bus.Emit(context.Background(), event.Event{
		Type: event.EntityCreated,
		Data: map[string]any{event.DataKeyOwnerID: "u1", "record": map[string]any{"id": 1}},
	})
	_ = bus.Emit(context.Background(), event.Event{Type: event.EntityUpdated, Data: map[string]any{"record": map[string]any{"id": 2}}})

	got := map[string]string{}
	ds, err := store.ListDeliveries(context.Background(), sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		got[d.Event] = d.Owner
	}
	if got[event.EntityCreated] != "u1" || got[event.EntityUpdated] != "" || len(got) != 2 {
		t.Fatalf("delivery owners by event = %v, want created: u1, updated: none", got)
	}
}

// A redactor that drops the owner from the payload still leaves the
// delivery tagged: the owner is read before the redactor runs.
func TestBridgeOwnerSurvivesRedactor(t *testing.T) {
	store := NewMemoryStore()
	mgr := New(store, Options{AllowPrivateNetworks: true})
	sub, err := mgr.Subscribe(context.Background(), Subscriber{URL: "https://example.com/hook", Secret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	bus := event.NewEventBus()
	defer BridgeWithOptions(bus, mgr, nil, WithBridgeRedactor(func(_ string, ev event.Event) event.Event {
		return event.Event{Type: ev.Type, Data: map[string]any{"redacted": true}}
	}))()
	_ = bus.Emit(context.Background(), event.Event{Type: event.EntityCreated, Data: map[string]any{event.DataKeyOwnerID: "u1"}})

	ds, err := store.ListDeliveries(context.Background(), sub.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Owner != "u1" {
		t.Fatalf("deliveries %+v, want one owned by u1", ds)
	}
}
