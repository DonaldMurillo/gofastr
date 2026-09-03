package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// partialStatsQueue is a Browsable whose Stats returns DBQueue's failure
// shape: a PARTIALLY populated map alongside the error (rows.Err() after
// some rows scanned). Both stats-rendering screens must drop the
// partial counts and degrade to the zero-count UI, not present stale
// numbers the caller has no reason to distrust.
type partialStatsQueue struct{}

func (partialStatsQueue) ListJobs(_ context.Context, _ string, _ int) ([]queue.Job, error) {
	return nil, nil
}

func (partialStatsQueue) Stats(_ context.Context) (queue.JobStats, error) {
	return queue.JobStats{"pending": 7}, errors.New("rows error mid-scan")
}

func TestAdmin_StatsErrorClearsPartialCounts(t *testing.T) {
	b := New(Config{Queue: partialStatsQueue{}})

	// Overview tile: the partial "pending: 7" StatCard must not ship.
	partialCard := string(statCard("pending", 7))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	b.handleIndex(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("handleIndex status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), partialCard) {
		t.Errorf("overview shipped the partially populated stats (pending=7) alongside the Stats error:\n%s", truncateBody(rr.Body.String()))
	}

	// Queue page filter chips: "pending (7)" must not ship either.
	req = httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr = httptest.NewRecorder()
	b.handleQueue(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("handleQueue status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "pending (7)") {
		t.Errorf("queue page shipped the partially populated stats (pending (7)) alongside the Stats error:\n%s", truncateBody(rr.Body.String()))
	}
}

func truncateBody(s string) string {
	if len(s) > 800 {
		return s[:800] + "…"
	}
	return s
}
