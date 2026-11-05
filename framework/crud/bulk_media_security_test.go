package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestBulkUpdateReleasesStoredFiles(t *testing.T) {
	handler, _ := clnWorld(t, false)
	query := NewTypedQuery[struct{ ID string }](handler)
	count, err := query.UpdateAll(context.Background(), map[string]any{"photo": "https://cdn.example.com/new.png", "photo_variants": "[]"})
	if err != nil || count != 3 {
		t.Fatalf("UpdateAll = %d, %v", count, err)
	}
	for _, key := range []string{clnKey, clnVariant, clnShared} {
		if clnExists(t, handler, key) {
			t.Errorf("replaced object %s survived the bulk update", key)
		}
	}
}

func TestBulkCleanupWaitsForCommit(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			handler, database := clnWorld(t, false)
			transaction, err := database.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback()
			ctx, queue := db.WithTxQueue(context.Background(), transaction)
			query := NewTypedQuery[struct{ ID string }](handler).Where(entity.NewStringColumn("photo").Eq(clnKey))
			count, err := query.UpdateAll(ctx, map[string]any{"photo": "https://cdn.example.com/new.png", "photo_variants": "[]"})
			if err != nil || count != 1 {
				t.Fatalf("UpdateAll = %d, %v", count, err)
			}
			if !clnExists(t, handler, clnKey) || !clnExists(t, handler, clnVariant) {
				t.Fatal("bulk cleanup ran before the outer transaction committed")
			}
			if commit {
				if err := transaction.Commit(); err != nil {
					t.Fatal(err)
				}
				queue.RunAfterCommit()
			} else if err := transaction.Rollback(); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{clnKey, clnVariant} {
				if exists := clnExists(t, handler, key); exists == commit {
					t.Errorf("commit=%v, %s exists=%v", commit, key, exists)
				}
			}
		})
	}
}

func TestBulkCleanupKeepsReferencedFiles(t *testing.T) {
	handler, database := clnWorld(t, true)
	if _, err := database.Exec("UPDATE cln SET deleted_at = '2026-11-05' WHERE id = 's2'"); err != nil {
		t.Fatal(err)
	}
	query := NewTypedQuery[struct{ ID string }](handler)
	count, err := query.UpdateAll(context.Background(), map[string]any{"photo": "https://cdn.example.com/new.png"})
	if err != nil || count != 2 {
		t.Fatalf("UpdateAll = %d, %v", count, err)
	}
	if clnExists(t, handler, clnKey) {
		t.Error("unreferenced main object survived")
	}
	for _, key := range []string{clnVariant, clnShared} {
		if !clnExists(t, handler, key) {
			t.Errorf("still referenced object %s was deleted", key)
		}
	}
}
