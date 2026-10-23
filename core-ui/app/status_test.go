package app

import (
	"context"
	"net/http"
	"testing"
)

// The first status set wins, an out-of-range code is ignored, and a
// render with no recorder ignores the call.
func TestSetStatusFirstWins(t *testing.T) {
	SetStatus(context.Background(), http.StatusNotFound) // no recorder: no panic, no effect

	ctx, recorded := TrackStatus(context.Background())
	if got := recorded(); got != 0 {
		t.Fatalf("untouched recorder = %d, want 0", got)
	}
	SetStatus(ctx, 99)
	SetStatus(ctx, 1000)
	if got := recorded(); got != 0 {
		t.Fatalf("out-of-range codes recorded %d, want 0", got)
	}
	SetStatus(ctx, http.StatusNotFound)
	SetStatus(ctx, http.StatusGone)
	if got := recorded(); got != http.StatusNotFound {
		t.Fatalf("recorded = %d, want the first set, 404", got)
	}
}
