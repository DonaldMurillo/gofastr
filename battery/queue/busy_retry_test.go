package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// One SQLite-file stress run left a job `claimed` past its lease window:
// a nack whose UPDATE met SQLITE_BUSY failed and the row stayed claimed
// until lease expiry (#656, queue and outbox). The settle writes now
// retry a busy database before giving up.

// openBusyQueueDB opens a file-backed SQLite with a 1ms busy timeout, so
// a second connection holding the write lock makes the first busy at
// once instead of blocking for the default 5s.
func openBusyQueueDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "busy.db")) +
		"?_busy_timeout=1&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNackRetriesSQLiteBusy(t *testing.T) {
	db := openBusyQueueDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	job := Job{Type: "t", Payload: []byte("{}")}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Hold the write lock on another pool connection.
	blocker, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.ExecContext(ctx, "UPDATE queue_jobs SET updated_at = updated_at"); err != nil {
		t.Fatal(err)
	}

	nacked := make(chan error, 1)
	go func() { nacked <- q.Nack(context.Background(), claimed) }()
	// The first attempts fail on the held lock; drop it while the retry
	// loop is still running.
	time.Sleep(50 * time.Millisecond)
	if _, err := blocker.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-nacked:
		if err != nil {
			t.Fatalf("Nack did not retry past SQLITE_BUSY: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Nack never returned")
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM queue_jobs WHERE id = $1", claimed.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("job status after Nack = %q, want pending", status)
	}
}

func TestAckRetriesSQLiteBusy(t *testing.T) {
	db := openBusyQueueDB(t)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "t", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}

	blocker, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	if _, err := blocker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.ExecContext(ctx, "UPDATE queue_jobs SET updated_at = updated_at"); err != nil {
		t.Fatal(err)
	}

	acked := make(chan error, 1)
	go func() { acked <- q.Ack(context.Background(), claimed) }()
	time.Sleep(50 * time.Millisecond)
	if _, err := blocker.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-acked:
		if err != nil {
			t.Fatalf("Ack did not retry past SQLITE_BUSY: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ack never returned")
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM queue_jobs WHERE id = $1", claimed.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the acked job row is still there")
	}
}
