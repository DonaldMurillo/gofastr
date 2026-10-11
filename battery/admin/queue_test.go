package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// fakeQueue is a Browsable and Replayable queue with scripted answers.
type fakeQueue struct {
	jobs      []queue.Job
	stats     queue.JobStats
	listErr   error
	statsErr  error
	replayErr error
	statuses  []string // the status each ListJobs asked for
	replayed  []string
	// ignoreOffset answers every offset with the first page.
	ignoreOffset bool
}

func (q *fakeQueue) ListJobs(_ context.Context, status string, limit, offset int) ([]queue.Job, error) {
	q.statuses = append(q.statuses, status)
	if q.listErr != nil {
		return nil, q.listErr
	}
	if q.ignoreOffset {
		offset = 0
	}
	from := min(offset, len(q.jobs))
	return q.jobs[from:min(from+limit, len(q.jobs))], nil
}

func (q *fakeQueue) Stats(context.Context) (queue.JobStats, error) { return q.stats, q.statsErr }

func (q *fakeQueue) Replay(_ context.Context, id string) error {
	if q.replayErr != nil {
		return q.replayErr
	}
	q.replayed = append(q.replayed, id)
	return nil
}

// browseOnly hides Replay.
type browseOnly struct{ q *fakeQueue }

func (b browseOnly) ListJobs(ctx context.Context, s string, n, off int) ([]queue.Job, error) {
	return b.q.ListJobs(ctx, s, n, off)
}
func (b browseOnly) Stats(ctx context.Context) (queue.JobStats, error) { return b.q.Stats(ctx) }

func failedJobs(ids ...string) []queue.Job {
	out := make([]queue.Job, len(ids))
	for i, id := range ids {
		out[i] = queue.Job{ID: id, Type: "send.email", MaxAttempts: 3, Attempts: 3, CreatedAt: time.Now(), Status: "failed"}
	}
	return out
}

func queueEnv(t *testing.T, q queue.Browsable) *env {
	return setup(t, nil, Config{Queue: q}, nil)
}

