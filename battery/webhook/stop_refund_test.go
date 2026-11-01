package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Stop cancelling a POST mid-flight is a shutdown, not a failed
// attempt: the delivery goes back to pending with the attempt
// refunded. Counted as a failure, a delivery on its last attempt was
// dead-lettered by a restart.
func TestStopMidPOSTRefundsTheAttempt(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          1,
		Backoff:              []time.Duration{0},
		PollInterval:         5 * time.Millisecond,
		HTTPClient:           &http.Client{Timeout: 30 * time.Second},
		AllowPrivateNetworks: true,
	})
	ctx := context.Background()
	sub, err := mgr.Subscribe(ctx, Subscriber{URL: srv.URL, Secret: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Publish(ctx, "x", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	mgr.Start()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the delivery was never sent")
	}
	if err := mgr.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	ds, err := store.ListDeliveries(ctx, sub.ID, 10)
	if err != nil || len(ds) != 1 {
		t.Fatalf("deliveries %v, %v; want one", ds, err)
	}
	if d := ds[0]; d.Status != StatusPending || d.Attempts != 0 {
		t.Fatalf("after a shutdown mid-POST the delivery is %s with %d attempts (%q); want pending with the attempt refunded", d.Status, d.Attempts, d.LastError)
	}
	if n := mgr.FailuresTotal(); n != 0 {
		t.Errorf("a shutdown counted %d failures", n)
	}
}
