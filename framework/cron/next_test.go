package cron

import (
	"testing"
	"time"
)

// Parse exposes the cron schedule for callers outside the scheduler
// (e.g. the queue battery) and Next computes the next firing strictly
// after a given instant.

func TestCron_NextDaily(t *testing.T) {
	sc, err := Parse("0 2 * * *") // every day at 02:00
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// 2026-06-08 01:30 -> next fire 02:00 same day.
	base := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	got := sc.Next(base)
	want := time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", base, got, want)
	}
}

func TestCron_NextRollsToTomorrow(t *testing.T) {
	sc, err := Parse("0 2 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// 03:00 is past 02:00 today, so next is tomorrow 02:00.
	base := time.Date(2026, 6, 8, 3, 0, 0, 0, time.UTC)
	got := sc.Next(base)
	want := time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", base, got, want)
	}
}

func TestCron_NextStrictlyAfter(t *testing.T) {
	sc, err := Parse("*/15 * * * *") // every 15 minutes
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Exactly on a fire boundary -> Next returns the *following* one.
	base := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	got := sc.Next(base)
	want := time.Date(2026, 6, 8, 10, 15, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", base, got, want)
	}
}
