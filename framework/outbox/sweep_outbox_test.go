package outbox

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestConsumeRefusesBadDeclarations(t *testing.T) {
	_, o := openOutbox(t)
	ok := func(context.Context, event.Event) error { return nil }
	o.Consume("c", "t", ok)
	cases := map[string]func(){
		"empty name":  func() { o.Consume("", "t", ok) },
		"empty type":  func() { o.Consume("c2", "", ok) },
		"nil handler": func() { o.Consume("c2", "t", nil) },
		"duplicate":   func() { o.Consume("c", "t", ok) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Consume with %s did not panic", name)
				}
			}()
			call()
		})
	}
	if got := o.declaredFor("t"); len(got) != 1 || got[0] != "c" {
		t.Fatalf("declared consumers for t = %v, want [c]", got)
	}
}

func TestNonPositiveHandlerTimeoutKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		_, o := openOutbox(t, WithHandlerTimeout(d))
		if o.handlerTimeout != 30*time.Second {
			t.Fatalf("WithHandlerTimeout(%s): budget %s, want the 30s default", d, o.handlerTimeout)
		}
	}
}

func TestExpandReachesNewerParentsPastTheBatch(t *testing.T) {
	db, o := openOutbox(t, WithBatchSize(2))
	o.Consume("c", "t", noopHandler)
	ctx := context.Background()
	var ids []string
	for range 5 {
		tx, _ := db.BeginTx(ctx, nil)
		id, err := o.Append(ctx, tx, "t", nil)
		if err != nil {
			t.Fatal(err)
		}
		tx.Commit()
		ids = append(ids, id)
	}
	for range 3 {
		if _, err := o.expandDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids {
		if len(mustDeliveries(t, o, id)) != 1 {
			t.Fatalf("parent %s has no delivery after three batch-limited expansions", id)
		}
	}
}

func TestPGExpandRaceIsANoOp(t *testing.T) {
	db, o := pgOutbox(t)
	db.SetMaxOpenConns(3)
	o.Consume("c", "t", noopHandler)
	ctx := context.Background()
	id := appendCommitted(t, db, o, "t")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s (row_id, consumer, status, attempts, created_at) VALUES ($1, 'c', 'pending', 0, $2)`, o.qd()), id, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := o.expandDeliveries(ctx); done <- err }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%INSERT%_delivery%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			_ = tx.Rollback()
			t.Fatal("the expansion never blocked on the uncommitted delivery row")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("expansion racing another relay's insert failed: %v", err)
	}
}

func TestExpiredExhaustedClaimIsDeadLettered(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(2))
	ctx := context.Background()
	id := appendCommitted(t, db, o, "t")
	insertDelivery(t, db, o, id, "crashed", "pending", 2, "")
	insertDelivery(t, db, o, id, "unleased", "pending", 2, "")
	if _, err := db.Exec(fmt.Sprintf(`UPDATE %s SET claimed_until = $1 WHERE row_id = $2 AND consumer = 'crashed'`, o.qd()),
		time.Now().UTC().Add(-time.Minute), id); err != nil {
		t.Fatal(err)
	}
	if _, err := o.claimDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	ds := mustDeliveries(t, o, id)
	if d := findDelivery(t, ds, "crashed"); d.Status != "dead" || d.LastError == "" {
		t.Fatalf("expired claim at the attempt budget = %q (%q), want dead with a cause", d.Status, d.LastError)
	}
	if d := findDelivery(t, ds, "unleased"); d.Status != "pending" {
		t.Fatalf("never-leased delivery at the attempt budget = %q, want pending", d.Status)
	}
}

func TestNoHandlerRequeueWaitsTheMaxBackoff(t *testing.T) {
	db, o := openOutbox(t, WithHandlerGrace(time.Hour))
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	o.now = func() time.Time { return now }
	o.Consume("keep", "t", noopHandler)
	id := appendCommitted(t, db, o, "t")
	insertDelivery(t, db, o, id, "ghost", "pending", 0, "")
	o.pump(context.Background())
	d := findDelivery(t, mustDeliveries(t, o, id), "ghost")
	if d.NextAttemptAt == nil || !d.NextAttemptAt.Equal(now.Add(o.backoffMax)) {
		t.Fatalf("no-handler requeue next attempt = %v, want now + max backoff %v", d.NextAttemptAt, now.Add(o.backoffMax))
	}
}

func TestRelayWithNoConsumersWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	_, o := openOutbox(t)
	o.StartRelay(context.Background())()
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "no declared consumers") {
		t.Fatalf("relay with no consumers logged %q, want a warning", buf.String())
	}
}

func TestRelayStartRewritesTimeTextToBindLayout(t *testing.T) {
	db, o := openOutbox(t, WithPollInterval(time.Millisecond))
	ctx := context.Background()
	other := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'dispatched', 0, $1)`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox_delivery (row_id, consumer, status, attempts, created_at)
		VALUES ('p1', 'c', 'dead', 1, $1)`, other); err != nil {
		t.Fatal(err)
	}
	o.Consume("c", "evt", noopHandler)
	o.StartRelay(ctx)()
	for _, q := range []string{
		`SELECT CAST(created_at AS TEXT) FROM event_outbox WHERE id='p1'`,
		`SELECT CAST(created_at AS TEXT) FROM event_outbox_delivery WHERE row_id='p1'`,
	} {
		var stored string
		if err := db.QueryRowContext(ctx, q).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, "T") {
			t.Fatalf("%s: stored %q is not in the driver's bind layout after the relay started", q, stored)
		}
	}
}

func TestMetricsCollectorOrdersConsumersAndHidesErrors(t *testing.T) {
	db, o := openOutbox(t)
	ctx := context.Background()
	if pending, dead, err := o.DeliveryCounts(ctx); err != nil || pending == nil || dead == nil {
		t.Fatalf("DeliveryCounts on an empty table = %v, %v, %v; want two non-nil maps", pending, dead, err)
	}
	for _, c := range []string{"zeta", "alpha", "mid", "beta", "omega"} {
		insertDelivery(t, db, o, "r", c, "pending", 0, "")
	}
	var buf bytes.Buffer
	o.MetricsCollector()(&buf)
	want := `outbox_pending{consumer="alpha"} 1
outbox_pending{consumer="beta"} 1
outbox_pending{consumer="mid"} 1
outbox_pending{consumer="omega"} 1
outbox_pending{consumer="zeta"} 1
`
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("collector output %q does not list consumers in order", buf.String())
	}
	db.Close()
	buf.Reset()
	o.MetricsCollector()(&buf)
	if buf.Len() != 0 {
		t.Fatalf("collector wrote %q after a failed count, want nothing", buf.String())
	}
}
