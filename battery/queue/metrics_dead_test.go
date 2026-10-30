package queue

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// A job that spent its attempts counts in queue_dead_letter_total. The
// collector read a "dead" key no backend writes; every backend files a
// dead-lettered job under "failed", so the metric stayed 0.
func TestDeadLetterMetricCountsFailed(t *testing.T) {
	ctx := context.Background()
	q := newDurableTestQueue(t, openDurableSchedulerDB(t))
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "email", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx, "email")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	MetricsCollector(q, "default")(&buf)
	if want := `queue_dead_letter_total{lane="default"} 1`; !strings.Contains(buf.String(), want) {
		t.Fatalf("metrics output missing %q\ngot:\n%s", want, buf.String())
	}
}
