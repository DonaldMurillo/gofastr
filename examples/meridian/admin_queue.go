package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Meridian's background jobs: one database queue runs the billing work
// (invoice mail, payment sync, webhooks) and the admin's queued bulk
// runs, and the admin's Queue page lists it. Done jobs stay a day so
// the page shows what ran.

// adminQueue and adminBulkJobs are the queue and the bulk runner the
// admin reads; nil without a database.
var (
	adminQueue    *queue.DBQueue
	adminBulkJobs *admin.BulkJobs
)

// doneJobsKept is how long a finished job stays on the Queue page.
const doneJobsKept = 24 * time.Hour

// demoJob is a billing job's payload: the record it is about, and for a
// webhook the endpoint it posts to.
type demoJob struct {
	Record string `json:"record"`
	URL    string `json:"url,omitempty"`
}

// setupQueue opens the queue, registers the billing handlers and wires
// the bulk runner into the entity UI's extensions. Call it before the
// entity UI is built; the queue starts with the app and stops with it.
func setupQueue(fwApp *framework.App, db *sql.DB) {
	if db == nil {
		return
	}
	q, err := queue.NewDBQueue(db, queue.WithDoneRetention(doneJobsKept), queue.WithDBHandlerTimeout(30*time.Second))
	if err != nil {
		log.Fatalf("queue: %v", err)
	}
	q.RegisterHandler("invoice.send_email", func(context.Context, queue.Job) error { return nil })
	q.RegisterHandler("payments.sync", func(context.Context, queue.Job) error { return nil })
	q.RegisterHandler("mrr.recalc", func(context.Context, queue.Job) error { return nil })
	// The demo's webhook endpoints do not exist: every delivery fails and
	// dead-letters, so the Queue page has failures to replay.
	q.RegisterHandler("webhook.deliver", func(_ context.Context, j queue.Job) error {
		var p demoJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		return errors.New("webhook: 502 Bad Gateway from " + p.URL)
	})
	// The bulk runner rebuilds the confirming user from the auth
	// manager, which the generated wiring creates after this call.
	bulk, err := admin.NewBulkJobs(q, func(ctx context.Context, user, tenant string) (context.Context, error) {
		return admin.AuthPrincipal(authMgr)(ctx, user, tenant)
	})
	if err != nil {
		log.Fatalf("bulk jobs: %v", err)
	}
	appExtensions.Jobs = bulk
	adminQueue, adminBulkJobs = q, bulk

	fwApp.OnStart(func(ctx context.Context) error {
		seedJobs(ctx, q)
		q.Start(context.WithoutCancel(ctx))
		return nil
	})
	fwApp.OnStop(q.Close)
}

// seedJobs queues a few billing jobs on an empty queue, so a fresh demo
// has work to show: mail and syncs that succeed and webhooks that fail.
func seedJobs(ctx context.Context, q *queue.DBQueue) {
	if stats, err := q.Stats(ctx); err != nil || len(stats) > 0 {
		return
	}
	jobs := []struct {
		kind string
		p    demoJob
	}{
		{"invoice.send_email", demoJob{Record: "INV-1001"}},
		{"invoice.send_email", demoJob{Record: "INV-1002"}},
		{"payments.sync", demoJob{Record: "stripe"}},
		{"mrr.recalc", demoJob{Record: "all"}},
		{"webhook.deliver", demoJob{Record: "INV-1003", URL: "https://hooks.example.com/billing"}},
		{"webhook.deliver", demoJob{Record: "INV-1007", URL: "https://hooks.example.com/billing"}},
	}
	for _, j := range jobs {
		payload, _ := json.Marshal(j.p)
		if err := q.Enqueue(ctx, queue.Job{Type: j.kind, Payload: payload, MaxAttempts: 2}); err != nil {
			log.Printf("seed jobs: %v", err)
			return
		}
	}
	// One job waits an hour, so the page shows a pending row too.
	payload, _ := json.Marshal(demoJob{Record: "INV-1012"})
	_ = q.Enqueue(ctx, queue.Job{Type: "invoice.send_email", Payload: payload, ScheduledAt: time.Now().Add(time.Hour)})
}

// adminBrowsable is the queue the admin lists, or nil (no Queue page)
// without a database. A nil *DBQueue in the interface would not be nil.
func adminBrowsable() queue.Browsable {
	if adminQueue == nil {
		return nil
	}
	return adminQueue
}
