package queue

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// assertLogLine fails the test unless buf contains a record with the given
// slog level and message substring. Each TextHandler record is one line, so
// both fragments must appear on the same line.
func assertLogLine(t *testing.T, buf *bytes.Buffer, wantLevel, wantMsg string) {
	t.Helper()
	for line := range strings.SplitSeq(buf.String(), "\n") {
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

type loggingQueue interface {
	Queue
	RegisterHandler(string, Handler)
}

func TestDBQueueLogsHandlerFailureAndDeadLetter(t *testing.T) {
	testQueueFailureLogs(t, func(t *testing.T) (loggingQueue, *bytes.Buffer, func(context.Context)) {
		_, q, buf := openDBQueueWithLogger(t, 1)
		return q, buf, q.Start
	})
}

func TestMemoryQueueLogsHandlerFailureAndDeadLetter(t *testing.T) {
	testQueueFailureLogs(t, func(t *testing.T) (loggingQueue, *bytes.Buffer, func(context.Context)) {
		q, buf := newMemoryQueueWithLogger(t, 1)
		return q, buf, func(context.Context) { q.Start() }
	})
}

func testQueueFailureLogs(t *testing.T, newQueue func(*testing.T) (loggingQueue, *bytes.Buffer, func(context.Context))) {
	t.Helper()
	cases := []struct {
		name      string
		job       Job
		err       error
		wantLevel string
		wantMsg   string
	}{
		{"handler failure", Job{Type: "flaky", MaxAttempts: 5}, errors.New("boom"), "WARN", "queue: handler failed"},
		{"exhausted dead-letter", Job{Type: "doomed", MaxAttempts: 1}, errors.New("fatal"), "ERROR", "queue: job dead-lettered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, buf, start := newQueue(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var attempts atomic.Int32
			q.RegisterHandler(tc.job.Type, func(context.Context, Job) error {
				attempts.Add(1)
				return tc.err
			})
			t.Cleanup(func() { _ = q.Close() })
			start(ctx)
			if err := q.Enqueue(ctx, tc.job); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			waitFor(t, func() bool { return attempts.Load() >= 1 }, 5*time.Second, "handler was never called")
			if err := q.Close(); err != nil {
				t.Fatalf("close queue: %v", err)
			}
			assertLogLine(t, buf, tc.wantLevel, tc.wantMsg)
		})
	}
}
