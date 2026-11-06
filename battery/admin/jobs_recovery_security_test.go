package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

type bulkRetryCapture struct {
	handlerQueue
	jobs []queue.Job
	err  error
}

func (capture *bulkRetryCapture) Enqueue(_ context.Context, job queue.Job) error {
	capture.jobs = append(capture.jobs, job)
	return capture.err
}

func TestBulkLeaseRetriesAreDelayed(t *testing.T) {
	for _, mode := range []string{"busy", "lost", "enqueue-refused"} {
		t.Run(mode, func(t *testing.T) {
			environment := newJobsEnv(t, 120)
			environment.setRoles("u1", "admin", "editor")
			code, output := environment.confirmBulk(t, "u1", 120)
			if code != http.StatusAccepted {
				t.Fatalf("confirm=%d %v", code, output)
			}
			jobID, _ := output["job"].(string)
			ctx := context.Background()
			job, err := environment.q.Dequeue(ctx, bulkJobType)
			if err != nil {
				t.Fatal(err)
			}
			job.Attempts = bulkJobMaxAttempts
			job.ClaimToken = job.ID + "-old"
			capture := &bulkRetryCapture{handlerQueue: environment.q}
			runner := environment.b.cfg.BulkJobs
			runner.q = capture
			refused := errors.New("queue unavailable")
			if mode == "enqueue-refused" {
				capture.err = refused
			}
			if mode == "lost" {
				runner.principal = func(ctx context.Context, userID, tenantID string) (context.Context, error) {
					if _, err := environment.db.Exec(`UPDATE gofastr_bulk_jobs SET claim_token = ?, lease_until = ? WHERE id = ?`, job.ID+"-other", time.Now().UTC().Add(5*time.Minute), jobID); err != nil {
						return nil, err
					}
					return environment.principal(ctx, userID, tenantID)
				}
			} else if _, err := environment.db.Exec(`UPDATE gofastr_bulk_jobs SET claim_token = ?, lease_until = ? WHERE id = ?`, job.ID+"-dead", time.Now().UTC().Add(3*time.Minute), jobID); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			err = runner.run(ctx, job)
			if capture.err != nil {
				if !errors.Is(err, refused) {
					t.Errorf("retry enqueue failure=%v, want %v", err, refused)
				}
			} else if err != nil {
				t.Errorf("lease contention consumed queue attempt: %v", err)
			}
			if len(capture.jobs) != 1 {
				t.Fatalf("delayed followups=%d, want 1", len(capture.jobs))
			}
			next := capture.jobs[0]
			if next.ScheduledAt.Before(started.Add(5 * time.Minute)) {
				t.Errorf("retry scheduled at %v, want at least one bulk lease after %v", next.ScheduledAt, started)
			}
			if next.ID != "" || next.ClaimToken != "" || next.Attempts != 0 || next.MaxAttempts != bulkJobMaxAttempts || next.Type != job.Type || string(next.Payload) != string(job.Payload) || next.UserID != "u1" {
				t.Errorf("retry did not preserve identity with a fresh attempt budget: %+v", next)
			}
			if status := environment.bulkJobStatus(t, jobID); status != entityui.BulkQueued {
				t.Errorf("contended snapshot status=%s, want queued", status)
			}
		})
	}
}

func TestBulkResumeExpiredEnqueuedJob(t *testing.T) {
	environment := buildJobsEnv(t, 2)
	environment.setRoles("u1", "admin", "editor")
	old := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := environment.db.Exec(`INSERT INTO gofastr_bulk_jobs (id, entity, action, count, creator, tenant, filter_hash, run_key, status, enqueued, created_at, claim_token, lease_until)
		VALUES ('abandoned', 'posts', 'delete', 2, 'u1', '', 'h', 'k', 'queued', 1, ?, 'dead-worker', ?)`, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Exec(`INSERT INTO gofastr_bulk_items (job_id, record_id, seq, outcome) VALUES ('abandoned', 'p000', 0, 'done'), ('abandoned', 'p001', 1, NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.db.Exec(`DELETE FROM posts WHERE id = 'p000'`); err != nil {
		t.Fatal(err)
	}
	environment.init(t)
	job, err := environment.q.Dequeue(context.Background(), bulkJobType)
	if err != nil {
		t.Fatalf("expired enqueued snapshot was not resumed: %v", err)
	}
	if err := environment.b.cfg.BulkJobs.run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var done int
	if err := environment.db.QueryRow(`SELECT done FROM gofastr_bulk_jobs WHERE id = 'abandoned'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if environment.bulkJobStatus(t, "abandoned") != entityui.BulkDone || done != 2 || environment.countPosts(t) != 0 {
		t.Fatalf("resumed snapshot status=%s done=%d posts=%d", environment.bulkJobStatus(t, "abandoned"), done, environment.countPosts(t))
	}
	deletes := 0
	for _, operation := range environment.auditOps("posts") {
		if operation == "delete" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("resumed deletes=%d, want only the unsettled row", deletes)
	}
}
