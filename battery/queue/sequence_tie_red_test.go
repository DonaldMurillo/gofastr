//go:build red

package queue

import (
	"context"
	"fmt"
	"testing"
)

// Open finding from the 2026-11-10 bug hunt, kept as a red probe: it
// asserts the correct behaviour and fails until the fix lands. The fix
// needs a monotonic insertion sequence on both dialects (a new column),
// which is a schema decision.
//
// SequenceKey promises "changes to one record ... reach a
// receiver in the order they happened" (Job.SequenceKey, queue.md
// "Ordered jobs"). The order is CreatedAt, which Enqueue stamps from the
// wall clock, and a CreatedAt tie is broken by the job ID, which Enqueue
// draws at random. Two jobs enqueued one after the other inside one
// reading of the clock therefore run in coin-flip order.
//
// One reading of the clock covers more than it sounds: Go's wall clock on
// Windows advances once per timer interrupt (0.5 to 15.6 ms), far longer
// than a SQLite insert, and a Postgres TIMESTAMPTZ keeps microseconds
// only. The package's own sequence tests step their fake clock a
// millisecond between enqueues, which is what hides it.
//
// Here the clock stands still across each pair, as it does inside one
// tick, and the two enqueues are strictly sequential.
func TestRedSequenceKeyOrderWithinOneClockTick(t *testing.T) {
	q, clock := openPolicyQueue(t)
	ctx := context.Background()

	const keys = 30
	for k := range keys {
		key := fmt.Sprintf("order:%d", k)
		for _, step := range []string{"1-created", "2-paid"} {
			err := q.Enqueue(ctx, Job{Type: "deliver", SequenceKey: key, Payload: []byte(`"` + step + `"`)})
			if err != nil {
				t.Fatal(err)
			}
		}
		clock.Advance(1) // the next pair lands in a later tick
	}

	// Drain as a single worker does: claim, run, ack.
	seen := map[string]string{} // key -> first step delivered
	swapped := 0
	for range 2 * keys {
		job, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		step := string(job.Payload)
		if _, ok := seen[job.SequenceKey]; !ok {
			seen[job.SequenceKey] = step
			if step != `"1-created"` {
				swapped++
			}
		}
		if err := q.Ack(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if swapped > 0 {
		t.Fatalf("%d of %d keys delivered \"2-paid\" before \"1-created\": two jobs enqueued in order inside one clock tick "+
			"share a CreatedAt, and the tie is broken by their random IDs, not by enqueue order", swapped, keys)
	}
}