func TestQueuePageListsJobsWithCounts(t *testing.T) {
	q := newDBQueue(t, newDB(t))
	for range 3 {
		if err := q.Enqueue(context.Background(), queue.Job{Type: "send.email", ScheduledAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	x := queueEnv(t, q)
	body := get(x.as(theAdmin), "/admin/queue").Body.String()
	if !strings.Contains(body, "send.email") || !strings.Contains(body, `Pending<span class="fui-seglinks__count">3</span>`) {
		t.Fatalf("jobs page lacks the job or its count:\n%s", body)
	}
}

// Request text never reaches ListJobs: an unknown status reads as All.
func TestQueueStatusIsOneOfTheKnownSet(t *testing.T) {
	q := &fakeQueue{}
	x := queueEnv(t, q)
	get(x.as(theAdmin), "/admin/queue?status=failed")
	get(x.as(theAdmin), "/admin/queue?status="+url.QueryEscape("x' OR 1=1"))
	if !slices.Equal(q.statuses, []string{"failed", ""}) {
		t.Fatalf("ListJobs saw %q, want [failed \"\"]", q.statuses)
	}
}

func TestQueueLoadFailureHidesDriverText(t *testing.T) {
	q := &fakeQueue{listErr: errors.New("dial tcp db.internal:5432: secret-dsn")}
	x := queueEnv(t, q)
	for _, p := range []string{"/admin/queue", "/admin"} {
		body := get(x.as(theAdmin), p).Body.String()
		if strings.Contains(body, "secret-dsn") || strings.Contains(body, "db.internal") {
			t.Errorf("%s leaked the driver error", p)
		}
		if !strings.Contains(body, "Could not load jobs") {
			t.Errorf("%s shows no load-failure notice:\n%s", p, body)
		}
	}
}

// Stats can come back partly filled beside its error; no count shows.
func TestQueueStatsErrorDropsCounts(t *testing.T) {
	q := &fakeQueue{stats: queue.JobStats{"pending": 7, "failed": 4}, statsErr: errors.New("rows error mid-scan")}
	x := queueEnv(t, q)
	if body := get(x.as(theAdmin), "/admin/queue").Body.String(); strings.Contains(body, "fui-seglinks__count") {
		t.Error("the jobs page showed a count from a failed Stats")
	}
	q.jobs = failedJobs("j1")
	if body := get(x.as(theAdmin), "/admin").Body.String(); strings.Contains(body, ">4<") {
		t.Error("the dashboard showed a failed count from a failed Stats")
	}
}

// A failed row replays in every view, and the header replays every
// failed job whenever there is one; nothing else replays.
func TestQueueReplayOffersOnFailedRows(t *testing.T) {
	jobs := append(failedJobs("j1", "j2"), queue.Job{ID: "p1", Type: "send.email", Status: "pending", CreatedAt: time.Now()})
	q := &fakeQueue{jobs: jobs, stats: queue.JobStats{"failed": 2, "pending": 1}}
	x := queueEnv(t, q)
	for _, page := range []string{"/admin/queue?status=failed", "/admin/queue"} {
		body := get(x.as(theAdmin), page).Body.String()
		if !strings.Contains(body, `action="/admin/queue/_replay/j1"`) || !strings.Contains(body, `action="/admin/queue/_replay_all"`) {
			t.Fatalf("%s offers no replay:\n%s", page, body)
		}
		if strings.Contains(body, "/_replay/p1") {
			t.Errorf("%s offers replay on a pending job", page)
		}
		if !strings.Contains(body, "Replay 2 failed") {
			t.Errorf("%s: the header does not count the failed jobs", page)
		}
	}
	q.stats = queue.JobStats{"pending": 1}
	q.jobs = jobs[2:]
	if body := get(x.as(theAdmin), "/admin/queue").Body.String(); strings.Contains(body, "/_replay") {
		t.Error("a queue with no failed job offers replay")
	}
	y := queueEnv(t, browseOnly{&fakeQueue{jobs: failedJobs("j1"), stats: queue.JobStats{"failed": 1}}})
	if body := get(y.as(theAdmin), "/admin/queue?status=failed").Body.String(); strings.Contains(body, "/_replay") {
		t.Error("a queue without Replay offers it")
	}
}

// The page filters by every status with its count, and each row shows
// its status, when it last changed and its last error.
func TestQueueColumnsAndFilters(t *testing.T) {
	updated := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	q := &fakeQueue{
		jobs: []queue.Job{{ID: "j1", Type: "webhook.deliver", Status: "failed", Attempts: 5, MaxAttempts: 5,
			UpdatedAt: updated, LastError: "webhook: 502 from https://hooks.example.com"}},
		stats: queue.JobStats{"pending": 1, "claimed": 2, "failed": 1, "done": 9},
	}
	x := queueEnv(t, q)
	body := get(x.as(theAdmin), "/admin/queue").Body.String()
	for _, want := range []string{
		`Pending<span class="fui-seglinks__count">1</span>`, `Running<span class="fui-seglinks__count">2</span>`,
		`Failed<span class="fui-seglinks__count">1</span>`, `Done<span class="fui-seglinks__count">9</span>`,
		`All<span class="fui-seglinks__count">13</span>`,
		// Each status is a link: picking one needs no Apply.
		`href="/admin/queue?status=claimed"`,
		">Status<", ">Updated<", ">Last error<",
		"webhook: 502 from https://hooks.example.com",
		`datetime="2026-10-09T12:00:00Z"`,
		">Failed<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("queue page misses %q", want)
		}
	}
	get(x.as(theAdmin), "/admin/queue?status=done")
	get(x.as(theAdmin), "/admin/queue?status=claimed")
	if got := q.statuses[len(q.statuses)-2:]; !slices.Equal(got, []string{"done", "claimed"}) {
		t.Errorf("ListJobs saw %q", got)
	}
}

func TestQueueReplayAuditsTheActor(t *testing.T) {
	q := &fakeQueue{}
	x := queueEnv(t, q)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay/job-911", nil)); got != "replayed" {
		t.Fatalf("result = %q, want replayed", got)
	}
	if !slices.Equal(q.replayed, []string{"job-911"}) {
		t.Fatalf("replayed %v", q.replayed)
	}
	var actor, record string
	if err := x.db.QueryRow(`SELECT actor_id, record_id FROM audit_log WHERE entity = 'queue' AND op = 'replay'`).Scan(&actor, &record); err != nil {
		t.Fatalf("no replay audit row: %v", err)
	}
	if actor != theAdmin.id || record != "job-911" {
		t.Fatalf("audit row = %s/%s", actor, record)
	}
}

func TestQueueReplayRPCAnswersAToast(t *testing.T) {
	q := &fakeQueue{}
	x := queueEnv(t, q)
	rr := rpc(x.as(theAdmin), "/admin/queue/_replay/j1", map[string]any{})
	if rr.Code != http.StatusNoContent || !strings.Contains(rr.Header().Get("X-Gofastr-Toast"), `"success"`) {
		t.Fatalf("RPC replay = %d, toast %q", rr.Code, rr.Header().Get("X-Gofastr-Toast"))
	}
	q.replayErr = errors.New("db down: secret")
	rr = rpc(x.as(theAdmin), "/admin/queue/_replay/j1", map[string]any{})
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("failed RPC replay = %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"error"`) {
		t.Fatalf("failed RPC replay has no error body: %s", rr.Body.String())
	}
}

func TestQueueReplayAllAuditsEachJob(t *testing.T) {
	q := &fakeQueue{jobs: failedJobs("j1", "j2", "j3")}
	x := queueEnv(t, q)
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay_all", nil)); got != "replayed-all" {
		t.Fatalf("result = %q", got)
	}
	if !slices.Equal(q.replayed, []string{"j1", "j2", "j3"}) || q.statuses[len(q.statuses)-1] != "failed" {
		t.Fatalf("replayed %v from status %v", q.replayed, q.statuses)
	}
	if ops := x.auditOps("queue"); len(ops) != 3 {
		t.Fatalf("audit ops = %v, want three replays", ops)
	}
}

