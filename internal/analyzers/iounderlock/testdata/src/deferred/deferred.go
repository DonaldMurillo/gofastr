package deferred

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type viaCallback struct {
	db        *sql.DB
	mu        sync.Mutex
	lastSweep time.Time
}

func (v *viaCallback) sweepAsync(ctx context.Context) {
	v.mu.Lock()
	if time.Since(v.lastSweep) < 15*time.Minute {
		v.mu.Unlock()
		return
	}
	v.lastSweep = time.Now()
	v.mu.Unlock()
	go func() { _, _ = v.db.ExecContext(ctx, `DELETE FROM t`) }()
	defer func() { _, _ = v.db.Exec(`DELETE FROM t`) }()
}

func NewDeferredIOFixture(database *sql.DB) (func(context.Context), *sync.Mutex) {
	store := &viaCallback{db: database}
	return store.sweepAsync, &store.mu
}
