package queue

import "github.com/DonaldMurillo/gofastr/core/datexport"

// The queue battery owns a physical table that lives OUTSIDE the framework
// entity registry (it is created with raw DDL in db.go). Registering it
// means any app that imports battery/queue has its jobs table included in
// App.ExportData, so a data dump/restore is complete, and reached by
// App.EraseUserData on user_id. The framework centralizes all raw
// read/write behind one SafeIdent-guarded path; this registration is purely
// declarative.
//
// init registers the default "queue_jobs" table. NewDBQueue registers the
// table it was built with, under datexport.ScopedName("queue_jobs", table):
// a queue built WithTable("billing_jobs") used to sit outside both planes,
// so an erasure left that user's jobs, payloads included, in place.

func init() {
	registerJobsTable("queue_jobs")
}

// registerJobsTable registers table's exporter and user_id eraser. Both
// registries replace an entry of the same name, so registering a table
// again (a second DBQueue over it, the default table at init and at
// NewDBQueue) is a no-op.
func registerJobsTable(table string) {
	name := datexport.ScopedName("queue_jobs", table)
	datexport.Register(datexport.DataExporter{
		Name:       name,
		Source:     "queue",
		Table:      table,
		PrimaryKey: "id",
		Columns: []string{
			"id",
			"type",
			"payload",
			"priority",
			"lane",
			"attempts",
			"max_attempts",
			"created_at",
			"scheduled_at",
			"status",
			"claimed_at",
			"user_id",
			"unique_key",
		},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name:   name,
		Source: "queue",
		Table:  table,
		Column: "user_id",
		Mode:   datexport.EraseDelete,
	})
}
