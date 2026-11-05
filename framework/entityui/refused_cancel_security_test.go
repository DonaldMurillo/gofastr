package entityui

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type cancelRefusedRunner struct {
	JobRunner
	cancel context.CancelFunc
}

func (runner cancelRefusedRunner) Enqueue(context.Context, BulkJob) error {
	runner.cancel()
	return errors.New("queue refused")
}

type cancelAwareBulkStore struct {
	BulkStore
	bounded []bool
	owners  []string
}

func (store *cancelAwareBulkStore) check(ctx context.Context) error {
	_, bounded := ctx.Deadline()
	store.bounded = append(store.bounded, bounded)
	store.owners = append(store.owners, userID(ctx))
	return ctx.Err()
}

func (store *cancelAwareBulkStore) Claim(ctx context.Context, id, runner string, now, until time.Time) (bool, error) {
	if err := store.check(ctx); err != nil {
		return false, err
	}
	return store.BulkStore.Claim(ctx, id, runner, now, until)
}

func (store *cancelAwareBulkStore) Finish(ctx context.Context, id, runner, status string, at time.Time) error {
	if err := store.check(ctx); err != nil {
		return err
	}
	return store.BulkStore.Finish(ctx, id, runner, status, at)
}

type cancelAwareBulkHost struct {
	Host
	BulkHost
	store BulkStore
}

func (host cancelAwareBulkHost) BulkStore() BulkStore { return host.store }

func TestRefusedJobSurvivesDisconnect(t *testing.T) {
	fixture := newRemindJob(t)
	ctx, cancel := context.WithCancel(bulkCtx("u1", nil))
	defer cancel()
	store := &cancelAwareBulkStore{BulkStore: fixture.mb}
	fixture.x.ui.host = cancelAwareBulkHost{Host: fixture.x.ui.host, BulkHost: fixture.x.ui.bulkHost(), store: store}
	fixture.x.ui.ext.Jobs = cancelRefusedRunner{JobRunner: fixture.mb, cancel: cancel}
	code, _ := postBulk(t, fixture.x, ctx, map[string]any{
		"action": "run:remind", "scope": "every", "match": matchDigest(invoiceIDs(t, fixture.x)),
	})
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", code)
	}
	fixture.x.ui.ext.Jobs = fixture.mb
	if count, err := fixture.x.ui.ResumeBulkJobs(context.Background(), 0); err != nil || count != 0 {
		t.Fatalf("refused job resumed: count = %d, error = %v", count, err)
	}
	if len(store.bounded) != 2 {
		t.Fatalf("settlement calls = %d, want Claim and Finish", len(store.bounded))
	}
	for index, bounded := range store.bounded {
		if !bounded || store.owners[index] != "u1" {
			t.Errorf("settlement %d: deadline = %v, owner = %q", index, bounded, store.owners[index])
		}
	}
}
