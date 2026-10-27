package webhook

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// claimSqlite said BEGIN IMMEDIATE and ran database/sql's plain BEGIN,
// which SQLite reads as DEFERRED: two workers on a file-backed WAL
// database both ran the SELECT, and the loser's UPDATE could not upgrade
// to the write lock and failed with SQLITE_BUSY. The claim now holds the
// write lock before the SELECT (battery/queue dequeueSQLite and
// framework/outbox claimDeliveriesSQLite do the same).
//
// The gofastr sqlite3 driver defaults _txlock=immediate, which hid the bug;
// the DSN opts back into deferred, the default of every other SQLite
// driver (mattn, modernc's own "sqlite") a host may hand the store.
func TestSQLiteClaimConcurrentNoBusy(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "webhook.db")) +
		"?_busy_timeout=5000&_journal_mode=WAL&_txlock=deferred"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewSQLStore(db, WithSQLAllowPlaintext())
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	const rows = 200
	for i := range rows {
		if err := s.AddDelivery(ctx, Delivery{ID: fmt.Sprintf("d%03d", i), SubscriberID: "s", Event: "e",
			Payload: []byte("p"), Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	const workers = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	claims := make([][]string, workers)
	errs := make([][]string, workers)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for empty := 0; empty < 2; {
				if ctx.Err() != nil {
					errs[w] = append(errs[w], ctx.Err().Error())
					return
				}
				batch, err := s.ClaimDueDeliveries(ctx, now, 5, time.Minute)
				switch {
				case err != nil:
					errs[w] = append(errs[w], err.Error())
					time.Sleep(2 * time.Millisecond)
				case len(batch) == 0:
					empty++
				default:
					empty = 0
					for _, d := range batch {
						claims[w] = append(claims[w], d.ID)
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	var failures []string
	seen := map[string]int{}
	for w := range workers {
		failures = append(failures, errs[w]...)
		for _, id := range claims[w] {
			seen[id]++
		}
	}
	if len(failures) > 0 {
		busy := 0
		for _, e := range failures {
			if s := strings.ToLower(e); strings.Contains(s, "busy") || strings.Contains(s, "locked") {
				busy++
			}
		}
		t.Errorf("%d claim errors (%d busy/locked) from concurrent workers, want none: %q",
			len(failures), busy, failures[:min(len(failures), 5)])
	}
	if len(seen) != rows {
		t.Errorf("claimed %d distinct deliveries, want %d", len(seen), rows)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("delivery %s claimed %d times, want 1", id, n)
		}
	}
}
