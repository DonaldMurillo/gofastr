package backoff

import (
	"testing"
	"time"
)

func TestMinimumAttemptDoesNotWrap(t *testing.T) {
	minInt := -int(^uint(0)>>1) - 1
	if got := Exponential(time.Second, time.Hour, minInt); got != time.Second {
		t.Fatalf("nonpositive attempts produced %v, want base", got)
	}
}

func TestSafeDoublingDoesNotClampEarly(t *testing.T) {
	base := time.Duration(1<<61) + 1
	if got := Exponential(base, time.Duration(1<<63-1), 2); got != 2*base {
		t.Fatalf("safe doubling produced %d, want %d", got, 2*base)
	}
}

func TestZeroBaseReturnsImmediately(t *testing.T) {
	done := make(chan time.Duration, 1)
	go func() { done <- Exponential(0, time.Hour, int(^uint(0)>>1)) }()
	select {
	case got := <-done:
		if got != 0 {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("zero base iterates the attempt count instead of returning zero")
	}
}
