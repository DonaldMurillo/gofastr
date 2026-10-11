package cron

import (
	"strconv"
	"testing"
)

func TestFieldStepLargerThanRangeDoesNotOverflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("the overflowing step requires a 64-bit int")
	}
	mask, err := parseFieldPart("59-59/9223372036854775807", 1, 59)
	if err != nil {
		t.Fatalf("parseFieldPart: %v", err)
	}
	if want := uint64(1) << 59; mask != want {
		t.Fatalf("mask = %#x, want only day 59 (%#x)", mask, want)
	}
}
