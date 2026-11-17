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
	start := time.Now()
	calls := 0
	_ = Retry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return codedError(5)
		}
		if d, _ := ctx.Deadline(); d.Sub(start) < 9*time.Second {
			t.Fatalf("retry budget is %v, want ten seconds", d.Sub(start))
		}
		return nil
	})
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
