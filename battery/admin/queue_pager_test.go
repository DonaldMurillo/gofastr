package admin

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

var jobRowIDRe = regexp.MustCompile(`<tr id="(q[0-9])"`)

// jobIDsShown lists the job rows a page draws, in order.
func jobIDsShown(body string) []string {
	var out []string
	for _, m := range jobRowIDRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// The Jobs page pages back through older jobs, newest first; a page turn
// keeps the status, and a page past the end shows the last one.
func TestQueuePagesOlderJobs(t *testing.T) {
	q := newDBQueue(t, newDB(t))
	now := time.Now().UTC()
	for i, id := range []string{"q1", "q2", "q3", "q4", "q5"} {
		if err := q.Enqueue(context.Background(), queue.Job{ID: id, Type: "send.email", CreatedAt: now.Add(time.Duration(i) * time.Second), ScheduledAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	x := setup(t, nil, Config{Queue: q, QueueListLimit: 2}, nil)
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"status=pending", []string{"q5", "q4"}},
		{"status=pending&p=2", []string{"q3", "q2"}},
		{"status=pending&p=3", []string{"q1"}},
		{"status=pending&p=99", []string{"q1"}},
		{"status=claimed&p=2", nil},
	} {
		body := get(x.as(theAdmin), "/admin/queue?"+c.query).Body.String()
		if got := jobIDsShown(body); !slices.Equal(got, c.want) {
			t.Errorf("%s shows %v, want %v", c.query, got, c.want)
		}
	}
	body := get(x.as(theAdmin), "/admin/queue?status=pending").Body.String()
	next := regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>3</a>`).FindStringSubmatch(body)
	if next == nil {
		t.Fatalf("the first page has no link to page 3:\n%s", body)
	}
	if href := html.UnescapeString(next[1]); !strings.Contains(href, "status=pending") || !strings.Contains(href, "p=3") {
		t.Errorf("page 3's link %q drops the status or the page", href)
	}
}

// Replay all re-queues every failed job, not the first page of them.
func TestQueueReplayAllPassesOnePage(t *testing.T) {
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = fmt.Sprintf("j%d", i)
	}
	q := &fakeQueue{jobs: failedJobs(ids...)}
	x := setup(t, nil, Config{Queue: q, QueueListLimit: 2}, nil)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay_all", nil)); got != "replayed-all" {
		t.Fatalf("result = %q", got)
	}
	if !slices.Equal(q.replayed, ids) {
		t.Fatalf("replayed %v, want %v", q.replayed, ids)
	}
}

// A backend that answers every offset with the same page ends the run
// instead of spinning on it.
func TestQueueReplayAllStopsOnARepeat(t *testing.T) {
	q := &fakeQueue{jobs: failedJobs("j1", "j2"), ignoreOffset: true}
	x := setup(t, nil, Config{Queue: q, QueueListLimit: 2}, nil)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay_all", nil)); got != "replayed-all" {
		t.Fatalf("result = %q", got)
	}
	if !slices.Equal(q.replayed, []string{"j1", "j2"}) {
		t.Fatalf("replayed %v, want each job once", q.replayed)
	}
}

// Replay all stops at maxReplayAll jobs a click.
func TestQueueReplayAllIsBounded(t *testing.T) {
	ids := make([]string, maxReplayAll+3)
	for i := range ids {
		ids[i] = fmt.Sprintf("j%d", i)
	}
	q := &fakeQueue{jobs: failedJobs(ids...)}
	x := setup(t, nil, Config{Queue: q, QueueListLimit: 7}, nil)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay_all", nil)); got != "replayed-all" {
		t.Fatalf("result = %q", got)
	}
	if len(q.replayed) != maxReplayAll {
		t.Fatalf("replayed %d jobs, want %d", len(q.replayed), maxReplayAll)
	}
}
