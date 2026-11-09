package sqlbusy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type codedError int

func (err codedError) Error() string { return "SQLite failure" }
func (err codedError) Code() int     { return int(err) }

func TestRetryHasBoundedOperationContext(t *testing.T) {
	start := time.Now()
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return codedError(5)
		}
		deadline, ok := ctx.Deadline()
		if !ok || deadline.Sub(start) > DefaultBudget+100*time.Millisecond {
			t.Errorf("retry on a deadline-free context has no DefaultBudget deadline: deadline=%v, present=%v", deadline, ok)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A settle that met the write lock gave up after seven attempts (about
// two seconds) while workers kept claiming, and its row sat claimed
// until the lease expired (#669). Retries now run until the caller's
// deadline, however many attempts that takes.
func TestRetryOutlastsAttemptCap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	calls := 0
	err := Retry(ctx, func(context.Context) error {
		calls++
		if calls < 40 {
			return codedError(5)
		}
		return nil
	})
	if err != nil || calls != 40 {
		t.Fatalf("busy retries stopped early: calls=%d err=%v", calls, err)
	}
}

func TestRetryDeadlineKeepsBusyCause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Retry(ctx, func(context.Context) error { return codedError(5) })
	if !errors.Is(err, context.DeadlineExceeded) || !Is(err) {
		t.Fatalf("err = %v, want the deadline wrapped around the busy error", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("retry ran %v past a 150ms deadline", took)
	}
}

func TestRetryPreservesFirstOperationBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	want, _ := ctx.Deadline()
	if err := Retry(ctx, func(ctx context.Context) error {
		if got, ok := ctx.Deadline(); !ok || !got.Equal(want) {
			t.Fatalf("first operation's deadline changed: got %v, want %v", got, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestIsUsesSQLiteResultCode(t *testing.T) {
	for _, code := range []int{5, 6, 261, 262, 517} {
		if !Is(fmt.Errorf("execute: %w", codedError(code))) {
			t.Errorf("SQLite result code %d was not retryable", code)
		}
	}
	if Is(errors.New("constraint failed: sqlite_busy_jobs")) {
		t.Fatal("non-SQLite error text was treated as a retryable result code")
	}
}

func TestIsMatchesDriverResultCodes(t *testing.T) {
	for _, err := range []error{
		codedError(5),
		codedError(6),
		fmt.Errorf("execute: %w", codedError(517)),
	} {
		if !Is(err) {
			t.Errorf("Is(%v) = false, want true", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("no such table: jobs"),
		errors.New("connection refused"),
	} {
		if Is(err) {
			t.Errorf("Is(%v) = true, want false", err)
		}
	}
}

func TestRetryStopsOnNonBusyAndContext(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), func(context.Context) error {
		calls++
		return errors.New("no such table")
	})
	if calls != 1 || err == nil || err.Error() != "no such table" {
		t.Fatalf("non-busy error retried: calls=%d err=%v", calls, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	err = Retry(ctx, func(context.Context) error {
		calls++
		cancel()
		return codedError(5)
	})
	if calls != 1 {
		t.Fatalf("a cancelled context kept retrying: calls=%d", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not returned: %v", err)
	}
}

func TestRetryRecoversAfterBusy(t *testing.T) {
	calls := 0
	start := time.Now()
	err := Retry(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return codedError(5)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	// Two pauses, at least 2.5ms and 5ms with jitter.
	if time.Since(start) < 7*time.Millisecond {
		t.Fatal("retry did not back off between attempts")
	}
}
