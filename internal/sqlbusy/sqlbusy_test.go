package sqlbusy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsMatchesDriverSpellings(t *testing.T) {
	for _, err := range []error{
		errors.New("database is locked"),
		errors.New("database is locked (5) (SQLITE_BUSY)"),
		errors.New("database table is locked: queue_jobs"),
		errors.New("stmt.Exec: SQLITE_BUSY"),
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
		return errors.New("database is locked")
	})
	if calls != 1 {
		t.Fatalf("a cancelled context kept retrying: calls=%d", calls)
	}
	if !Is(err) {
		t.Fatalf("the busy error was swallowed: %v", err)
	}
}

func TestRetryRecoversAfterBusy(t *testing.T) {
	calls := 0
	start := time.Now()
	err := Retry(context.Background(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Fatal("retry did not back off between attempts")
	}
}
