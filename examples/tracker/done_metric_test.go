package main

import "testing"

func TestDoneMetricUsesTwoRecentStamps(t *testing.T) {
	if got := countDoneThisWeek(); got != 2 {
		t.Fatalf("Done this week = %d, want 2 recent stamps", got)
	}
}
