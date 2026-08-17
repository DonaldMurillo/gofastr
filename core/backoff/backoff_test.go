package backoff

import (
	"testing"
	"time"
)

// TestExponential ports the table the outbox and queue batteries
// historically pinned (base*2^(attempts-1), capped at max) plus the
// boundary cases their shared implementation must keep: non-positive
// attempts, max below base, uncapped overflow clamping, and a zero base.
func TestExponential(t *testing.T) {
	ms := func(n int64) time.Duration { return time.Duration(n) * time.Millisecond }
	cases := []struct {
		name     string
		base     time.Duration
		max      time.Duration
		attempts int
		want     time.Duration
	}{
		// Ported verbatim from framework/outbox TestBackoffFor.
		{name: "first retry waits base", base: ms(10), max: ms(100), attempts: 1, want: ms(10)},
		{name: "second retry doubles", base: ms(10), max: ms(100), attempts: 2, want: ms(20)},
		{name: "third retry doubles", base: ms(10), max: ms(100), attempts: 3, want: ms(40)},
		{name: "fourth retry doubles", base: ms(10), max: ms(100), attempts: 4, want: ms(80)},
		{name: "capped at max", base: ms(10), max: ms(100), attempts: 5, want: ms(100)},
		{name: "still capped", base: ms(10), max: ms(100), attempts: 9, want: ms(100)},

		// attempts <= 1 clamps the exponent to zero: the delay is base.
		{name: "zero attempts yields base", base: ms(10), max: ms(100), attempts: 0, want: ms(10)},
		{name: "negative attempts yields base", base: ms(10), max: ms(100), attempts: -5, want: ms(10)},

		// max below base clamps immediately.
		{name: "max below base at first attempt", base: ms(100), max: ms(50), attempts: 1, want: ms(50)},
		{name: "max below base at later attempt", base: ms(100), max: ms(50), attempts: 5, want: ms(50)},

		// Uncapped (max <= 0): doubling stops at the largest value that
		// cannot overflow int64 on the next doubling.
		{name: "uncapped growth", base: time.Duration(1), max: 0, attempts: 1, want: 1},
		{name: "uncapped doubling", base: time.Duration(1), max: 0, attempts: 2, want: 2},
		{name: "uncapped last exact power", base: time.Duration(1), max: 0, attempts: 62, want: time.Duration(1) << 61},
		{name: "uncapped clamp at 1<<62", base: time.Duration(1), max: 0, attempts: 63, want: time.Duration(1) << 62},
		{name: "uncapped clamp holds", base: time.Duration(1), max: 0, attempts: 64, want: time.Duration(1) << 62},
		{name: "uncapped huge base unchanged", base: time.Duration(1) << 62, max: 0, attempts: 100, want: time.Duration(1) << 62},

		// Zero base is zero (callers gate on base > 0 before using backoff).
		{name: "zero base is zero", base: 0, max: ms(100), attempts: 5, want: 0},
	}
	for _, c := range cases {
		if got := Exponential(c.base, c.max, c.attempts); got != c.want {
			t.Errorf("%s: Exponential(%v, %v, %d) = %v, want %v", c.name, c.base, c.max, c.attempts, got, c.want)
		}
	}
}

// TestExponentialMonotone pins the growth shape between the cap and the
// exponent clamp: the sequence never decreases, and once it reaches the
// cap it stays there.
func TestExponentialMonotone(t *testing.T) {
	base := 10 * time.Millisecond
	max := 1 * time.Hour
	prev := time.Duration(0)
	for attempts := 1; attempts < 80; attempts++ {
		got := Exponential(base, max, attempts)
		if got < prev {
			t.Fatalf("Exponential not monotone at attempts=%d: %v < %v", attempts, got, prev)
		}
		if got > max {
			t.Fatalf("Exponential(%v, %v, %d) = %v exceeds max %v", base, max, attempts, got, max)
		}
		prev = got
	}
	if prev != max {
		t.Fatalf("sequence never reached max: last = %v", prev)
	}
}

// TestAt pins the positional-schedule policy the webhook battery uses:
// table[attempts-1], with the final entry reused once attempts runs past
// the table. attempts < 1 reads as the first attempt; an empty table
// yields zero.
func TestAt(t *testing.T) {
	table := []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute}
	cases := []struct {
		name     string
		table    []time.Duration
		attempts int
		want     time.Duration
	}{
		{name: "first attempt", table: table, attempts: 1, want: 30 * time.Second},
		{name: "second attempt", table: table, attempts: 2, want: time.Minute},
		{name: "last in-range attempt", table: table, attempts: 3, want: 5 * time.Minute},
		{name: "past the table reuses last", table: table, attempts: 4, want: 5 * time.Minute},
		{name: "far past the table reuses last", table: table, attempts: 50, want: 5 * time.Minute},
		{name: "non-positive attempts reads first", table: table, attempts: 0, want: 30 * time.Second},
		{name: "negative attempts reads first", table: table, attempts: -3, want: 30 * time.Second},
		{name: "single entry always", table: []time.Duration{time.Second}, attempts: 99, want: time.Second},
		{name: "empty table is zero", table: nil, attempts: 3, want: 0},
	}
	for _, c := range cases {
		if got := At(c.table, c.attempts); got != c.want {
			t.Errorf("%s: At(%v, %d) = %v, want %v", c.name, c.table, c.attempts, got, c.want)
		}
	}
}
