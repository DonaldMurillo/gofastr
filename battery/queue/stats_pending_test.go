package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// bareRedis is a client with the required RedisClient methods only.
type bareRedis struct{ RedisClient }

// rangeLog records the widest LRANGE a client was asked for.
type rangeLog struct {
	ranges int
	whole  bool  // a read to the end of the list (a negative stop from 0)
	widest int64 // the most entries one read asked for
}

func (l *rangeLog) note(start, stop int64) {
	l.ranges++
	if start >= 0 && stop < 0 {
		l.whole = true
		return
	}
	l.widest = max(l.widest, stop-start+1)
}

// countingLRange is a client with no LLEN, logging its LRANGEs.
type countingLRange struct {
	RedisClient
	rangeLog
}

func (c *countingLRange) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	c.note(start, stop)
	return c.RedisClient.LRange(ctx, key, start, stop)
}

// lengthRedis is the mock, LLEN included, logging its LRANGEs. between
// runs after each LRANGE has read its page.
type lengthRedis struct {
	*mockRedis
	rangeLog
	between func()
}

func (c *lengthRedis) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	c.note(start, stop)
	out, err := c.mockRedis.LRange(ctx, key, start, stop)
	if c.between != nil {
		c.between()
	}
	return out, err
}

// seedDead puts n dead jobs d0..d(n-1) on name's dead list, d(n-1) newest.
func seedDead(t *testing.T, client RedisClient, name string, n int) {
	t.Helper()
	for i := range n {
		raw, err := json.Marshal(Job{ID: fmt.Sprintf("d%d", i), Type: "a", Attempts: 1, MaxAttempts: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.LPush(context.Background(), name+":dead", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

// RedisQueue counted its lists with LRANGE 0 -1, pulling every payload to
// take a length on each scrape and each admin page. It counts with LLEN
// alone: without it Stats is an error and a scrape emits nothing.
func TestRedisQueueCountsNeedLLEN(t *testing.T) {
	ctx := context.Background()
	const dead = 3 * maxSkipDrain
	seed := func(t *testing.T, client RedisClient) *RedisQueue {
		q := NewRedisQueue(client, "depth-llen")
		t.Cleanup(func() { _ = q.Close() })
		for _, job := range []Job{{Type: "a"}, {Type: "a"}, {Type: "a", ScheduledAt: time.Now().Add(time.Hour)}} {
			if err := q.Enqueue(ctx, job); err != nil {
				t.Fatal(err)
			}
		}
		seedDead(t, client, "depth-llen", dead)
		return q
	}
	t.Run("client with LLEN", func(t *testing.T) {
		counting := &lengthRedis{mockRedis: newMockRedis()}
		q := seed(t, counting)
		var buf bytes.Buffer
		MetricsCollector(q, "r")(&buf)
		for _, want := range []string{`queue_depth{lane="r"} 3`, fmt.Sprintf(`queue_dead_letter_jobs{lane="r"} %d`, dead)} {
			if !strings.Contains(buf.String(), want) {
				t.Fatalf("metrics output missing %q\ngot:\n%s", want, buf.String())
			}
		}
		stats, err := q.Stats(ctx)
		if err != nil || stats["failed"] != dead {
			t.Fatalf("Stats = %v, %v; want failed=%d", stats, err, dead)
		}
		if counting.ranges != 0 {
			t.Fatalf("a scrape and a Stats ran %d LRANGEs, want none", counting.ranges)
		}
	})
	t.Run("client without LLEN", func(t *testing.T) {
		counting := &countingLRange{RedisClient: bareRedis{newMockRedis()}}
		q := seed(t, counting)
		var buf bytes.Buffer
		MetricsCollector(q, "r")(&buf)
		if buf.Len() != 0 {
			t.Fatalf("a client with no LLEN cannot count, want no samples:\n%s", buf.String())
		}
		if stats, err := q.Stats(ctx); !errors.Is(err, errNoListLength) || stats != nil {
			t.Fatalf("Stats = %v, %v; want no count and errNoListLength", stats, err)
		}
		if counting.ranges != 0 {
			t.Fatalf("a scrape and a Stats ran %d LRANGEs, want none", counting.ranges)
		}
	})
}

// Every RedisQueue read a request or a scrape reaches asks for a bounded
// page, with or without LLEN: none reads a list to its end.
func TestRedisQueueRequestPathsReadBoundedPages(t *testing.T) {
	ctx := context.Background()
	const dead = 3 * maxSkipDrain
	paths := map[string]func(q *RedisQueue) error{
		"Stats":            func(q *RedisQueue) error { _, _ = q.Stats(ctx); return nil },
		"MetricsCollector": func(q *RedisQueue) error { MetricsCollector(q, "r")(&bytes.Buffer{}); return nil },
		"ListJobs":         func(q *RedisQueue) error { _, err := q.ListJobs(ctx, "failed", 50, 50); return err },
		"ReplayJob newest": func(q *RedisQueue) error { return wantReplayed(q.ReplayJob(ctx, fmt.Sprintf("d%d", dead-1))) },
		"ReplayJob oldest": func(q *RedisQueue) error { return wantReplayed(q.ReplayJob(ctx, "d0")) },
		"ReplayJob absent": func(q *RedisQueue) error { _, err := q.ReplayJob(ctx, "nobody"); return err },
	}
	for name, run := range paths {
		for _, llen := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/llen=%v", name, llen), func(t *testing.T) {
				var client RedisClient
				var log *rangeLog
				if llen {
					c := &lengthRedis{mockRedis: newMockRedis()}
					client, log = c, &c.rangeLog
				} else {
					c := &countingLRange{RedisClient: bareRedis{newMockRedis()}}
					client, log = c, &c.rangeLog
				}
				q := NewRedisQueue(client, "bounded")
				t.Cleanup(func() { _ = q.Close() })
				seedDead(t, client, "bounded", dead)
				if err := run(q); err != nil {
					t.Fatal(err)
				}
				if log.whole || log.widest > maxSkipDrain+1 {
					t.Fatalf("read a list to its end (%v) or %d entries in one LRANGE, want at most %d", log.whole, log.widest, maxSkipDrain+1)
				}
			})
		}
	}
}

func wantReplayed(replayed bool, err error) error {
	if err == nil && !replayed {
		return errors.New("ReplayJob reported nothing queued for a dead job")
	}
	return err
}

// ReplayJob pages the dead list. A list that shifts between two pages could
// hide an entry, so the scan starts over, and gives up with an error when
// the list never holds still.
func TestRedisReplayRestartsWhenTheDeadListShifts(t *testing.T) {
	ctx := context.Background()
	const dead = 3 * maxSkipDrain
	setup := func(t *testing.T) (*lengthRedis, *RedisQueue) {
		client := &lengthRedis{mockRedis: newMockRedis()}
		q := NewRedisQueue(client, "shift")
		t.Cleanup(func() { _ = q.Close() })
		seedDead(t, client, "shift", dead)
		return client, q
	}
	dropHead := func(client *lengthRedis) {
		head, _ := client.mockRedis.LRange(ctx, "shift:dead", 0, 0)
		if len(head) == 1 {
			_, _ = client.LRem(ctx, "shift:dead", 1, head[0])
		}
	}
	t.Run("one shift", func(t *testing.T) {
		client, q := setup(t)
		// The first entry of the second page moves onto the first page's
		// end once the head is gone: a scan that kept its offsets skips it.
		target := fmt.Sprintf("d%d", dead-1-deadScanFirst)
		client.between = func() {
			if client.ranges == 1 {
				dropHead(client)
			}
		}
		replayed, err := q.ReplayJob(ctx, target)
		if err != nil || !replayed {
			t.Fatalf("ReplayJob = %v, %v; want the job found after the list shifted", replayed, err)
		}
	})
	t.Run("never still", func(t *testing.T) {
		client, q := setup(t)
		client.between = func() { dropHead(client) }
		replayed, err := q.ReplayJob(ctx, "d0")
		if !errors.Is(err, errDeadListMoved) || replayed {
			t.Fatalf("ReplayJob = %v, %v; want errDeadListMoved", replayed, err)
		}
	})
}

// Stats counts the jobs ListJobs can list, on every backend: a pager sums
// its keys and pages by them. MemoryQueue and RedisQueue list dead-letter
// jobs only, so there a waiting job shows up under no key; DBQueue lists
// every status it counts.
func TestStatsKeysAreListableOnEveryBackend(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryQueue(0)
	t.Cleanup(func() { _ = mem.Close() })
	red := NewRedisQueue(newMockRedis(), "listable")
	t.Cleanup(func() { _ = red.Close() })
	db := newDurableTestQueue(t, openDurableSchedulerDB(t))
	for name, q := range map[string]interface {
		Queue
		Browsable
	}{"memory": mem, "redis": red, "db": db} {
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