func TestQueueReplayWithoutReplayable(t *testing.T) {
	x := queueEnv(t, browseOnly{&fakeQueue{}})
	if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay/j1", nil)); got != "replay-unsupported" {
		t.Fatalf("result = %q", got)
	}
	if ops := x.auditOps("queue"); len(ops) != 0 {
		t.Fatalf("an unsupported replay audited %v", ops)
	}
}

func TestQueueUnwiredHasNoPage(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	if rr := get(x.as(theAdmin), "/admin/queue"); rr.Code != http.StatusNotFound {
		t.Errorf("unwired /admin/queue = %d, want 404", rr.Code)
	}
	if rr := post(x.as(theAdmin), "/admin/queue/_replay/j1", nil); rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("unwired replay = %d, want 404", rr.Code)
	}
	if body := get(x.as(theAdmin), "/admin").Body.String(); strings.Contains(body, `href="/admin/queue"`) {
		t.Error("the sidebar links an unwired queue")
	}
}

// With a Queue the strip ends with Failed jobs, linking to the failed
// filter and saying it needs a replay; the failed rows, each with
// Replay, sit in Needs attention, and there is no separate card.
func TestDashboardFailedJobsFigure(t *testing.T) {
	q := &fakeQueue{jobs: failedJobs("j1", "j2"), stats: queue.JobStats{"failed": 2}}
	x := setup(t, nil, Config{Queue: q}, nil)
	dash := get(x.as(theAdmin), "/admin").Body.String()
	for _, want := range []string{"Failed jobs", `href="/admin/queue?status=failed"`, "Needs a replay", "fui-stat-card__trend--down", `data-cui-poll-src="/admin/_metric/jobs"`} {
		if !strings.Contains(dash, want) {
			t.Errorf("the dashboard lacks %q:\n%s", want, dash)
		}
	}
	att := dash[strings.Index(dash, "Needs attention"):]
	if !strings.Contains(att, "/admin/queue/_replay/j1") {
		t.Errorf("the failed rows are not in Needs attention:\n%s", att)
	}
	if strings.Contains(dash, ">Failed jobs</h2>") {
		t.Errorf("Failed jobs is still a card of its own too:\n%s", dash)
	}
	if body := get(x.as(theAdmin), "/admin/_metric/jobs").Body.String(); !strings.Contains(body, ">2<") {
		t.Errorf("the polled figure = %q", body)
	}
	if rr := get(x.as(aReader), "/admin/_metric/jobs"); rr.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a reader read the jobs figure: %d", rr.Code)
	}
}
