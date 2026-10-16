package pagination

import (
	"math"
	"testing"
)

func TestOffsetPageKeepsIntegerPrecision(t *testing.T) {
	for _, total := range []int{math.MaxInt, math.MaxInt - 1} {
		got := NewOffsetPage(nil, 1, 1, total)
		if got.TotalPages != total {
			t.Fatalf("total %d gave %d pages at size 1", total, got.TotalPages)
		}
	}
}

func TestCursorPageRejectsNegativeLimitWithoutPanic(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("negative limit panicked: %v", p)
		}
	}()
	got := NewCursorPage([]map[string]any{{"id": 1}}, "id", -1)
	if len(got.Data) != 1 || got.HasMore {
		t.Fatalf("invalid limit failed to use default: %+v", got)
	}
}
