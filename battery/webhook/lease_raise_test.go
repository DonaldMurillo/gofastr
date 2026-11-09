package webhook

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// New raises a LeasePeriod no longer than the HTTP client timeout — but
// only when the store implements LeasedStore. The lease is read on the
// LeasedStore claim path alone; with any other store the raise and its
// log line were noise about a lease nothing uses (#659, second
// decision).
// unleasedStore is a Store with no LeasedStore method: the plain
// best-effort shape a single-instance host can still hand to New.
type unleasedStore struct{}

func (unleasedStore) AddSubscriber(ctx context.Context, s Subscriber) error {
	return nil
}
func (unleasedStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	return nil, nil
}
func (unleasedStore) ListSubscribers(ctx context.Context) ([]Subscriber, error) {
	return nil, nil
}
func (unleasedStore) DeleteSubscriber(ctx context.Context, id string) error {
	return nil
}
func (unleasedStore) AddDelivery(ctx context.Context, d Delivery) error {
	return nil
}
func (unleasedStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	return nil
}
func (unleasedStore) ListDeliveries(ctx context.Context, subscriberID string, limit int) ([]Delivery, error) {
	return nil, nil
}
func (unleasedStore) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	return nil, nil
}

func TestNewRaisesShortLeaseOnlyForLeasedStore(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// A LeasedStore (both shipped stores implement it) gets the raise.
	var leasedLogs strings.Builder
	m := New(NewMemoryStore(), Options{
		LeasePeriod: 5 * time.Second,
		HTTPClient:  client,
		Logger:      func(format string, args ...any) { leasedLogs.WriteString(strings.TrimSpace(format) + "\n") },
	})
	if m.opts.LeasePeriod <= client.Timeout {
		t.Fatalf("LeasedStore: lease %s was not raised past the client timeout %s", m.opts.LeasePeriod, client.Timeout)
	}
	if !strings.Contains(leasedLogs.String(), "raised") {
		t.Fatalf("LeasedStore: the raise was not logged: %q", leasedLogs.String())
	}

	// Any other store: no lease is ever claimed, so no raise and no log.
	var plainLogs strings.Builder
	m2 := New(unleasedStore{}, Options{
		LeasePeriod: 5 * time.Second,
		HTTPClient:  client,
		Logger:      func(format string, args ...any) { plainLogs.WriteString(strings.TrimSpace(format) + "\n") },
	})
	if m2.opts.LeasePeriod != 5*time.Second {
		t.Fatalf("plain store: lease %s was raised although no claim path reads it", m2.opts.LeasePeriod)
	}
	if plainLogs.Len() != 0 {
		t.Fatalf("plain store: logged %q although no claim path reads the lease", plainLogs.String())
	}
}
