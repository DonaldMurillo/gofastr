package queue

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for a worker goroutine to write while
// the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForLog polls buf until it holds want, or fails after a generous bound.
func waitForLog(t *testing.T, buf *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q log line; got:\n%s", want, buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The worker loop treated every claim error as an empty queue: a dropped
// or renamed table, a lost database, a row the claim cannot decode, each
// stalled every job with no log line at all. The outbox relay logs the
// onset of such an outage once and its recovery once; the queue now does
// the same.
func TestClaimErrorsAreLogged(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	buf := &syncBuffer{}
	q, err := NewDBQueue(db, WithWorkers(0), WithDBLogger(slog.New(slog.NewTextHandler(buf, nil))))
	if err != nil {
		t.Fatal(err)
	}
	q.RegisterHandler("t", func(context.Context, Job) error { return nil })
	if _, err := db.Exec("ALTER TABLE " + q.qt() + " RENAME TO moved_away"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		q.workerLoop(ctx, "")
	}()
	waitForLog(t, buf, "queue: claiming jobs failed")
	// Let a few more polls fail (one every 100ms), so a log per failure
	// would show. The pass never depends on this wait.
	time.Sleep(350 * time.Millisecond)
	if _, err := db.Exec("ALTER TABLE moved_away RENAME TO " + q.qt()); err != nil {
		t.Fatal(err)
	}
	waitForLog(t, buf, "queue: claiming jobs recovered")
	cancel()
	<-done
	if n := strings.Count(buf.String(), "queue: claiming jobs failed"); n != 1 {
		t.Errorf("the outage was logged %d times, want once", n)
	}
}
