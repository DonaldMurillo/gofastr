package crud

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func TestAmbientDeleteFollowsCommit(t *testing.T) {
	for _, operation := range []string{"soft", "hard", "purge"} {
		for _, commit := range []bool{false, true} {
			name := operation + "/rollback"
			if commit {
				name = operation + "/commit"
			}
			t.Run(name, func(t *testing.T) {
				ch, conn := covNotesHandler(t)
				if operation != "hard" {
					ch.Entity.Config.Scope.SoftDelete = true
					if _, err := conn.Exec(`ALTER TABLE notes ADD COLUMN deleted_at TIMESTAMP`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := conn.Exec(`INSERT INTO notes (id, title) VALUES ('note', 'test')`); err != nil {
					t.Fatal(err)
				}
				if operation == "purge" {
					if _, err := conn.Exec(`UPDATE notes SET deleted_at = CURRENT_TIMESTAMP`); err != nil {
						t.Fatal(err)
					}
				}
				ch.Events = event.NewEventBus()
				got := make(chan event.Event, 1)
				t.Cleanup(ch.Events.Subscribe(event.EntityDeleted, func(_ context.Context, value event.Event) error {
					got <- value
					return nil
				}))
				tx, err := conn.Begin()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { tx.Rollback() })
				ctx := db.WithTx(WithServerWrites(context.Background()), tx)
				if operation == "purge" {
					err = ch.PurgeOne(ctx, "note")
				} else {
					err = ch.DeleteOne(ctx, "note")
				}
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-got:
					t.Fatal("delete announced before transaction ended")
				case <-time.After(50 * time.Millisecond):
				}
				if commit {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-got:
					if !commit {
						t.Error("rolled-back delete announced")
					}
				case <-time.After(500 * time.Millisecond):
					if commit {
						t.Error("committed delete was not announced")
					}
				}
			})
		}
	}
}
