package framework

import (
	"context"
	"testing"
	"time"
)

// TestReadinessTimeoutDoesNotRaceLateCheck pins the /readyz deadline
// path. On timeout runReadinessChecks read out[i] to find the rows no
// check had filled, then copied the slice, while a check that ignored
// ctx was still running and about to write its own out[i]: an
// unsynchronized read and write of a struct holding strings, which
// `go test -race` reports and which can hand the JSON encoder a torn
// string header. Run under -race; the late check finishes after the
// response is built, and the race detector sees the two accesses.
func TestReadinessTimeoutDoesNotRaceLateCheck(t *testing.T) {
	// Sleep, not a channel the test closes: a channel would order the
	// check's write after the test's read and hide the race.
	checks := []ReadinessCheck{
		{Name: "late", Check: func(context.Context) error {
			time.Sleep(80 * time.Millisecond)
			return nil
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := runReadinessChecks(ctx, checks, false)
	if res.Checks[0].Status != "timeout" {
		t.Fatalf("late check: status %q, want timeout", res.Checks[0].Status)
	}
	// Let the check's write land before the test ends.
	time.Sleep(200 * time.Millisecond)
	if res.Checks[0].Status != "timeout" {
		t.Fatalf("the late check rewrote the returned row: %+v", res.Checks[0])
	}
}

// TestReadinessTimeoutKeepsFinishedUnnamedCheck: the timeout path told a
// finished row from an unfinished one by an empty Name, so a check
// registered with an empty name that had already passed was reported as
// a timeout the moment any other check hung.
func TestReadinessTimeoutKeepsFinishedUnnamedCheck(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	checks := []ReadinessCheck{
		{Name: "", Check: func(context.Context) error { return nil }},
		{Name: "hang", Check: func(context.Context) error { <-block; return nil }},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := runReadinessChecks(ctx, checks, false)
	if got := res.Checks[0].Status; got != "ok" {
		t.Errorf("finished unnamed check reported %q, want ok", got)
	}
	if got := res.Checks[1].Status; got != "timeout" {
		t.Errorf("hung check reported %q, want timeout", got)
	}
}
