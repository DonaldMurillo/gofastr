package backoff

import (
	"testing"
	"time"
)

func TestExponentialStopsBeforeNegativeOverflow(t *testing.T) {
	const base = time.Duration(-1 << 62)
	if got := Exponential(base, time.Hour, 3); got != -1<<63 {
		t.Fatalf("got %v, want the last representable delay", got)
	}
}
