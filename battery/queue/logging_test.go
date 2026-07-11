package queue

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// assertLogLine fails the test unless buf contains a record with the given
// slog level and message substring. Each TextHandler record is one line, so
// both fragments must appear on the same line.
func assertLogLine(t *testing.T, buf *bytes.Buffer, wantLevel, wantMsg string) {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "level="+wantLevel) && strings.Contains(line, wantMsg) {
			return
		}
	}
	t.Fatalf("missing log line level=%s msg=%q\nfull output:\n%s", wantLevel, wantMsg, buf.String())
}

// openDBQueueWithLogger is openDBQueue but injects a recording logger so the
// worker loop's Warn/Error output can be asserted on.
func openDBQueueWithLogger(t *testing.T, workers int) (*sql.DB, *DBQueue, *bytes.Buffer) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	q, err := NewDBQueue(db, WithWorkers(workers), WithDBLogger(logger))
	if err != nil {
		t.Fatalf("new db queue: %v", err)
	}
	return db, q, &buf
}

// newMemoryQueueWithLogger wires a recording logger into a MemoryQueue.
func newMemoryQueueWithLogger(t *testing.T, workers int) (*MemoryQueue, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	return NewMemoryQueue(workers, WithLogger(logger)), &buf
}

// ─── DBQueue logging ─────────────────────────────────────────────────

// A failing handler logs a WARN per attempt even while retries remain.
func TestDBQueueLogsHandlerFailure(t *testing.T) {
	_, q, buf := openDBQueueWithLogger(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls atomic.Int32
	q.RegisterHandler("flaky", func(_ context.Context, _ Job) error {
		calls.Add(1)
		return errors.New("boom")
	})
	q.Start(ctx)
	if err := q.Enqueue(ctx, Job{Type: "flaky", MaxAttempts: 5}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitFor(t, func() bool { return calls.Load() >= 1 }, 5*time.Second,
		"handler was never called")
	q.Close()

	assertLogLine(t, buf, "WARN", "queue: handler failed")
}

// When attempts are exhausted the worker loop escalates to ERROR dead-letter.
func TestDBQueueLogsDeadLetterAtError(t *testing.T) {
	_, q, buf := openDBQueueWithLogger(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls atomic.Int32
	q.RegisterHandler("doomed", func(_ context.Context, _ Job) error {
		calls.Add(1)
		return errors.New("fatal")
	})
	q.Start(ctx)
	if err := q.Enqueue(ctx, Job{Type: "doomed", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitFor(t, func() bool { return calls.Load() >= 1 }, 5*time.Second,
		"handler was never called")
	q.Close()

	assertLogLine(t, buf, "ERROR", "queue: job dead-lettered")
}

// ─── MemoryQueue logging ─────────────────────────────────────────────

// A failing handler logs a WARN per attempt while retries remain.
func TestMemoryQueueLogsHandlerFailure(t *testing.T) {
	q, buf := newMemoryQueueWithLogger(t, 1)

	var attempts atomic.Int32
	q.RegisterHandler("flaky", func(_ context.Context, _ Job) error {
		attempts.Add(1)
		return fmt.Errorf("boom")
	})
	q.Start()
	if err := q.Enqueue(context.Background(), Job{Type: "flaky", MaxAttempts: 5}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitFor(t, func() bool { return attempts.Load() >= 1 }, 5*time.Second,
		"handler was never called")
	q.Close()

	assertLogLine(t, buf, "WARN", "queue: handler failed")
}

// A job that exhausts MaxAttempts logs an ERROR dead-letter.
func TestMemoryQueueLogsDeadLetter(t *testing.T) {
	q, buf := newMemoryQueueWithLogger(t, 1)

	var attempts atomic.Int32
	q.RegisterHandler("doomed", func(_ context.Context, _ Job) error {
		attempts.Add(1)
		return fmt.Errorf("fatal")
	})
	q.Start()
	if err := q.Enqueue(context.Background(), Job{Type: "doomed", MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitFor(t, func() bool { return attempts.Load() >= 1 }, 5*time.Second,
		"handler was never called")
	q.Close()

	assertLogLine(t, buf, "ERROR", "queue: job dead-lettered")
}
