package a

import "context"

// A test that drives a handler and acks on its own context is not a
// shipped shutdown path.
func driveInTest(ctx context.Context, q *Queue, job Job, h Handler) error { // want driveInTest:`runsCallback\[0\]`
	if err := h(ctx, job); err != nil {
		return err
	}
	return q.Ack(ctx, job)
}
