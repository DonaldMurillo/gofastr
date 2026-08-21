package queue

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeBrowsable is a minimal Browsable for collector tests: it returns fixed
// counts (or a forced error) without touching a real queue backend.
type fakeBrowsable struct {
	stats JobStats
	err   error
}

func (f *fakeBrowsable) ListJobs(_ context.Context, _ string, _ int) ([]Job, error) {
	return nil, nil
}

func (f *fakeBrowsable) Stats(_ context.Context) (JobStats, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.stats, nil
}

// TestQueueMetricsCollector emits queue_depth and queue_dead_letter_total from
// a Browsable's Stats snapshot, labelled with the queue lane.
func TestQueueMetricsCollector(t *testing.T) {
	fake := &fakeBrowsable{stats: JobStats{"pending": 3, "dead": 1}}

	var buf bytes.Buffer
	MetricsCollector(fake, "ingest")(&buf)

	out := buf.String()
	for _, want := range []string{
		"# HELP queue_depth Jobs waiting to be processed.",
		"# TYPE queue_depth gauge",
		`queue_depth{lane="ingest"} 3`,
		"# HELP queue_dead_letter_total Jobs that exhausted their retry budget and were dead-lettered.",
		"# TYPE queue_dead_letter_total counter",
		`queue_dead_letter_total{lane="ingest"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestQueueMetricsCollector_StatsError emits nothing when Stats errors, a
// transient DB failure must never break /metrics.
func TestQueueMetricsCollector_StatsError(t *testing.T) {
	fake := &fakeBrowsable{err: errors.New("db is down")}

	var buf bytes.Buffer
	MetricsCollector(fake, "ingest")(&buf)

	if buf.Len() != 0 {
		t.Errorf("expected no output on Stats error, got:\n%s", buf.String())
	}
}

// TestQueueMetricsCollector_MissingKeys defaults absent status keys to 0.
func TestQueueMetricsCollector_MissingKeys(t *testing.T) {
	fake := &fakeBrowsable{stats: JobStats{}}

	var buf bytes.Buffer
	MetricsCollector(fake, "ingest")(&buf)

	out := buf.String()
	if !strings.Contains(out, `queue_depth{lane="ingest"} 0`) {
		t.Errorf("expected pending default 0, got:\n%s", out)
	}
	if !strings.Contains(out, `queue_dead_letter_total{lane="ingest"} 0`) {
		t.Errorf("expected dead default 0, got:\n%s", out)
	}
}
