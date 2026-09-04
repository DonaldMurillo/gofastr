package webhook

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func openSQLStore(t *testing.T) (*sql.DB, *SQLStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLStore(db, WithSQLAllowPlaintext())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return db, s
}

// TestSQLStore_RefusesPlaintextWithoutOptIn verifies that NewSQLStore
// fails fast when neither WithSQLSecretCodec(...) nor
// WithSQLAllowPlaintext() is supplied, silently storing subscriber
// secrets in cleartext was the previous default and is exactly the
// kind of accidental cleartext store that the agent's audit flagged.
func TestSQLStore_RefusesPlaintextWithoutOptIn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := NewSQLStore(db); err == nil {
		t.Errorf("SECURITY: [webhook_secret] NewSQLStore(db) succeeded without an explicit codec or plaintext opt-in. Attack: subscriber secrets persisted in cleartext.")
	}
}

func TestSQLStore_SubscriberRoundTrip(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	sub := Subscriber{
		ID:      "sub-1",
		URL:     "https://example.com/hook",
		Secret:  "topsecret",
		Events:  []string{"orders.*"},
		Active:  true,
		Created: time.Now().Truncate(time.Second),
	}
	if err := s.AddSubscriber(ctx, sub); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := s.GetSubscriber(ctx, "sub-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.URL != sub.URL || got.Secret != sub.Secret || !got.Active || len(got.Events) != 1 || got.Events[0] != "orders.*" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestSQLStore_SubscriberListAndDelete(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		_ = s.AddSubscriber(ctx, Subscriber{ID: id, URL: "u", Secret: "s", Active: true, Created: time.Now()})
	}
	list, _ := s.ListSubscribers(ctx)
	if len(list) != 3 {
		t.Fatalf("list: got %d", len(list))
	}
	_ = s.DeleteSubscriber(ctx, "b")
	list, _ = s.ListSubscribers(ctx)
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "c" {
		t.Fatalf("list after delete: %+v", list)
	}
}

func TestSQLStore_DeliveryDueQuery(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	now := time.Now()
	rows := []Delivery{
		{ID: "ready", SubscriberID: "x", Event: "e", Payload: []byte("{}"), Status: StatusPending, NextAttemptAt: now.Add(-time.Second), CreatedAt: now, UpdatedAt: now},
		{ID: "later", SubscriberID: "x", Event: "e", Payload: []byte("{}"), Status: StatusPending, NextAttemptAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "success", SubscriberID: "x", Event: "e", Payload: []byte("{}"), Status: StatusSuccess, CreatedAt: now, UpdatedAt: now},
	}
	for _, d := range rows {
		if err := s.AddDelivery(ctx, d); err != nil {
			t.Fatalf("add %s: %v", d.ID, err)
		}
	}

	due, err := s.DueDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].ID != "ready" {
		t.Fatalf("expected only 'ready' due, got %+v", due)
	}
}

// TestSQLStore_DeliveryUpdateRoundTrip pins the settle write's column
// contract: status/last_error/next_attempt_at/updated_at round-trip, while
// the attempts column is owned by the claim (ClaimDueDeliveries consumes
// one per claim) and a settle must neither change nor rewind it.
func TestSQLStore_DeliveryUpdateRoundTrip(t *testing.T) {
	_, s := openSQLStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	d := Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"), Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.AddDelivery(ctx, d); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Two claims consume two attempts (the crash-recovery shape: the
	// first claim's lease expires, a second worker re-claims).
	first, err := s.ClaimDueDeliveries(ctx, now, 1, time.Minute)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v (n=%d)", err, len(first))
	}
	second, err := s.ClaimDueDeliveries(ctx, now.Add(2*time.Minute), 1, time.Minute)
	if err != nil || len(second) != 1 {
		t.Fatalf("recovery claim: %v (n=%d)", err, len(second))
	}
	if first[0].Attempts != 1 || second[0].Attempts != 2 {
		t.Fatalf("claim snapshots = %d, %d; want 1 then 2 (one attempt consumed per claim)",
			first[0].Attempts, second[0].Attempts)
	}
	// The settle carries a stale snapshot's Attempts; the store must ignore it.
	d.Attempts = 99
	d.Status = StatusDead
	d.LastError = "boom"
	d.UpdatedAt = now.Add(time.Minute)
	d.NextAttemptAt = time.Time{} // dead: no future attempt
	if err := s.UpdateDelivery(ctx, d); err != nil {
		t.Fatalf("update: %v", err)
	}

	list, err := s.ListDeliveries(ctx, "s", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Attempts != 2 || list[0].Status != StatusDead || list[0].LastError != "boom" || !list[0].NextAttemptAt.IsZero() {
		t.Fatalf("update roundtrip: %+v (want attempts=2 from the two claims, dead, boom, no next attempt)", list)
	}
}

func TestManager_DrivenBySQLStore_EndToEnd(t *testing.T) {
	_, store := openSQLStore(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mgr := New(store, Options{
		MaxAttempts:          3,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		AllowPrivateNetworks: true,
	})
	mgr.Start()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := mgr.Stop(stopCtx); err != nil {
			t.Errorf("manager stop: %v", err)
		}
	}()

	ctx := context.Background()
	if _, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "orders.created", []byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}

	// Leave scheduler slack for Windows when the repository suite is running
	// many package processes concurrently; PollInterval is not a delivery SLA.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatalf("SQL-backed manager never delivered")
	}
}

func TestSQLStore_UnsafeTableRejected(t *testing.T) {
	db, _ := openSQLStore(t)
	if _, err := NewSQLStore(db, WithSQLSubscribersTable("bad name")); err == nil {
		t.Fatal("expected unsafe table to be rejected")
	}
	if _, err := NewSQLStore(db, WithSQLDeliveriesTable("bad name")); err == nil {
		t.Fatal("expected unsafe deliveries table to be rejected")
	}
}
