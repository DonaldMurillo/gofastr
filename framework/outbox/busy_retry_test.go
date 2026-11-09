package outbox

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliverySettlesRetrySQLiteBusy(t *testing.T) {
	for _, arm := range []string{"dispatched", "retry", "dead", "unhandled", "abandoned", "release"} {
		t.Run(arm, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "busy.db"))+"?_busy_timeout=1&_journal_mode=WAL")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(3)
			grace := time.Hour
			if arm == "abandoned" {
				grace = 0
			}
			o, err := New(db, WithHandlerGrace(grace))
			if err != nil {
				t.Fatal(err)
			}
			id := appendCommitted(t, db, o, "t")
			if _, err := o.expandDeliveries(ctx); err != nil {
				t.Fatal(err)
			}
			claim := claimOne(t, o, "worker")
			blocker, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Close()
			if _, err := blocker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			unlocked := make(chan error, 1)
			time.AfterFunc(75*time.Millisecond, func() {
				_, err := blocker.ExecContext(ctx, "ROLLBACK")
				unlocked <- err
			})
			switch arm {
			case "dispatched":
				o.markDeliveryDispatched(ctx, claim)
			case "retry":
				o.markDeliveryFailure(ctx, claim, errors.New("retry"))
			case "dead":
				claim.Attempts = o.maxAttempts
				o.markDeliveryFailure(ctx, claim, errors.New("dead"))
			case "unhandled", "abandoned":
				o.requeueNoHandler(ctx, claim)
			case "release":
				o.releaseUnstarted(ctx, []claimedDelivery{claim})
			}
			if err := <-unlocked; err != nil {
				t.Fatal(err)
			}
			if lease := storedLease(t, db, o, id); lease != nil {
				t.Fatalf("settle left the lease active: %v", lease)
			}
			want := map[string]string{"dispatched": "dispatched", "retry": "pending", "dead": "dead", "unhandled": "pending", "abandoned": "abandoned", "release": "pending"}[arm]
			if got := findDelivery(t, mustDeliveries(t, o, id), "c"); got.Status != want {
				t.Fatalf("status=%s, want %s", got.Status, want)
			}
		})
	}
}
