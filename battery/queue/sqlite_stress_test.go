package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The reported reproduction (#656): one SQLite-file run, 12 workers over
// two lanes, left a job `claimed` past its lease window once in 15 runs.
// The cause (#669): a settle (ack, nack, hand-back) that met the write
// lock gave up after seven retries, about two seconds, while the other
// workers kept claiming, and its row sat claimed until the lease expired.
// This soak drives that shape hard (two queues on one file, a 5ms busy
// timeout, half the handlers nacking) and asserts the drain invariant:
// once every job has settled, none sits claimed, and no settle failed.
//
// The lease is far longer than the drain deadline, so a drain that only
// finishes because lease expiry reclaimed a stranded row fails here
// instead of passing late. The settle-failure log check fails the run on
// the first settle that gave up, before any timing comes into it.
func TestSQLiteFileStressDrainsClean(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "stress.db")) +
		"?_busy_timeout=5&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = db.Close() })
	assertStressDrainsClean(t, db)
}

// The Postgres twin of the soak: row locks and SKIP LOCKED claims never
// fail a settle on contention, and this holds it to the same invariant.
func TestPGStressDrainsClean(t *testing.T) {
	db := pgtest.DB(t)
	// pgtest pins one connection; the soak needs the workers to race.
	db.SetMaxOpenConns(16)
	assertStressDrainsClean(t, db)
}

// stressDeadline is how long the drain may take. It sits well below
// stressLease, the claim lease, so lease recovery cannot finish a drain
// in time. A passing run returns the moment the queue drains (about a
// second on a laptop); the margin is for a loaded CI runner, where 15
// seconds ran out with the drain still moving.
const (
	stressDeadline = 60 * time.Second
	stressLease    = 2 * time.Minute
)

func assertStressDrainsClean(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), stressDeadline+5*time.Second)
	defer cancel()

	const jobs = 600
	var nacked atomic.Int64
	settles := &settleFailureLog{}
	mk := func() *DBQueue {
		q, err := NewDBQueue(db, WithWorkers(6), WithDoneRetention(time.Hour),
			WithLeaseTimeout(stressLease), WithDBLogger(slog.New(settles)))
		if err != nil {
			t.Fatal(err)
		}
		q.RegisterHandler("ok", func(context.Context, Job) error { return nil })
		q.RegisterHandler("boom", func(context.Context, Job) error {
			nacked.Add(1)
			return errors.New("flake")
		})
		return q
	}
	queues := []*DBQueue{mk(), mk()}
	for _, q := range queues {
		for i := range jobs / 2 {
			typ := "ok"
			if i%2 == 0 {
				typ = "boom"
			}
			if err := q.Enqueue(ctx, Job{Type: typ, Payload: []byte("{}"), MaxAttempts: 3}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
		}
	}
	for _, q := range queues {
		q.Start(ctx)
	}
	defer func() {
		for _, q := range queues {
			q.Close()
		}
	}()

	count := func(status string) int {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM queue_jobs WHERE status = $1", status).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", status, err)
		}
		return n
	}
	deadline := time.Now().Add(stressDeadline)
	for time.Now().Before(deadline) {
		if failed := settles.list(); len(failed) > 0 {
			t.Fatalf("a settle gave up and left its row claimed (stuck claimed rows: %d): %s",
				count("claimed"), strings.Join(failed, "; "))
		}
		// One query for both states: a nack moves a row from claimed back
		// to pending, so two separate counts can each read zero while
		// that row is still pending.
		var live int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM queue_jobs WHERE status IN ('pending', 'claimed')").Scan(&live); err != nil {
			t.Fatalf("count live: %v", err)
		}
		if live == 0 {
			// Every job settled and nothing is stuck mid-flight.
			if nacked.Load() == 0 {
				t.Fatal("the flaky handler never ran; the soak proved nothing")
			}
			for _, status := range []string{"done", "failed"} {
				if n := count(status); n != jobs/2 {
					t.Fatalf("%s rows=%d, want %d", status, n, jobs/2)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("drain did not finish in %v (pending rows: %d, stuck claimed rows: %d, settle failures: %v)",
		stressDeadline, count("pending"), count("claimed"), settles.list())
}

// settleFailureLog is a slog handler that keeps the worker's settle
// failures (an ack, nack or hand-back that gave up), the warnings a
// drained soak must never log.
type settleFailureLog struct {
	mu     sync.Mutex
	failed []string
}

func (l *settleFailureLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *settleFailureLog) Handle(_ context.Context, r slog.Record) error {
	if !isSettleFailure(r.Message) {
		return nil
	}
	line := r.Message
	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})
	l.mu.Lock()
	l.failed = append(l.failed, line)
	l.mu.Unlock()
	return nil
}

func (l *settleFailureLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *settleFailureLog) WithGroup(string) slog.Handler      { return l }

func (l *settleFailureLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.failed...)
}

// isSettleFailure matches the worker loop's settle-failure warnings.
func isSettleFailure(msg string) bool {
	for _, m := range []string{"queue: ack failed", "queue: nack failed", "queue: handing back", "queue: dead-lettering a job with no handler failed"} {
		if strings.HasPrefix(msg, m) {
			return true
		}
	}
	return false
}
