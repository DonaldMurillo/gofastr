package queue

import "github.com/DonaldMurillo/gofastr/core/datexport"

func init() {
	registerJobsTable("queue_jobs")
}

func registerJobsTable(table string) {
	name := datexport.ScopedName("queue_jobs", table)
	datexport.Register(datexport.DataExporter{
		Name:       name,
		Source:     "queue",
		Table:      table,
		PrimaryKey: "id",
		Columns: []string{
			"id",
			"occurrence_id",
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
			"claim_token",
			"user_id",
			"unique_key",
			"updated_at",
			"last_error",
			"max_age_ns",
			"sequence_key",
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
