package framework

import (
	"context"
	"testing"
	"time"
)

// TestReadinessDeadlineDoesNotRaceALateCheck: at the deadline /readyz
// fills the rows still missing, reading the result slice while a check
// that returned in that same moment writes its own row into it. Run with
// -race: a check that notices the cancelled context and returns writes
// its row just as the fill reads it. Each row also had to name its check
// to count as reported, so a completed check registered with no name was
// reported as a timeout.
func TestReadinessDeadlineDoesNotRaceALateCheck(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		checks := []ReadinessCheck{
			{Name: "", Check: func(context.Context) error { return nil }},
		}
		for range 8 {
			checks = append(checks, ReadinessCheck{Name: "late", Check: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}})
		}
		res := runReadinessChecks(ctx, checks, false)
		cancel()
		if got := res.Checks[0].Status; got != "ok" && got != "timeout" {
			t.Fatalf("unnamed check status %q", got)
		}
	}
}

// TestReadinessUnnamedCompletedCheckIsNotATimeout pins the second half:
// a check that finished before the deadline keeps its result.
func TestReadinessUnnamedCompletedCheckIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := runReadinessChecks(ctx, []ReadinessCheck{
		{Name: "", Check: func(context.Context) error { return nil }},
		{Name: "hang", Check: func(context.Context) error { time.Sleep(200 * time.Millisecond); return nil }},
	}, false)
	if got := res.Checks[0].Status; got != "ok" {
		t.Fatalf("a completed unnamed check reported %q, want ok", got)
	}
}
