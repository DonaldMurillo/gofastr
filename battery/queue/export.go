package queue

import "github.com/DonaldMurillo/gofastr/framework/datexport"

// The queue battery owns a physical table that lives OUTSIDE the framework
// entity registry (it is created with raw DDL in db.go). Registering it here
// from init() — mirroring framework/agentsinv — means any app that imports
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
		},
	})
}
