package queue

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func jobIDs(jobs []Job) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}

// checkOffsets asks b for each window of the newest-first ids and wants
// exactly that window back; a negative offset reads as the first page.
func checkOffsets(t *testing.T, b Browsable, status string, newestFirst []string) {
	t.Helper()
	ctx := context.Background()
	for _, c := range []struct {
		limit, offset int
		want          []string
	}{
		{2, 0, newestFirst[0:2]},
		{2, 2, newestFirst[2:4]},
		{2, 4, newestFirst[4:]},
		{2, 9, nil},
		{2, -3, newestFirst[0:2]},
		{2, math.MaxInt, nil},
	} {
		got, err := b.ListJobs(ctx, status, c.limit, c.offset)
		if err != nil {
			t.Fatalf("ListJobs(%d, %d): %v", c.limit, c.offset, err)
		}
		if ids := jobIDs(got); !slices.Equal(ids, c.want) {
			t.Errorf("ListJobs(%d, %d) = %v, want %v", c.limit, c.offset, ids, c.want)
		}
	}
}

func TestDBListJobsOffset(t *testing.T) {
	_, q := openDBQueue(t, 0)
	checkDBOffsets(t, q)
}

// Postgres refuses a negative OFFSET where SQLite reads it as zero, so
// only this twin sees ListJobs pass one through.
func TestPGListJobsOffset(t *testing.T) {
	q, err := NewDBQueue(pgtest.DB(t))
	if err != nil {
		t.Fatal(err)
	}
	checkDBOffsets(t, q)
}

func checkDBOffsets(t *testing.T, q *DBQueue) {
	t.Helper()
	now := time.Now().UTC()
	for i, id := range []string{"j1", "j2", "j3", "j4", "j5"} {
		if err := q.Enqueue(context.Background(), Job{ID: id, Type: "x", CreatedAt: now.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	checkOffsets(t, q, "", []string{"j5", "j4", "j3", "j2", "j1"})
}

func TestMemoryListJobsOffset(t *testing.T) {
	q := NewMemoryQueue(1)
	defer q.Close()
	var ids []string
	for range 5 {
		ids = append(ids, driveToFailure(t, q))
	}
	slices.Reverse(ids)
	checkOffsets(t, q, "failed", ids)
}

func TestRedisListJobsOffset(t *testing.T) {
	q := NewRedisQueue(newMockRedis(), "offsets")
	ctx := context.Background()
	for _, id := range []string{"d1", "d2", "d3", "d4", "d5"} {
		_ = q.Enqueue(ctx, Job{ID: id, Type: "x", MaxAttempts: 1})
		job, _ := q.Dequeue(ctx)
		_ = q.Nack(ctx, job)
	}
	checkOffsets(t, q, "failed", []string{"d5", "d4", "d3", "d2", "d1"})
}
