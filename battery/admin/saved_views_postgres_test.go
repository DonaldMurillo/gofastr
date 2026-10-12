package admin

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Concurrent creates on Postgres cannot take an owner past the cap: under
// READ COMMITTED each transaction's COUNT misses the others' inserts, so
// the count and the insert need a lock, not only a transaction.
func TestSavedViewsCapHoldsUnderConcurrency(t *testing.T) {
	db := pgtest.DB(t)
	db.SetMaxOpenConns(32) // pgtest pins one connection; the race needs many
	store, err := newSavedViews(context.Background(), db, "admin_saved_views")
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerCtx("u1", "t1")
	for i := range entityui.SavedViewCap - 1 {
		mustCreateView(t, ctx, store, fmt.Sprintf("seed-%02d", i))
	}
	// Open the pool up front, so every create starts on a live
	// connection at once instead of queueing behind a dial.
	db.SetMaxIdleConns(32)
	conns := make([]*sql.Conn, 24)
	for i := range conns {
		if conns[i], err = db.Conn(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = store.Create(ctx, entityui.SavedView{Entity: "posts", Name: fmt.Sprintf("race-%02d", i)})
		}()
	}
	close(start)
	wg.Wait()
	views, err := store.List(ctx, "posts")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != entityui.SavedViewCap {
		t.Fatalf("SECURITY: %d views after concurrent creates, want the cap %d", len(views), entityui.SavedViewCap)
	}
}
