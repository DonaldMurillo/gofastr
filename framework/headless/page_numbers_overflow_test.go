package headless

import (
	"math"
	"slices"
	"testing"
)

func TestPageNumbersNearLargestPage(t *testing.T) {
	got := pageNumbers(math.MaxInt, math.MaxInt-1, 3)
	want := []int{1, 0, math.MaxInt - 4, math.MaxInt - 3, math.MaxInt - 2, math.MaxInt - 1, math.MaxInt}
	if !slices.Equal(got, want) {
		t.Errorf("pageNumbers = %v, want %v", got, want)
	}
}
