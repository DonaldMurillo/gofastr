package entityui

import (
	"math"
	"testing"
)

func TestIntBoundClampsHugeBounds(t *testing.T) {
	huge, low, nan := 1e30, -1e30, math.NaN()
	if got := intBound(&huge); got != 1<<53 {
		t.Errorf("intBound(1e30) = %d, want 2^53", got)
	}
	if got := intBound(&low); got != -(1 << 53) {
		t.Errorf("intBound(-1e30) = %d, want -2^53", got)
	}
	if got := intBound(&nan); got != 0 {
		t.Errorf("intBound(NaN) = %d, want 0", got)
	}
}
