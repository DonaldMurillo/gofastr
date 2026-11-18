package sqlbusy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryStopsOnALaterNonBusyError(t *testing.T) {
	other := errors.New("constraint")
	calls := 0
	err := Retry(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			return codedError(5)
		}
		return other
	})
	if !errors.Is(err, other) || calls != 2 {
		t.Fatalf("err = %v after %d calls, want the non-busy error after 2", err, calls)
	}
}

func TestRetryOnAnEndedContextNeverCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := Retry(ctx, func(context.Context) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err = %v, called = %v; want context.Canceled without a call", err, called)
	}
}

func TestDefaultBudgetIsTenSeconds(t *testing.T) {
	if DefaultBudget != 10*time.Second {
		t.Fatalf("DefaultBudget = %v, want 10s", DefaultBudget)
	}
	start := time.Now()
	calls := 0
	err := Retry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return codedError(5)
		}
		d, ok := ctx.Deadline()
		if !ok {
			t.Fatal("retry context has no deadline")
		}
		if lo, hi := start.Add(DefaultBudget), time.Now().Add(DefaultBudget); d.Before(lo) || d.After(hi) {
			t.Fatalf("retry deadline is %v after start, want %v", d.Sub(start), DefaultBudget)
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("Retry = %v after %d calls, want nil after 2", err, calls)
	}
}

func TestExecReportsRowsOfTheAttemptThatLanded(t *testing.T) {
	calls := 0
	rows, err := Exec(context.Background(), func(context.Context) (int64, error) {
		calls++
		if calls == 1 {
			return 0, codedError(5)
		}
		return 3, nil
	})
	if err != nil || rows != 3 {
		t.Fatalf("Exec = %d, %v; want 3, nil", rows, err)
	}
}
