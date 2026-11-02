package a

import (
	"context"
	"time"
)

// A test's retry loop is not a worker.
func retryInTest(q *queue) {
	for {
		if _, err := q.dequeue(context.Background()); err != nil {
			time.Sleep(time.Millisecond)
			continue
		}
		return
	}
}
