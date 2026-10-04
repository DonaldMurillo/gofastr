package framework

import "github.com/DonaldMurillo/gofastr/framework/cron"

// Root spellings of framework/cron. framework.X is the public API; the
// extraction moved the implementation, not the name callers write.

type (
	CronJob   = cron.CronJob
	Scheduler = cron.Scheduler
)

// NewScheduler wraps cron.NewScheduler.
func NewScheduler() *cron.Scheduler { return cron.NewScheduler() }
