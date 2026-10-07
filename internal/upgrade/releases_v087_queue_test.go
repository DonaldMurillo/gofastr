package upgrade_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// queueKit is battery/queue's Browsable surface with ListJobs spelled
// limitArgs: the v0.86 shape, then the v0.87 one.
func queueKit(limitArgs string) map[string]string {
	return map[string]string{"battery/queue/queue.go": `package queue

import "context"

type Job struct{ ID string }

type JobStats map[string]int

type Browsable interface {
	ListJobs(ctx context.Context, status string, ` + limitArgs + `) ([]Job, error)
	Stats(ctx context.Context) (JobStats, error)
}

type DBQueue struct{}

func (q *DBQueue) ListJobs(ctx context.Context, status string, ` + limitArgs + `) ([]Job, error) {
	return nil, nil
}

func (q *DBQueue) Stats(ctx context.Context) (JobStats, error) { return nil, nil }

type MemoryQueue struct{}

func (q *MemoryQueue) ListJobs(_ context.Context, status string, ` + limitArgs + `) ([]Job, error) {
	return nil, nil
}

func (q *MemoryQueue) Stats(_ context.Context) (JobStats, error) { return nil, nil }
`}
}

// The ListJobs note hits a call through the interface and through a
// concrete queue, and is silent on the calls the guidance migrates
// them to.
func TestV087ListJobsNoteHitsOldCalls(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	var n *upgrade.Note
	for _, rel := range reg.Releases {
		for i := range rel.Notes {
			if rel.Version == "v0.87.0" && strings.HasPrefix(rel.Notes[i].Change, "queue.Browsable.ListJobs") {
				n = rel.Notes[i]
			}
		}
	}
	if n == nil {
		t.Fatal("no v0.87.0 ListJobs note")
	}
	src := func(extra string) map[string]string {
		return map[string]string{"jobs.go": `package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

func failed(ctx context.Context, b queue.Browsable, d *queue.DBQueue, m *queue.MemoryQueue) int {
	x, _ := b.ListJobs(ctx, "failed", 10` + extra + `)
	y, _ := d.ListJobs(ctx, "", 10` + extra + `)
	z, _ := m.ListJobs(ctx, "failed", 10` + extra + `)
	return len(x) + len(y) + len(z)
}
`}
	}
	old := scantest.App(t, src(""), scantest.Options{Kit: queueKit("limit int")})
	res := scantest.Run(t, old, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("old-shape app did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
	}
	if got := scantest.Hits(res, n); len(got) != 3 {
		t.Fatalf("hits = %v, want the three old ListJobs calls", got)
	}
	fixed := scantest.App(t, src(", 0"), scantest.Options{Kit: queueKit("limit, offset int")})
	res = scantest.Run(t, fixed, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if !res.TypeChecked {
		t.Fatalf("fixed app did not type-check: broken=%v unexplained=%v", res.Broken, res.Unexplained)
	}
	if got := scantest.Hits(res, n); len(got) != 0 {
		t.Fatalf("fires on the migrated calls: %v", got)
	}
}
