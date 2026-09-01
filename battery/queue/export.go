package queue

import "github.com/DonaldMurillo/gofastr/framework/datexport"

// The queue battery owns a physical table that lives OUTSIDE the framework
// entity registry (it is created with raw DDL in db.go). Registering it here
// from init(), mirroring framework/agentsinv, means any app that imports
// battery/queue has its queue_jobs table included in App.ExportData, so a data
// dump/restore is complete. The framework centralizes all raw read/write
// behind one SafeIdent-guarded path; this registration is purely declarative.
//
// The table name is the default "queue_jobs". A host that renamed it via
// WithTable must datexport.Register the new name (or the default entry is
// skipped with a note at export time and that table is excluded).

func init() {
	datexport.Register(datexport.DataExporter{
		Name:       "queue_jobs",
		Source:     "queue",
		Table:      "queue_jobs",
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
		},
	})
	// The erase-plane mirror. A job's payload routinely carries the very
	// data an erasure is meant to remove (the address an email job renders,
	// the document a render job points at), and queue_jobs lives outside
	// the entity registry, so App.EraseUserData can only reach it through a
	// declaration like this one. Terminal rows make it acute: DBQueue's only
	// job-row DELETE is Ack-of-claimed, and 'failed' rows are retained on
	// purpose for Stats/ListJobs/Replay, so without this a dead-lettered job
	// keeps the erased user's data forever and every later ExportData dump
	// re-discloses it. Delete rather than anonymize: a job row minus its
	// payload cannot be replayed, so there is nothing left worth retaining.
	datexport.RegisterEraser(datexport.DataEraser{
		Name:   "queue_jobs",
		Source: "queue",
		Table:  "queue_jobs",
		Column: "user_id",
		Mode:   datexport.EraseDelete,
	})
}
