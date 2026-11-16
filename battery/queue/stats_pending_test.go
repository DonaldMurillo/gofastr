package queue

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// queue_depth read Stats' "pending" key, which MemoryQueue and RedisQueue
// do not report, so it was 0 for them whatever the backlog. Every backend
// now gives the gauge its waiting jobs, a delayed one included.
func TestQueueDepthMetricOnEveryBackend(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryQueue(0)
	t.Cleanup(func() { _ = mem.Close() })
	red := NewRedisQueue(newMockRedis(), "depth")
	t.Cleanup(func() { _ = red.Close() })
	db := newDurableTestQueue(t, openDurableSchedulerDB(t))
	for name, q := range map[string]interface {
		Queue
		Browsable
	}{"memory": mem, "redis": red, "db": db} {
		t.Run(name, func(t *testing.T) {
			for _, job := range []Job{{Type: "a"}, {Type: "a"}, {Type: "a", ScheduledAt: time.Now().Add(time.Hour)}} {
				if err := q.Enqueue(ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			MetricsCollector(q, name)(&buf)
			if want := `queue_depth{lane="` + name + `"} 3`; !strings.Contains(buf.String(), want) {
				t.Fatalf("metrics output missing %q\ngot:\n%s", want, buf.String())
			}
		})
	}
}

// Stats counts the jobs ListJobs can list: a pager sums its keys and pages
// by them. MemoryQueue and RedisQueue list dead-letter jobs only, so a
// waiting job must not show up under any key.
func TestStatsKeysAreListableOnMemoryAndRedis(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryQueue(0)
	t.Cleanup(func() { _ = mem.Close() })
	red := NewRedisQueue(newMockRedis(), "listable")
	t.Cleanup(func() { _ = red.Close() })
	for name, q := range map[string]interface {
		Queue
		Browsable
	}{"memory": mem, "redis": red} {
		t.Run(name, func(t *testing.T) {
			for _, job := range []Job{
				{ID: "dead", Type: "a", MaxAttempts: 1},
				{ID: "waiting", Type: "b"},
				{ID: "later", Type: "b", ScheduledAt: time.Now().Add(time.Hour)},
			} {
				if err := q.Enqueue(ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			job, err := q.Dequeue(ctx, "a")
			if err != nil {
				t.Fatal(err)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			stats, err := q.Stats(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(stats) == 0 {
				t.Fatal("Stats is empty, want the dead job counted")
			}
			total := 0
			for status, n := range stats {
				total += n
				jobs, err := q.ListJobs(ctx, status, 100, 0)
				if err != nil {
					t.Fatal(err)
				}
				if len(jobs) != n {
					t.Errorf("Stats[%q] = %d, but ListJobs(%q) lists %d", status, n, status, len(jobs))
				}
			}
			all, err := q.ListJobs(ctx, "", 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != total {
				t.Errorf("Stats sums to %d, but ListJobs(\"\") lists %d", total, len(all))
			}
		})
	}
}

// The dead-letter count falls on a Replay, so it is a gauge: as a counter
// named _total, every fall read as a counter reset.
func TestDeadLetterMetricIsAGauge(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue(0)
	t.Cleanup(func() { _ = q.Close() })
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "a", MaxAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatal(err)
	}
	scrape := func() string {
		var buf bytes.Buffer
		MetricsCollector(q, "mem")(&buf)
		return buf.String()
	}
	out := scrape()
	for _, want := range []string{"# TYPE queue_dead_letter_jobs gauge", `queue_dead_letter_jobs{lane="mem"} 1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "_total") {
		t.Fatalf("a value that falls must not be named _total:\n%s", out)
	}
	if err := q.Replay(ctx, "j1"); err != nil {
		t.Fatal(err)
	}
	if out := scrape(); !strings.Contains(out, `queue_dead_letter_jobs{lane="mem"} 0`) {
		t.Fatalf("after Replay the gauge should read 0\ngot:\n%s", out)
	}
}

// DBQueue.ListJobs left UserID empty; MemoryQueue and RedisQueue returned it.
func TestListJobsCarriesUserIDOnEveryBackend(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryQueue(0)
	t.Cleanup(func() { _ = mem.Close() })
	red := NewRedisQueue(newMockRedis(), "list-user")
	t.Cleanup(func() { _ = red.Close() })
	db := newDurableTestQueue(t, openDurableSchedulerDB(t))
	for name, q := range map[string]interface {
		Queue
		Browsable
	}{"memory": mem, "redis": red, "db": db} {
		t.Run(name, func(t *testing.T) {
			if err := q.Enqueue(ctx, Job{ID: "j1", Type: "a", UserID: "u1", MaxAttempts: 1}); err != nil {
				t.Fatal(err)
			}
			job, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := q.Nack(ctx, job); err != nil {
				t.Fatal(err)
			}
			jobs, err := q.ListJobs(ctx, "failed", 10, 0)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("ListJobs: %d jobs, err %v; want the one dead job", len(jobs), err)
			}
			if jobs[0].UserID != "u1" {
				t.Fatalf("ListJobs UserID = %q, want u1", jobs[0].UserID)
			}
		})
	}
}

// The in-memory builder returned nil and registered nothing with no job
// type; the durable builder refused it. Both refuse it.
func TestRegisterRefusesAScheduleWithNoJobType(t *testing.T) {
	mem := NewScheduler(NewMemoryQueue(0))
	t.Cleanup(func() { _ = mem.Close() })
	durable, err := NewDurableScheduler(newDurableTestQueue(t, openDurableSchedulerDB(t)), DurableSchedulerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for name, register := range map[string]func() error{
		"memory every":  func() error { return mem.Every(time.Minute).Register() },
		"memory cron":   func() error { return mem.Cron("* * * * *").Job("", nil).Register() },
		"durable every": func() error { return durable.Every("a", time.Minute).Register() },
		"durable cron":  func() error { return durable.Cron("b", "* * * * *").Job("", nil).Register() },
	} {
		if err := register(); err == nil {
			t.Errorf("%s: Register with no job type returned nil", name)
		}
	}
	if n := len(mem.schedules); n != 0 {
		t.Errorf("in-memory scheduler holds %d schedules, want 0", n)
	}
}
