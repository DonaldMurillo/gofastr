package queue

import (
	"context"
	"testing"
	"time"
)

type editingQueue struct{ Queue }

func (editingQueue) Enqueue(_ context.Context, job Job) error {
	job.Payload[0] = '!'
	return nil
}

func TestScheduledPayloadIsIndependentAcrossQueuesAndRuns(t *testing.T) {
	recorder := &recordQueue{}
	scheduler := NewScheduler(editingQueue{}, recorder)
	defer scheduler.Close()
	base := time.Now()
	if err := scheduler.Every(time.Second).Job("mail", `"queued"`).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	scheduler.dispatchDue(t.Context(), base.Add(time.Second))
	scheduler.dispatchDue(t.Context(), base.Add(2*time.Second))
	if len(recorder.jobs) != 2 {
		t.Fatalf("jobs = %v", recorder.jobs)
	}
	for _, job := range recorder.jobs {
		if string(job.Payload) != `"queued"` {
			t.Fatalf("payload = %q", job.Payload)
		}
	}
}
