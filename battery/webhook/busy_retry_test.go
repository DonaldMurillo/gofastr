package webhook

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimSettlesRetrySQLiteBusy(t *testing.T) {
	for _, arm := range []string{"success", "retry", "dead", "release"} {
		t.Run(arm, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "busy.db"))+"?_busy_timeout=1&_journal_mode=WAL")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(3)
			store, err := NewSQLStore(db, WithSQLAllowPlaintext())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			if err := store.AddSubscriber(ctx, Subscriber{ID: "sub", URL: "https://example.com/hook", Secret: "secret", Active: true, Created: now}); err != nil {
				t.Fatal(err)
			}
			if err := store.AddDelivery(ctx, Delivery{ID: "delivery", SubscriberID: "sub", Event: "t", Payload: []byte("{}"), Status: StatusPending, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			claims, err := store.ClaimDueDeliveries(ctx, now, 1, time.Minute)
			if err != nil || len(claims) != 1 {
				t.Fatalf("claims=%v, err=%v", claims, err)
			}
			claim := claims[0]
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
			if arm == "release" {
				err = store.releaseClaim(ctx, claim, now)
			} else {
				claim.Status = map[string]DeliveryStatus{"success": StatusSuccess, "retry": StatusPending, "dead": StatusDead}[arm]
				claim.NextAttemptAt = now.Add(time.Second)
				err = store.UpdateDelivery(ctx, claim)
			}
			if unlockErr := <-unlocked; unlockErr != nil {
				t.Fatal(unlockErr)
			}
			if err != nil {
				t.Fatalf("settle failed under transient contention: %v", err)
			}
			rows, err := store.ListDeliveries(ctx, "sub", 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("deliveries=%v, err=%v", rows, err)
			}
			if rows[0].NextAttemptAt.Equal(claim.claimedUntil) {
				t.Fatal("settle left the original claim lease active")
			}
		})
	}
}
