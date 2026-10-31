package access_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A refresh whose reload keeps failing put the role straight back into the
// set drainDirty was looping over, so with the database down the worker
// spun without its backoff and never returned to see stopWork: the fanout
// stop waited on it forever (2026-10-27 D6). The worker now drains one
// snapshot per wake and leaves failures to its retry delay.
func TestGrantRefreshDownDBStops(t *testing.T) {
	ctx := context.Background()
	dbA, dbB := newGrantDB(t), newGrantDB(t)
	storeA := access.NewGrantStore(dbA, access.NewRolePolicy())
	storeB := access.NewGrantStore(dbB, access.NewRolePolicy())
	for _, s := range []*access.GrantStore{storeA, storeB} {
		if err := s.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	f := fanout.NewInProcess()
	stopA, err := storeA.SetFanout(f)
	if err != nil {
		t.Fatal(err)
	}
	defer stopA()
	stopB, err := storeB.SetFanout(f)
	if err != nil {
		t.Fatal(err)
	}

	// B's database goes away; A's grant then reaches B as a refresh that
	// fails every time.
	_ = dbB.Close()
	if err := storeA.Grant(ctx, "editor", access.Permission("posts:read")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		stopB()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("the fanout stop never returned while the refresh kept failing")
	}
}
