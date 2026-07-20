package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

const legacyLayout = "2006-01-02 15:04:05.999999999-07:00"

func legacyLeaseOutbox(t *testing.T, claimedUntil time.Time) (*Outbox, chan struct{}) {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	o, err := New(db, WithHandlerGrace(0), WithPollInterval(5*time.Millisecond))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}

	created := time.Now().UTC().Add(-time.Minute).Format(legacyLayout)
	if _, err := db.Exec(`INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('row1', 'evt', '{}', 'pending', 0, $1)`, created); err != nil {
		t.Fatalf("insert legacy event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO event_outbox_delivery (row_id, consumer, status, attempts, created_at, claimed_until)
		VALUES ('row1', 'legacy-consumer', 'pending', 0, $1, $2)`,
		created, claimedUntil.UTC().Format(legacyLayout)); err != nil {
		t.Fatalf("insert legacy delivery: %v", err)
	}

	called := make(chan struct{}, 1)
	o.Consume("legacy-consumer", "evt", func(context.Context, event.Event) error {
		called <- struct{}{}
		return nil
	})
	return o, called
}

// A delivery whose legacy-format (space-separated) lease is still in the
// future must not be reclaimed by the relay.
func TestLegacyActiveLeaseNotReclaimed(t *testing.T) {
	o, called := legacyLeaseOutbox(t, time.Now().UTC().Add(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := o.StartRelay(ctx)
	defer stop()

	select {
	case <-called:
		t.Fatal("handler called while a future lease is active")
	case <-time.After(500 * time.Millisecond):
	}
}

// A delivery whose legacy-format lease has expired is picked up again.
func TestLegacyExpiredLeaseDelivers(t *testing.T) {
	o, called := legacyLeaseOutbox(t, time.Now().UTC().Add(-time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := o.StartRelay(ctx)
	defer stop()

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("expired legacy lease was never redelivered")
	}
}
